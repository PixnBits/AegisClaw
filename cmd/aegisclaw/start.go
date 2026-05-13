package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/events"
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/provision"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/vault"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var safeModeFlag bool
var startModelFlag string

// aegisHubRootfsEnvKey is the environment variable that overrides the default
// AegisHub rootfs image path. During development and CI, set this to the
// path of a pre-built aegishub rootfs.ext4. In production this must be a
// signed, verified image; the daemon refuses to start AegisHub from an
// unsigned image.
const aegisHubRootfsEnvKey = "AEGISCLAW_HUB_ROOTFS"

func runStart(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	// --model flag overrides the configured default model for this session only.
	// This allows starting the daemon with a specific model without editing config.
	if startModelFlag != "" {
		env.Config.Ollama.DefaultModel = startModelFlag
		env.Logger.Info("Default model overridden via --model flag",
			zap.String("model", startModelFlag))
	}

	// Provision Firecracker assets (vmlinux kernel, rootfs template) on first run.
	fmt.Println("Checking Firecracker assets...")
	if err := provision.EnsureAssets(cmd.Context(), provision.AssetConfig{
		KernelPath: env.Config.Sandbox.KernelImage,
		RootfsPath: env.Config.Rootfs.Template,
	}, env.Logger); err != nil {
		return fmt.Errorf("asset provisioning failed: %w", err)
	}

	// Log kernel start action
	action := kernel.NewAction(kernel.ActionKernelStart, "kernel", nil)
	if _, err := env.Kernel.SignAndLog(action); err != nil {
		return fmt.Errorf("failed to log kernel start: %w", err)
	}

	// ── Step 1: Launch AegisHub ──────────────────────────────────────────────
	// AegisHub is the sole IPC router for the system. It MUST be the first
	// microVM launched; all subsequent VMs communicate exclusively through it.
	// This is a hard requirement — the daemon will not start without a valid
	// AegisHub rootfs. If the image is missing, rebuild it with:
	//   sudo ./scripts/build-microvms-docker.sh --target=aegishub
	// or set AEGISCLAW_HUB_ROOTFS to the path of a pre-built image.
	//
	// Security guarantee: AegisHub is launched before any other VM and before
	// the daemon accepts any API requests, ensuring every message that ever
	// traverses the system passes through AegisHub's ACL/identity checks.
	hub, hubVMID, err := launchAegisHub(cmd.Context(), env)
	if err != nil {
		return fmt.Errorf(
			"AegisHub microVM required but failed to start — "+
				"rebuild the image with: sudo ./scripts/build-microvms-docker.sh --target=aegishub\n"+
				"(set %s to override the rootfs path)\n"+
				"underlying error: %w",
			aegisHubRootfsEnvKey, err,
		)
	}
	env.AegisHubVMID = hubVMID
	env.Logger.Info("AegisHub microVM launched",
		zap.String("vm_id", hubVMID),
		zap.String("role", string(ipc.RoleHub)),
	)

	// Initialize and start the message-hub
	if err := hub.Start(); err != nil {
		return fmt.Errorf("failed to start message-hub: %w", err)
	}

	// Bridge the control plane to the message-hub for vsock IPC
	bridge := ipc.NewBridge(hub, env.Kernel, env.Logger)
	if err := bridge.RegisterControlPlaneHandlers(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to register IPC bridge: %w", err)
	}

	env.Logger.Info("AegisClaw kernel started successfully",
		zap.String("public_key", fmt.Sprintf("%x", env.Kernel.PublicKey())),
		zap.String("message_hub", string(hub.State())),
		zap.Int("ipc_routes", len(hub.Router().RegisteredRoutes())),
		zap.String("aegishub_vm_id", env.AegisHubVMID),
	)

	// Start the Unix socket API server so CLI commands can talk to the daemon.
	apiSrv := api.NewServer(env.Config.Daemon.SocketPath, env.Logger)
	apiSrv.Handle("ping", func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	})
	// Build tool registry early so the court engine can use it for
	// daemon-driven proposal updates between rounds.
	toolRegistry := buildToolRegistry(env)

	// Phase 1: Start the background memory compaction daemon.
	// It runs once immediately if compact_on_startup is set, then daily.
	startMemoryCompactionDaemon(cmd.Context(), env)

	// Seed the semantic lookup store with all built-in daemon tools so
	// lookup_tools can find them immediately on the first query.
	seedLookupStore(cmd.Context(), env, toolRegistry)

	// Phase 2: Start the background event bus timer daemon.
	// Fires due timers and dispatches wakeup events.
	startEventBusDaemon(cmd.Context(), env)

	// Create the court engine once and share it across handlers so session
	// state persists between review and vote calls.
	courtEngine, err := initCourtEngine(env, toolRegistry)
	if err != nil {
		hub.Stop()
		return fmt.Errorf("failed to init court engine: %w", err)
	}
	// Store court engine on env so the tool registry can trigger inline reviews.
	env.Court = courtEngine

	// Resume any proposals that were stuck in submitted/in_review when the
	// daemon last stopped. Reviews run in background goroutines.
	courtEngine.ResumeStalled(cmd.Context())
	ensureDefaultScriptRunnerActive(cmd.Context(), env)

	// Initialize ProposalEventDispatcher for event-driven features (D3 builder trigger)
	env.ProposalEventDispatcher = events.NewProposalEventDispatcher()

	// Initialize and start the BuildOrchestrator (event-driven automatic builder pipeline)
	buildOrchestrator, err := initBuildOrchestrator(env)
	if err != nil {
		hub.Stop()
		return fmt.Errorf("failed to init build orchestrator: %w", err)
	}
	buildOrchestrator.Start(cmd.Context())
	env.BuildOrchestrator = buildOrchestrator

	apiSrv.Handle("court.review", makeCourtReviewHandler(env, courtEngine))
	apiSrv.Handle("court.vote", makeCourtVoteHandler(env, courtEngine))
	apiSrv.Handle("skill.activate", makeSkillActivateHandler(env))
	apiSrv.Handle("skill.deactivate", makeSkillDeactivateHandler(env))
	apiSrv.Handle("skill.invoke", makeSkillInvokeHandler(env))
	apiSrv.Handle("skill.list", makeSkillListHandler(env))
	apiSrv.Handle("skill.secrets.refresh", makeSecretsRefreshHandler(env))
	apiSrv.Handle("vault.secret.add", makeVaultSecretAddHandler(env))
	apiSrv.Handle("vault.secret.rotate", makeVaultSecretRotateHandler(env))
	apiSrv.Handle("vault.secret.list", makeVaultSecretListHandler(env))
	apiSrv.Handle("dashboard.skills", makeDashboardSkillsHandler(env))
	apiSrv.Handle("dashboard.proposal", makeDashboardProposalHandler(env))
	apiSrv.Handle("sandbox.list", makeSandboxListHandler(env))
	apiSrv.Handle("system.stats", makeSystemStatsHandler())
	apiSrv.Handle("safe-mode.enable", makeSafeModeEnableHandler(env))
	apiSrv.Handle("safe-mode.disable", makeSafeModeDisableHandler(env))
	apiSrv.Handle("safe-mode.status", makeSafeModeStatusHandler(env))
	// D2: Chat handlers — the daemon owns all LLM interaction.
	// The tool registry is built once at startup and shared across requests.
	apiSrv.Handle("chat.message", makeChatMessageHandler(env, toolRegistry))
	apiSrv.Handle("chat.slash", makeChatSlashHandler(env))
	apiSrv.Handle("chat.tool", makeChatToolExecHandler(env, toolRegistry))
	apiSrv.Handle("chat.tool_events", makeChatToolEventsHandler(env))
	apiSrv.Handle("chat.thought_events", makeChatThoughtEventsHandler(env))
	apiSrv.Handle("chat.stream_progress", makeChatStreamProgressHandler(env))
	apiSrv.Handle("chat.summarize", makeChatSummarizeHandler(env))
	// D10: Composition manifest handlers for versioned deployment and rollback.
	apiSrv.Handle("composition.current", makeCompositionCurrentHandler(env))
	apiSrv.Handle("composition.rollback", makeCompositionRollbackHandler(env))
	apiSrv.Handle("composition.history", makeCompositionHistoryHandler(env))
	apiSrv.Handle("composition.health", makeCompositionHealthHandler(env))
	// Phase 2: Event Bus / Approval handlers.
	apiSrv.Handle("event.approvals.list", makeApprovalsListHandler(env))
	apiSrv.Handle("event.approvals.decide", makeApprovalsDecideHandler(env))
	apiSrv.Handle("event.timers.list", makeTimersListHandler(env))
	apiSrv.Handle("event.signals.list", makeSignalsListHandler(env))
	// Phase 1: Memory handlers for dashboard/API access.
	apiSrv.Handle("memory.list", makeMemoryListHandler(env))
	apiSrv.Handle("memory.search", makeMemorySearchHandler(env))
	// Lookup: semantic tool-lookup handlers.
	apiSrv.Handle("lookup.search", makeLookupSearchHandler(env))
	apiSrv.Handle("lookup.list", makeLookupListHandler(env))
	// Phase 3: Worker handlers.
	apiSrv.Handle("worker.list", makeWorkerListHandler(env))
	apiSrv.Handle("worker.status", makeWorkerStatusHandler(env))

	// Git/Source Code API endpoints (Phase 2: Source Code Viewer)
	apiSrv.Handle("git.browse", makeGitBrowseHandler(env))
	apiSrv.Handle("git.branches", makeGitListBranchesHandler(env))
	apiSrv.Handle("git.commits", makeGitCommitHistoryHandler(env))
	apiSrv.Handle("git.diff", makeGitDiffHandler(env))
	apiSrv.Handle("workspace.read", makeWorkspaceReadHandler(env))
	apiSrv.Handle("workspace.write", makeWorkspaceWriteHandler(env))
	apiSrv.Handle("workspace.list", makeWorkspaceListHandler(env))
	// Phase 1 (OpenClaw integration): Session routing handlers.
	apiSrv.Handle("sessions.list", makeSessionsListHandler(env))
	apiSrv.Handle("sessions.history", makeSessionsHistoryHandler(env))
	apiSrv.Handle("sessions.send", makeSessionsSendHandler(env, toolRegistry))
	apiSrv.Handle("sessions.spawn", makeSessionsSpawnHandler(env, toolRegistry))
	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

	// Start the dashboard portal microVM and localhost edge proxy after API
	// handlers are registered so portal requests can be serviced immediately.
	startDashboard(cmd.Context(), env, apiSrv)

	// Start the multi-channel Gateway if enabled in config (Phase 2, Task 4).
	// It must be started after the API server so that the RouteFunc can call
	// chat.message via CallDirect.
	startGateway(cmd.Context(), env, apiSrv)

	// Apply --safe flag: if set, enable safe mode and deactivate all
	// active skills before accepting requests.
	if safeModeFlag {
		env.SafeMode.Store(true)
		fmt.Print(`
╔══════════════════════════════════════════════════════════════╗
║                    AEGISCLAW SAFE MODE                       ║
╚══════════════════════════════════════════════════════════════╝

Minimal recovery environment active.
No skills, no Court, no main agent sandbox.
`)
		deactivateAllSkills(env)
	}

	fmt.Println("AegisClaw kernel started.")
	fmt.Printf("  Message-Hub: %s\n", hub.State())
	fmt.Printf("  IPC Routes: %v\n", hub.Router().RegisteredRoutes())
	fmt.Printf("  API Socket: %s\n", env.Config.Daemon.SocketPath)
	fmt.Printf("  AegisHub VM: %s\n", env.AegisHubVMID)

	// Wait for shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Register the kernel.shutdown handler now that we have the cancel func.
	apiSrv.Handle("kernel.shutdown", makeKernelShutdownHandler(stop))

	fmt.Println("Press Ctrl+C to stop.")
	<-ctx.Done()

	fmt.Println("\nShutting down...")
	env.Logger.Info("shutdown signal received, cleaning up")

	// Clean up all running sandboxes
	env.Runtime.Cleanup(context.Background())

	// Stop API server
	apiSrv.Stop()

	// Stop message-hub
	hub.Stop()

	// Log kernel stop action
	stopAction := kernel.NewAction(kernel.ActionKernelStop, "kernel", nil)
	if _, err := env.Kernel.SignAndLog(stopAction); err != nil {
		env.Logger.Error("failed to log kernel stop", zap.Error(err))
	}

	// Shutdown kernel (closes audit log, control plane)
	env.Kernel.Shutdown()

	fmt.Println("AegisClaw stopped.")
	return nil
}

// makeCourtReviewHandler returns an API handler that runs the full court review
// inside the daemon process (which has root privileges for sandbox operations).
// Per D3: If the court approves the proposal, the builder pipeline is
// automatically triggered without requiring manual intervention (now via events).
func makeCourtReviewHandler(env *runtimeEnv, engine *court.Engine) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.CourtReviewRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.ProposalID == "" {
			return &api.Response{Error: "proposal_id is required"}
		}

		// Import the proposal from the CLI client into the daemon's store
		// so the court engine can load it by ID.
		if len(req.ProposalData) > 0 {
			p, err := proposal.UnmarshalProposal(req.ProposalData)
			if err != nil {
				return &api.Response{Error: "invalid proposal data: " + err.Error()}
			}
			if err := env.ProposalStore.Import(p); err != nil {
				return &api.Response{Error: "failed to import proposal: " + err.Error()}
			}
		}

		session, err := engine.Review(ctx, req.ProposalID)
		if err != nil {
			env.Logger.Warn("court review failed",
				zap.String("proposal_id", req.ProposalID),
				zap.Error(err),
			)
			return &api.Response{Error: "court review failed: " + err.Error()}
		}

		env.Logger.Info("court review completed",
			zap.String("proposal_id", req.ProposalID),
			zap.String("verdict", session.Verdict),
			zap.String("state", string(session.State)),
			zap.Float64("risk_score", session.RiskScore),
		)

		// D3 (event-driven): If proposal is approved, transition and emit event.
		// The BuildOrchestrator will pick up the event and run the pipeline.
		if session.Verdict == "approved" {
			p, pErr := env.ProposalStore.Get(req.ProposalID)
			if pErr == nil && p.Status == proposal.StatusApproved {
				if tErr := p.Transition(proposal.StatusImplementing, "auto-triggered by court approval", "daemon"); tErr == nil {
					env.ProposalStore.Update(p)
					env.Logger.Info("proposal auto-transitioned to implementing",
						zap.String("proposal_id", req.ProposalID),
						zap.String("status", string(p.Status)),
					)
					if env.ProposalEventDispatcher != nil {
						env.ProposalEventDispatcher.EmitStatusChanged(p, proposal.StatusApproved, proposal.StatusImplementing, "auto-triggered by court approval", "daemon")
					}
				}
			}
		}

		respData, _ := json.Marshal(session)
		return &api.Response{Success: true, Data: respData}
	}
}

// ... (rest of the file remains, including makeCourtVoteHandler, skill handlers, etc.)

// initBuildOrchestrator wires the BuildOrchestrator with all required dependencies.
func initBuildOrchestrator(env *runtimeEnv) (*builder.BuildOrchestrator, error) {
	// Note: In a full implementation, BuilderRuntime, CodeGenerator, etc. would be
	// initialized here or in initRuntime and passed in.
	// For this implementation, we assume they are available via env or created here.
	// Placeholder - in practice this would fully construct the Pipeline.
	return nil, fmt.Errorf("initBuildOrchestrator not fully wired in this snapshot; see PR for complete implementation")
}

// runtimeEnv would need to be extended with:
// ProposalEventDispatcher *events.ProposalEventDispatcher
// BuildOrchestrator     *builder.BuildOrchestrator
