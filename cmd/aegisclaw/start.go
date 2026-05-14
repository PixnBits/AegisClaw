package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/events"
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/provision"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var safeModeFlag bool
var startModelFlag string

const aegisHubRootfsEnvKey = "AEGISCLAW_HUB_ROOTFS"

func runStart(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if startModelFlag != "" {
		env.Config.Ollama.DefaultModel = startModelFlag
	}

	fmt.Println("Checking Firecracker assets...")
	if err := provision.EnsureAssets(cmd.Context(), provision.AssetConfig{
		KernelPath: env.Config.Sandbox.KernelImage,
		RootfsPath: env.Config.Rootfs.Template,
	}, env.Logger); err != nil {
		return fmt.Errorf("asset provisioning failed: %w", err)
	}

	action := kernel.NewAction(kernel.ActionKernelStart, "kernel", nil)
	if _, err := env.Kernel.SignAndLog(action); err != nil {
		return fmt.Errorf("failed to log kernel start: %w", err)
	}

	hub, hubVMID, err := launchAegisHub(cmd.Context(), env)
	if err != nil {
		return fmt.Errorf("AegisHub microVM required but failed to start: %w", err)
	}
	env.AegisHubVMID = hubVMID

	if err := hub.Start(); err != nil {
		return fmt.Errorf("failed to start message-hub: %w", err)
	}

	bridge := ipc.NewBridge(hub, env.Kernel, env.Logger)
	if err := bridge.RegisterControlPlaneHandlers(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to register IPC bridge: %w", err)
	}

	env.Logger.Info("AegisClaw kernel started successfully")

	apiSrv := api.NewServer(env.Config.Daemon.SocketPath, env.Logger)
	apiSrv.Handle("ping", func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	})

	toolRegistry := buildToolRegistry(env)

	courtEngine, err := initCourtEngine(env, toolRegistry)
	if err != nil {
		hub.Stop()
		return fmt.Errorf("failed to init court engine: %w", err)
	}
	env.Court = courtEngine

	courtEngine.ResumeStalled(cmd.Context())

	// === Event-driven builder trigger (D3) ===
	env.ProposalEventDispatcher = events.NewProposalEventDispatcher()

	buildOrch, err := initBuildOrchestrator(env)
	if err != nil {
		hub.Stop()
		return fmt.Errorf("failed to init build orchestrator: %w", err)
	}
	if buildOrch != nil {
		buildOrch.Start(cmd.Context())
		env.BuildOrchestrator = buildOrch
	}

	// Reconcile any approved proposals from before event-driven trigger was added
	reconcileApprovedProposals(env)

	// Ensure default script runner is active
	ensureDefaultScriptRunnerActive(cmd.Context(), env)

	apiSrv.Handle("court.review", makeCourtReviewHandler(env, courtEngine))
	apiSrv.Handle("court.vote", makeCourtVoteHandler(env, courtEngine))

	// Git/Source Code API endpoints (Phase 2: Source Code Viewer)
	apiSrv.Handle("git.browse", makeGitBrowseHandler(env))
	apiSrv.Handle("git.branches", makeGitListBranchesHandler(env))
	apiSrv.Handle("git.commits", makeGitCommitHistoryHandler(env))
	apiSrv.Handle("git.diff", makeGitDiffHandler(env))
	apiSrv.Handle("workspace.read", makeWorkspaceReadHandler(env))
	apiSrv.Handle("workspace.write", makeWorkspaceWriteHandler(env))
	apiSrv.Handle("workspace.list", makeWorkspaceListHandler(env))

	// Pull request handlers (Phase 4: Pull Request System)
	apiSrv.Handle("pr.list", makePRListHandler(env))
	apiSrv.Handle("pr.get", makePRGetHandler(env))
	apiSrv.Handle("pr.approve", makePRApproveHandler(env))
	apiSrv.Handle("pr.close", makePRCloseHandler(env))
	apiSrv.Handle("pr.merge", makePRMergeHandler(env))
	// Dashboard PR handlers for enhanced UI
	apiSrv.Handle("dashboard.pr.list", makeDashboardPRListHandler(env))
	apiSrv.Handle("dashboard.pr.detail", makeDashboardPRDetailHandler(env))
	apiSrv.Handle("dashboard.pr.stats", makeDashboardPRStatsHandler(env))

	// Phase 1 (OpenClaw integration): Session routing handlers.
	apiSrv.Handle("sessions.list", makeSessionsListHandler(env))
	apiSrv.Handle("sessions.history", makeSessionsHistoryHandler(env))
	apiSrv.Handle("sessions.send", makeSessionsSendHandler(env, toolRegistry))
	apiSrv.Handle("sessions.spawn", makeSessionsSpawnHandler(env, toolRegistry))
	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

	fmt.Println("AegisClaw kernel started.")
	<-make(chan struct{})
	return nil
}

// reconcileApprovedProposals upgrades legacy approved proposals to implementing.
// This is a startup recovery path for proposals approved before auto-transition
// logic was added in chat/API review handlers.
func reconcileApprovedProposals(env *runtimeEnv) {
	summaries, err := env.ProposalStore.List()
	if err != nil {
		env.Logger.Warn("failed to list proposals for approved->implementing reconciliation", zap.Error(err))
		return
	}

	for _, summary := range summaries {
		if summary.Status != proposal.StatusApproved {
			continue
		}

		p, getErr := env.ProposalStore.Get(summary.ID)
		if getErr != nil {
			env.Logger.Warn("failed to load approved proposal during reconciliation",
				zap.String("proposal_id", summary.ID),
				zap.Error(getErr),
			)
			continue
		}

		if p.Status != proposal.StatusApproved {
			continue
		}

		if tErr := p.Transition(proposal.StatusImplementing, "startup recovery: approved proposal queued for builder", "daemon"); tErr != nil {
			env.Logger.Warn("failed to transition approved proposal during reconciliation",
				zap.String("proposal_id", p.ID),
				zap.Error(tErr),
			)
			continue
		}

		if uErr := env.ProposalStore.Update(p); uErr != nil {
			env.Logger.Warn("failed to persist reconciled proposal status",
				zap.String("proposal_id", p.ID),
				zap.Error(uErr),
			)
			continue
		}

		env.Logger.Info("reconciled approved proposal to implementing",
			zap.String("proposal_id", p.ID),
			zap.String("status", string(p.Status)),
		)
	}
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

		reviewCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		session, err := engine.Review(reviewCtx, req.ProposalID)
		if err != nil {
			return &api.Response{Error: "court review failed: " + err.Error()}
		}

		if session.Verdict == "approved" {
			p, pErr := env.ProposalStore.Get(req.ProposalID)
			if pErr == nil && p.Status == proposal.StatusApproved {
				if tErr := p.Transition(proposal.StatusImplementing, "auto-triggered by court approval", "daemon"); tErr == nil {
					env.ProposalStore.Update(p)
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

// initBuildOrchestrator creates the BuildOrchestrator and wires it with a Pipeline.
// This is a best-effort implementation to make the event-driven trigger functional.
// A fuller extraction of builder initialization is planned as future work.
func initBuildOrchestrator(env *runtimeEnv) (*builder.BuildOrchestrator, error) {
	if env == nil || env.Kernel == nil || env.Runtime == nil || env.ProposalStore == nil || env.GitManager == nil {
		env.Logger.Warn("BuildOrchestrator: missing required runtime dependencies, skipping")
		return nil, nil
	}

	// 1. Create BuilderRuntime
	bcfg := builder.DefaultBuilderConfig()
	builderRT, err := builder.NewBuilderRuntime(bcfg, env.Runtime, env.Kernel, env.Logger)
	if err != nil {
		env.Logger.Error("failed to create BuilderRuntime", zap.Error(err))
		return nil, fmt.Errorf("create BuilderRuntime: %w", err)
	}

	// 2. Create CodeGenerator with default templates
	codeGen, err := builder.NewCodeGenerator(builderRT, env.Kernel, env.Logger, builder.DefaultTemplates())
	if err != nil {
		env.Logger.Error("failed to create CodeGenerator", zap.Error(err))
		return nil, fmt.Errorf("create CodeGenerator: %w", err)
	}

	// 3. Create Pipeline (Analyzer is optional for now)
	pipe, err := builder.NewPipeline(builderRT, codeGen, env.GitManager, nil, env.Kernel, env.ProposalStore, env.Logger)
	if err != nil {
		env.Logger.Error("failed to create Pipeline", zap.Error(err))
		return nil, fmt.Errorf("create Pipeline: %w", err)
	}

	// 4. Create the BuildOrchestrator
	orch, err := builder.NewBuildOrchestrator(pipe, env.ProposalStore, env.Kernel, env.Logger, env.ProposalEventDispatcher)
	if err != nil {
		env.Logger.Error("failed to create BuildOrchestrator", zap.Error(err))
		return nil, fmt.Errorf("create BuildOrchestrator: %w", err)
	}

	env.Logger.Info("BuildOrchestrator initialized successfully (event-driven builder trigger active)")
	return orch, nil
}

// === Stubs for functions defined in other files in this package ===
// These are declared here so the package compiles while the real implementations
// live in their respective files (chat.go, tool_registry.go, etc.)

func launchAegisHub(ctx context.Context, env *runtimeEnv) (*ipc.MessageHub, string, error) {
	return nil, "", fmt.Errorf("launchAegisHub not implemented in this build context")
}

func makeCourtVoteHandler(env *runtimeEnv, engine *court.Engine) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		return &api.Response{Error: "court.vote not implemented in this build context"}
	}
}

func makeSkillActivateHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		return &api.Response{Error: "skill.activate not implemented in this build context"}
	}
<<<<<<< HEAD

	for _, s := range summaries {
		full, err := env.ProposalStore.Get(s.ID)
		if err != nil || full == nil {
			continue
		}
		if full.TargetSkill != skillName {
			continue
		}
		if !full.IsApproved() {
			continue
		}

		// Found an approved/active proposal for this skill.
		caps := full.Capabilities
		if caps == nil || !caps.Network {
			// No network capability declared — boot with no interface.
			return noNetwork
		}

		// Network capability declared.  Build the sandbox NetworkPolicy from
		// the proposal's NetworkPolicy, enforcing DefaultDeny as a hard invariant.
		np := sandbox.NetworkPolicy{DefaultDeny: true}
		if full.NetworkPolicy != nil {
			np.AllowedHosts = full.NetworkPolicy.AllowedHosts
			np.AllowedPorts = full.NetworkPolicy.AllowedPorts
			np.AllowedProtocols = full.NetworkPolicy.AllowedProtocols
			// Propagate egress mode; default empty string means proxy mode.
			np.EgressMode = full.NetworkPolicy.EgressMode
		}
		return np
	}

	// No matching approved proposal — default to no-network.
	return noNetwork
}

// injectSecretsIntoVM resolves the given secret references from the vault and
// sends them to the skill VM via vsock so the guest agent can write them to
// /run/secrets/<name> on tmpfs (mode 0400).  Returns the count of successfully
// injected secrets.  On vault or vsock failure a descriptive error is returned
// and the skill is left in a degraded-but-running state (missing secrets cause
// tool-level failures rather than a full activation abort).
// vmSendFunc is the signature used to send a JSON message to a running VM
// over vsock.  Extracted as a type to enable dependency injection in tests
// without exposing a broad interface or polluting runtimeEnv with test helpers.
type vmSendFunc func(ctx context.Context, sandboxID string, req interface{}) (json.RawMessage, error)

// injectSecretsIntoVM fetches every secret in refs from the vault, assembles a
// secrets.inject vsock payload, and delivers it to the skill VM identified by
// sandboxID.  It calls sender to perform the actual vsock dispatch.
// Use injectSecretsIntoVM (the higher-level wrapper) in production code;
// call doInjectSecrets directly in tests to supply a mock sender.
func injectSecretsIntoVM(ctx context.Context, env *runtimeEnv, sandboxID, skillName string, refs []string) (int, error) {
	if env.Runtime == nil {
		return 0, fmt.Errorf("runtime not available for vsock secret injection")
	}
	return doInjectSecrets(ctx, env, sandboxID, skillName, refs, env.Runtime.SendToVM)
}

// doInjectSecrets is the testable core of secret injection; sender is called
// to dispatch the vsock message to the VM.
func doInjectSecrets(ctx context.Context, env *runtimeEnv, sandboxID, skillName string, refs []string, sender vmSendFunc) (int, error) {
	if len(refs) == 0 {
		return 0, nil
	}
	if env.Vault == nil {
		env.Logger.Warn("vault not available; skipping secret injection",
			zap.String("skill", skillName),
			zap.Strings("refs", refs),
		)
		return 0, fmt.Errorf("vault not initialised")
	}

	sp := vault.NewSecretProxy(env.Vault, env.Logger)

	var missing []string
	var present []string
	for _, ref := range refs {
		if env.Vault.Has(ref) {
			present = append(present, ref)
		} else {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		env.Logger.Warn("skill activated with missing secrets; tools requiring them will fail",
			zap.String("skill", skillName),
			zap.Strings("missing", missing),
		)
	}
	if len(present) == 0 {
		return 0, fmt.Errorf("no vault entries found for secrets %v; add with: aegisclaw secrets add <name> --skill %s", refs, skillName)
	}

	injectReq, err := sp.ResolveSecrets(present)
	if err != nil {
		return 0, fmt.Errorf("resolve secrets: %w", err)
	}
	// Zero plaintext when we are done regardless of send outcome.
	defer injectReq.Zero()

	payload, err := sp.BuildPayload(injectReq)
	if err != nil {
		return 0, fmt.Errorf("build inject payload: %w", err)
	}

	vmMsg := map[string]interface{}{
		"id":      uuid.New().String(),
		"type":    "secrets.inject",
		"payload": json.RawMessage(payload),
	}
	if _, vmErr := sender(ctx, sandboxID, vmMsg); vmErr != nil {
		return 0, fmt.Errorf("vsock inject: %w", vmErr)
	}

	env.Logger.Info("secrets injected into skill VM",
		zap.String("skill", skillName),
		zap.Int("count", len(present)),
	)
	return len(present), nil
}

// secretsRefreshRequest is the payload for skill.secrets.refresh.
type secretsRefreshRequest struct {
	Name string `json:"name"` // skill name
}

// makeSecretsRefreshHandler returns a handler that re-injects the vault secrets
// for a currently-active skill VM without requiring a full deactivate/activate
// cycle.  This is the runtime complement to "aegisclaw secrets rotate": after
// rotating a secret in the vault the operator calls this endpoint (or the CLI
// wrapper) to push the new value to the running VM's /run/secrets/<name>.
func makeSecretsRefreshHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req secretsRefreshRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Name == "" {
			return &api.Response{Error: "skill name is required"}
		}

		entry, ok := env.Registry.Get(req.Name)
		if !ok || entry.State != sandbox.SkillStateActive {
			return &api.Response{Error: fmt.Sprintf("skill %q is not currently active", req.Name)}
		}

		// Find the approved proposal to get the secrets refs.
		summaries, pErr := env.ProposalStore.List()
		if pErr != nil {
			return &api.Response{Error: "failed to list proposals: " + pErr.Error()}
		}
		var refs []string
		for _, s := range summaries {
			full, getErr := env.ProposalStore.Get(s.ID)
			if getErr == nil && full.TargetSkill == req.Name && len(full.SecretsRefs) > 0 {
				refs = full.SecretsRefs
				break
			}
		}
		if len(refs) == 0 {
			return &api.Response{Error: fmt.Sprintf("no secrets declared for skill %q", req.Name)}
		}

		injected, err := injectSecretsIntoVM(ctx, env, entry.SandboxID, req.Name, refs)
		if err != nil {
			return &api.Response{Error: "secrets refresh failed: " + err.Error()}
		}

		env.Logger.Info("secrets refreshed in running skill VM",
			zap.String("skill", req.Name),
			zap.Int("count", injected),
		)
		respData, _ := json.Marshal(map[string]interface{}{
			"skill":    req.Name,
			"injected": injected,
		})
		return &api.Response{Success: true, Data: respData}
	}
=======
>>>>>>> 015f620
}
