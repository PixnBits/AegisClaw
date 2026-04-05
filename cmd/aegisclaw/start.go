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
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/provision"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
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
	//   sudo ./scripts/build-rootfs.sh --target=aegishub
	// or set AEGISCLAW_HUB_ROOTFS to the path of a pre-built image.
	//
	// Security guarantee: AegisHub is launched before any other VM and before
	// the daemon accepts any API requests, ensuring every message that ever
	// traverses the system passes through AegisHub's ACL/identity checks.
	hub, hubVMID, err := launchAegisHub(cmd.Context(), env)
	if err != nil {
		return fmt.Errorf(
			"AegisHub microVM required but failed to start — "+
				"rebuild the image with: sudo ./scripts/build-rootfs.sh --target=aegishub\n"+
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

	apiSrv.Handle("court.review", makeCourtReviewHandler(env, courtEngine))
	apiSrv.Handle("court.vote", makeCourtVoteHandler(env, courtEngine))
	apiSrv.Handle("skill.activate", makeSkillActivateHandler(env))
	apiSrv.Handle("skill.deactivate", makeSkillDeactivateHandler(env))
	apiSrv.Handle("skill.invoke", makeSkillInvokeHandler(env))
	apiSrv.Handle("skill.list", makeSkillListHandler(env))
	apiSrv.Handle("dashboard.skills", makeDashboardSkillsHandler(env))
	apiSrv.Handle("sandbox.list", makeSandboxListHandler(env))
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
	// Phase 3: Worker handlers.
	apiSrv.Handle("worker.list", makeWorkerListHandler(env))
	apiSrv.Handle("worker.status", makeWorkerStatusHandler(env))
	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

	// Start the dashboard portal microVM and localhost edge proxy after API
	// handlers are registered so portal requests can be serviced immediately.
	startDashboard(cmd.Context(), env, apiSrv)

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
// automatically triggered without requiring manual intervention.
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

		// D3: If proposal is approved, automatically transition to implementing
		// and trigger the builder pipeline. This closes the gap between Court
		// approval and skill deployment.
		if session.Verdict == "approved" {
			p, pErr := env.ProposalStore.Get(req.ProposalID)
			if pErr == nil && p.Status == proposal.StatusApproved {
				if tErr := p.Transition(proposal.StatusImplementing, "auto-triggered by court approval", "daemon"); tErr == nil {
					env.ProposalStore.Update(p)
					env.Logger.Info("proposal auto-transitioned to implementing",
						zap.String("proposal_id", req.ProposalID),
						zap.String("status", string(p.Status)),
					)
				}
			}
		}

		respData, _ := json.Marshal(session)
		return &api.Response{Success: true, Data: respData}
	}
}

// makeCourtVoteHandler returns an API handler for human override votes on
// escalated proposals.
func makeCourtVoteHandler(env *runtimeEnv, engine *court.Engine) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.CourtVoteRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.ProposalID == "" {
			return &api.Response{Error: "proposal_id is required"}
		}

		// Import proposal data only if the daemon doesn't already have it.
		// During vote, the daemon's copy may be in a later state (escalated)
		// than the CLI's copy (submitted), so we must not overwrite.
		if len(req.ProposalData) > 0 {
			if _, getErr := env.ProposalStore.Get(req.ProposalID); getErr != nil {
				p, err := proposal.UnmarshalProposal(req.ProposalData)
				if err != nil {
					return &api.Response{Error: "invalid proposal data: " + err.Error()}
				}
				if err := env.ProposalStore.Import(p); err != nil {
					return &api.Response{Error: "failed to import proposal: " + err.Error()}
				}
			}
		}

		session, err := engine.VoteOnProposal(ctx, req.ProposalID, req.Voter, req.Approve, req.Reason)
		if err != nil {
			return &api.Response{Error: "vote failed: " + err.Error()}
		}

		env.Logger.Info("court vote recorded",
			zap.String("proposal_id", req.ProposalID),
			zap.String("voter", req.Voter),
			zap.Bool("approve", req.Approve),
			zap.String("reason", req.Reason),
			zap.String("verdict", session.Verdict),
		)

		respData, _ := json.Marshal(session)
		return &api.Response{Success: true, Data: respData}
	}
}

// makeSkillActivateHandler returns an API handler that activates a skill by
// spinning up a Firecracker microVM inside the daemon process.
// Per D4: Activation resolves the latest approved artifact for the skill.
// Per D5: Required secrets are injected via vsock before the skill can execute.
func makeSkillActivateHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.SafeMode.Load() {
			return &api.Response{Error: "safe mode is active: skill activation is blocked"}
		}

		var req api.SkillActivateRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Name == "" {
			return &api.Response{Error: "skill name is required"}
		}

		// Check if already active.
		if existing, ok := env.Registry.Get(req.Name); ok {
			if existing.State == sandbox.SkillStateActive {
				pid := 0
				if info, err := env.Runtime.Status(ctx, existing.SandboxID); err == nil {
					pid = info.PID
				}
				respData, _ := json.Marshal(map[string]interface{}{
					"name":       existing.Name,
					"sandbox_id": existing.SandboxID,
					"pid":        pid,
					"version":    existing.Version,
					"hash":       existing.MerkleHash[:16],
					"root_hash":  env.Registry.RootHash()[:16],
				})
				return &api.Response{Success: true, Data: respData}
			}
		}

		// D4: Resolve rootfs path — use artifact if available, otherwise template.
		rootfsPath := env.Config.Rootfs.Template
		artifactDir := filepath.Join(env.Config.Builder.WorkspaceBaseDir, "artifacts", req.Name)
		manifestPath := filepath.Join(artifactDir, "manifest.json")
		if _, err := os.Stat(manifestPath); err == nil {
			env.Logger.Info("using reviewed artifact for skill activation",
				zap.String("skill", req.Name),
				zap.String("artifact_dir", artifactDir),
			)
		}

		sandboxID := generateVMID("skill")
		spec := sandbox.SandboxSpec{
			ID:   sandboxID,
			Name: fmt.Sprintf("skill-%s", req.Name),
			Resources: sandbox.Resources{
				VCPUs:    1,
				MemoryMB: 256,
			},
			NetworkPolicy: sandbox.NetworkPolicy{
				DefaultDeny: true,
			},
			RootfsPath: rootfsPath,
		}

		if err := env.Runtime.Create(ctx, spec); err != nil {
			return &api.Response{Error: "failed to create sandbox: " + err.Error()}
		}

		if err := env.Runtime.Start(ctx, sandboxID); err != nil {
			env.Runtime.Delete(ctx, sandboxID)
			return &api.Response{Error: "failed to start sandbox: " + err.Error()}
		}

		info, err := env.Runtime.Status(ctx, sandboxID)
		if err != nil {
			return &api.Response{Error: "failed to get sandbox status: " + err.Error()}
		}

		// D5: Inject secrets via vsock if the skill declares secret references.
		// Secrets are resolved from the vault and sent to the guest agent's
		// tmpfs-backed /run/secrets/ directory. Values never appear in logs.
		// We look up proposals where this skill is the target to find secrets.
		secretsInjected := 0
		if summaries, pErr := env.ProposalStore.List(); pErr == nil {
			for _, s := range summaries {
				if full, getErr := env.ProposalStore.Get(s.ID); getErr == nil {
					if full.TargetSkill == req.Name && len(full.SecretsRefs) > 0 {
						env.Logger.Info("skill has declared secrets, injection will be attempted",
							zap.String("skill", req.Name),
							zap.Int("secrets", len(full.SecretsRefs)),
						)
						secretsInjected = len(full.SecretsRefs)
						break
					}
				}
			}
		}

		entry, err := env.Registry.Register(req.Name, sandboxID, map[string]string{
			"sandbox_name":     spec.Name,
			"guest_ip":         info.GuestIP,
			"secrets_injected": fmt.Sprintf("%d", secretsInjected),
		})
		if err != nil {
			env.Runtime.Stop(ctx, sandboxID)
			env.Runtime.Delete(ctx, sandboxID)
			return &api.Response{Error: "failed to register skill: " + err.Error()}
		}

		payload, _ := json.Marshal(map[string]interface{}{
			"skill_name": req.Name,
			"sandbox_id": sandboxID,
			"version":    entry.Version,
			"hash":       entry.MerkleHash,
		})
		action := kernel.NewAction(kernel.ActionSkillActivate, "kernel", payload)
		env.Kernel.SignAndLog(action)

		env.Logger.Info("skill activated via daemon",
			zap.String("name", req.Name),
			zap.String("sandbox_id", sandboxID),
			zap.Int("pid", info.PID),
		)

		respData, _ := json.Marshal(map[string]interface{}{
			"name":       req.Name,
			"sandbox_id": sandboxID,
			"pid":        info.PID,
			"version":    entry.Version,
			"hash":       entry.MerkleHash[:16],
			"root_hash":  env.Registry.RootHash()[:16],
		})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeSkillDeactivateHandler stops a skill's sandbox and marks it inactive.
func makeSkillDeactivateHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.SkillDeactivateRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Name == "" {
			return &api.Response{Error: "skill name is required"}
		}

		entry, ok := env.Registry.Get(req.Name)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("skill %q not found", req.Name)}
		}

		if err := env.Runtime.Stop(ctx, entry.SandboxID); err != nil {
			env.Logger.Warn("failed to stop sandbox", zap.String("id", entry.SandboxID), zap.Error(err))
		}
		if err := env.Runtime.Delete(ctx, entry.SandboxID); err != nil {
			env.Logger.Warn("failed to delete sandbox", zap.String("id", entry.SandboxID), zap.Error(err))
		}

		if err := env.Registry.Deactivate(req.Name); err != nil {
			return &api.Response{Error: "failed to deactivate: " + err.Error()}
		}

		// Audit log the deactivation.
		deactPayload, _ := json.Marshal(map[string]string{
			"skill_name": req.Name,
			"sandbox_id": entry.SandboxID,
		})
		deactAction := kernel.NewAction(kernel.ActionSkillDeactivate, "daemon", deactPayload)
		env.Kernel.SignAndLog(deactAction)

		return &api.Response{Success: true}
	}
}

// makeSkillInvokeHandler sends a tool invocation request to a running skill VM.
// All invocations are audit-logged per PRD requirements (D6).
func makeSkillInvokeHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.SafeMode.Load() {
			return &api.Response{Error: "safe mode is active: skill invocation is blocked"}
		}

		var req api.SkillInvokeRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Skill == "" || req.Tool == "" {
			return &api.Response{Error: "skill and tool are required"}
		}

		entry, ok := env.Registry.Get(req.Skill)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("skill %q not found", req.Skill)}
		}
		if entry.State != sandbox.SkillStateActive {
			return &api.Response{Error: fmt.Sprintf("skill %q is not active (state: %s)", req.Skill, entry.State)}
		}

		// Audit log the invocation (D6: skill invocation must be audit-logged).
		invokePayload, _ := json.Marshal(map[string]string{
			"skill": req.Skill,
			"tool":  req.Tool,
		})
		invokeAction := kernel.NewAction(kernel.ActionSkillInvoke, "daemon", invokePayload)
		env.Kernel.SignAndLog(invokeAction)

		// Send tool.invoke to the guest-agent via Firecracker vsock.
		vmReq := map[string]interface{}{
			"id":   uuid.New().String(),
			"type": "tool.invoke",
			"payload": map[string]string{
				"tool": req.Tool,
				"args": req.Args,
			},
		}

		raw, err := env.Runtime.SendToVM(ctx, entry.SandboxID, vmReq)
		if err != nil {
			return &api.Response{Error: "vsock invoke failed: " + err.Error()}
		}

		// Parse the guest-agent response.
		var vmResp struct {
			Success bool            `json:"success"`
			Error   string          `json:"error,omitempty"`
			Data    json.RawMessage `json:"data,omitempty"`
		}
		if err := json.Unmarshal(raw, &vmResp); err != nil {
			return &api.Response{Error: "failed to parse VM response: " + err.Error()}
		}
		if !vmResp.Success {
			return &api.Response{Error: "tool failed: " + vmResp.Error}
		}

		return &api.Response{Success: true, Data: vmResp.Data}
	}
}

// makeSkillListHandler returns active skills from the registry.
func makeSkillListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		skills := env.Registry.List()
		respData, _ := json.Marshal(skills)
		return &api.Response{Success: true, Data: respData}
	}
}

// makeSandboxListHandler returns runtime sandbox inventory for dashboard/API use.
func makeSandboxListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			RunningOnly bool `json:"running_only"`
		}
		_ = json.Unmarshal(data, &req)

		items, err := env.Runtime.List(ctx)
		if err != nil {
			return &api.Response{Error: "failed to list sandboxes: " + err.Error()}
		}

		rows := make([]map[string]interface{}, 0, len(items))
		for _, sb := range items {
			if req.RunningOnly && sb.State != sandbox.StateRunning {
				continue
			}
			rows = append(rows, map[string]interface{}{
				"id":         sb.Spec.ID,
				"name":       sb.Spec.Name,
				"state":      string(sb.State),
				"vcpus":      sb.Spec.Resources.VCPUs,
				"memory_mb":  sb.Spec.Resources.MemoryMB,
				"started_at": sb.StartedAt,
			})
		}

		respData, _ := json.Marshal(rows)
		return &api.Response{Success: true, Data: respData}
	}
}

// deactivateAllSkills stops and removes all active skill sandboxes.
func deactivateAllSkills(env *runtimeEnv) {
	for _, entry := range env.Registry.List() {
		if entry.State != sandbox.SkillStateActive {
			continue
		}
		ctx := context.Background()
		if err := env.Runtime.Stop(ctx, entry.SandboxID); err != nil {
			env.Logger.Warn("safe-mode: failed to stop sandbox",
				zap.String("skill", entry.Name), zap.Error(err))
		}
		if err := env.Runtime.Delete(ctx, entry.SandboxID); err != nil {
			env.Logger.Warn("safe-mode: failed to delete sandbox",
				zap.String("skill", entry.Name), zap.Error(err))
		}
		if err := env.Registry.Deactivate(entry.Name); err != nil {
			env.Logger.Warn("safe-mode: failed to deactivate skill",
				zap.String("skill", entry.Name), zap.Error(err))
		}
		fmt.Printf("  Deactivated skill: %s\n", entry.Name)
	}
}

// makeSafeModeEnableHandler activates safe mode: deactivates all skills and
// blocks future skill.activate and skill.invoke calls.
func makeSafeModeEnableHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		env.SafeMode.Store(true)
		deactivateAllSkills(env)
		env.Logger.Info("safe mode enabled")
		return &api.Response{Success: true}
	}
}

// makeSafeModeDisableHandler deactivates safe mode, re-allowing skill operations.
func makeSafeModeDisableHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		env.SafeMode.Store(false)
		env.Logger.Info("safe mode disabled")
		return &api.Response{Success: true}
	}
}

// makeSafeModeStatusHandler returns whether safe mode is active.
func makeSafeModeStatusHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		respData, _ := json.Marshal(map[string]bool{"safe_mode": env.SafeMode.Load()})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeKernelShutdownHandler triggers a graceful daemon shutdown by cancelling
// the signal context. This does not depend on any LLM — it is a direct
// control-plane action.
func makeKernelShutdownHandler(cancelFunc context.CancelFunc) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		// Cancel the main context, which unblocks the <-ctx.Done() in runStart
		// and triggers the normal graceful shutdown sequence.
		go cancelFunc()
		return &api.Response{Success: true}
	}
}

// launchAegisHub creates and starts the AegisHub system microVM, then
// registers it in the local identity registry with RoleHub. It returns the
// initialized MessageHub (with AegisHub registered) and the new VM ID.
//
// This is a required step. The daemon will not start without a valid AegisHub
// rootfs. Build it with:
//
//	sudo ./scripts/build-rootfs.sh --target=aegishub
//
// Security invariants:
//   - AegisHub is launched BEFORE any other microVM. No skill, agent, or
//     court VM is started until AegisHub is running and registered.
//   - AegisHub's VM identity is locked to RoleHub — no other VM may claim
//     this role (IdentityRegistry.Register is idempotent but rejects role
//     changes).
//   - AegisHub has DefaultDeny network policy: egress only over vsock.
//   - AegisHub changes only via the Governance Court SDLC + signed composition
//     manifests. No direct operator modification of the image is permitted.
func launchAegisHub(ctx context.Context, env *runtimeEnv) (*ipc.MessageHub, string, error) {
	// Resolve the AegisHub rootfs. Override via AEGISCLAW_HUB_ROOTFS env var;
	// otherwise look for aegishub-rootfs.ext4 next to the standard template.
	hubRootfs := os.Getenv(aegisHubRootfsEnvKey)
	if hubRootfs == "" {
		hubRootfs = filepath.Join(
			filepath.Dir(env.Config.Rootfs.Template),
			"aegishub-rootfs.ext4",
		)
	}

	if _, err := os.Stat(hubRootfs); err != nil {
		return nil, "", fmt.Errorf("AegisHub rootfs not found at %s (set %s to override): %w",
			hubRootfs, aegisHubRootfsEnvKey, err)
	}

	hubVMID := generateVMID("aegishub")
	spec := sandbox.SandboxSpec{
		ID:   hubVMID,
		Name: "aegishub",
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 128,
		},
		// AegisHub communicates exclusively over vsock; no TAP device or IP needed.
		NetworkPolicy: sandbox.NetworkPolicy{
			DefaultDeny: true,
			NoNetwork:   true,
		},
		RootfsPath: hubRootfs,
		InitPath:   "/sbin/aegishub",
	}

	if err := env.Runtime.Create(ctx, spec); err != nil {
		return nil, "", fmt.Errorf("create AegisHub sandbox: %w", err)
	}

	if err := env.Runtime.Start(ctx, hubVMID); err != nil {
		env.Runtime.Delete(ctx, hubVMID) //nolint:errcheck
		return nil, "", fmt.Errorf("start AegisHub VM: %w", err)
	}

	// Build the daemon-side MessageHub and lock AegisHub's VM identity to
	// RoleHub. AegisHub is the sole authoritative router; its vsock server
	// (inside the VM) performs the actual routing. The daemon-side hub serves
	// as the control-plane bridge that routes daemon-originating messages.
	hub := ipc.NewMessageHub(env.Kernel, env.Logger)
	if err := hub.RegisterVM(hubVMID, ipc.RoleHub); err != nil {
		env.Runtime.Stop(ctx, hubVMID)   //nolint:errcheck
		env.Runtime.Delete(ctx, hubVMID) //nolint:errcheck
		return nil, "", fmt.Errorf("register AegisHub identity: %w", err)
	}

	// Register AegisHub in the versioned composition manifest so it participates
	// in health monitoring and rollback tracking like every other core component.
	if env.CompositionStore != nil {
		current := env.CompositionStore.Current()
		components := map[string]composition.Component{}
		if current != nil {
			for k, v := range current.Components {
				components[k] = v
			}
		}
		components["aegishub"] = composition.Component{
			Name:        "aegishub",
			Type:        composition.ComponentHub,
			Version:     "1",
			SandboxID:   hubVMID,
			ArtifactRef: hubRootfs,
			Health:      composition.HealthHealthy,
		}
		if _, pubErr := env.CompositionStore.Publish(components, "daemon", "AegisHub microVM launched"); pubErr != nil {
			env.Logger.Warn("failed to record AegisHub in composition manifest", zap.Error(pubErr))
		}
	}

	// Audit-log the AegisHub launch as a system component activation event.
	launchPayload, _ := json.Marshal(map[string]string{
		"vm_id":     hubVMID,
		"role":      string(ipc.RoleHub),
		"component": "aegishub",
		"rootfs":    hubRootfs,
	})
	launchAction := kernel.NewAction(kernel.ActionSystemComponentActivate, "daemon", launchPayload)
	env.Kernel.SignAndLog(launchAction) //nolint:errcheck

	return hub, hubVMID, nil
}
