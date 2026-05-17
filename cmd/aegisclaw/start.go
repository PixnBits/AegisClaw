package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/events"
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/provision"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var safeModeFlag bool
var startModelFlag string
var startForeground bool
var startAllowExistingDaemon bool

const aegisHubRootfsEnvKey = "AEGISCLAW_HUB_ROOTFS"

func runStart(cmd *cobra.Command, args []string) error {
	if err := ensureDaemonNotRunning(cmd.Context(), startAllowExistingDaemon); err != nil {
		return err
	}
	if !startForeground {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable path: %w", err)
		}
		childArgs := []string{"start", "--foreground"}
		if safeModeFlag {
			childArgs = append(childArgs, "--safe")
		}
		if startModelFlag != "" {
			childArgs = append(childArgs, "--model", startModelFlag)
		}
		proc := exec.Command(exePath, childArgs...)
		proc.Stdout = os.Stdout
		proc.Stderr = os.Stderr
		if err := proc.Start(); err != nil {
			return fmt.Errorf("start daemon in background: %w", err)
		}
		fmt.Printf("AegisClaw daemon started in background (pid %d).\n", proc.Process.Pid)
		return nil
	}

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

	// === COURT EXTRACTION (AGGRESSIVE) ===
	// Court initialization has been removed from the Host Daemon.
	// We are moving toward dedicated Court components.
	// For now we use a stub client so the rest of the system can compile.
	courtClient := &court.StubClient{}
	_ = courtClient // placeholder until we wire real routing via AegisHub

	// Note: ResumeStalled and full Court engine initialization removed.
	// This is intentional as part of Minimal TCB refactor.

	regDir := filepath.Join(filepath.Dir(env.Config.Audit.Dir), "cli-registry")
	if teamReg, err := newTeamRegistry(regDir); err != nil {
		env.Logger.Warn("team registry disabled", zap.Error(err))
	} else {
		env.TeamRegistry = teamReg
	}
	if autoReg, err := newAutonomyRegistry(regDir); err != nil {
		env.Logger.Warn("autonomy registry disabled", zap.Error(err))
	} else {
		env.AutonomyRegistry = autoReg
	}

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

	// Court handlers now use the stub client during transition.
	// Real implementation will route through AegisHub to dedicated Court components.
	apiSrv.Handle("court.review", makeCourtReviewHandler(env, courtClient))
	apiSrv.Handle("court.vote", makeCourtVoteHandler(env, courtClient))

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

	daemonQuit := make(chan struct{})
	registerExtendedDaemonAPI(apiSrv, env, toolRegistry, hub, daemonQuit)

	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

	startDashboard(cmd.Context(), env, apiSrv)

	fmt.Println("AegisClaw kernel started.")
	<-daemonQuit
	env.Logger.Info("daemon exiting after shutdown request")
	return nil
}

func ensureDaemonNotRunning(ctx context.Context, allowExisting bool) error {
	if allowExisting {
		return nil
	}
	client := api.NewClient(resolveDaemonSocketPath())
	pingCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	if err := client.Ping(pingCtx); err == nil {
		return fmt.Errorf("daemon already running (use: aegisclaw restart)")
	}
	return nil
}

// reconcileApprovedProposals upgrades legacy approved proposals to implementing.
// This is a startup recovery path for proposals approved before auto-transition
// logic was added in chat/API review handlers.
func reconcileApprovedProposals(env *runtimeEnv) {
	ctx := context.Background()

	summaries, err := env.Store.Proposals().List(ctx, proposal.Filter{})
	if err != nil {
		env.Logger.Warn("failed to list proposals for approved->implementing reconciliation", zap.Error(err))
		return
	}

	for _, summary := range summaries {
		if summary.Status != proposal.StatusApproved {
			continue
		}

		p, getErr := env.Store.Proposals().Get(ctx, summary.ID)
		if getErr != nil {
			env.Logger.Warn("failed to load approved proposal during reconciliation",
				zap.String("proposal_id", summary.ID),
				zap.Error(getErr))
			continue
		}

		if p.Status != proposal.StatusApproved {
			continue
		}

		if tErr := p.Transition(proposal.StatusImplementing, "startup recovery: approved proposal queued for builder", "daemon"); tErr != nil {
			env.Logger.Warn("failed to transition approved proposal during reconciliation",
				zap.String("proposal_id", p.ID),
				zap.Error(tErr))
			continue
		}

		if uErr := env.Store.Proposals().Update(ctx, p); uErr != nil {
			env.Logger.Warn("failed to persist reconciled proposal status",
				zap.String("proposal_id", p.ID),
				zap.Error(uErr))
			continue
		}

		env.Logger.Info("reconciled approved proposal to implementing",
			zap.String("proposal_id", p.ID),
			zap.String("status", string(p.Status)),
		)
	}
}

// makeCourtReviewHandler is temporarily updated during aggressive Court extraction.
// It currently uses the stub client. Real implementation will route through AegisHub.
func makeCourtReviewHandler(env *runtimeEnv, client *court.StubClient) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		// TODO: Route court.review through AegisHub to dedicated Court components.
		// For now this is a stub during the aggressive Minimal TCB refactor.
		return &api.Response{Success: true, Data: []byte(`{"status":"stubbed","message":"Court extraction in progress"}`)}
	}
}

// makeCourtVoteHandler is temporarily stubbed during aggressive Court extraction.
func makeCourtVoteHandler(env *runtimeEnv, client *court.StubClient) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		// TODO: Route court.vote through AegisHub to dedicated Court components.
		return &api.Response{Success: true}
	}
}

// initBuildOrchestrator creates the BuildOrchestrator and wires it with a Pipeline.
// This is a best-effort implementation to make the event-driven trigger functional.
// A fuller extraction of builder initialization is planned as future work.
func initBuildOrchestrator(env *runtimeEnv) (*builder.BuildOrchestrator, error) {
	if env == nil || env.Kernel == nil || env.Runtime == nil || env.GitManager == nil {
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
	pipe, err := builder.NewPipeline(builderRT, codeGen, env.GitManager, nil, env.Kernel, env.Logger)
	if err != nil {
		env.Logger.Error("failed to create Pipeline", zap.Error(err))
		return nil, fmt.Errorf("create Pipeline: %w", err)
	}

	// 4. Create the BuildOrchestrator
	orch, err := builder.NewBuildOrchestrator(pipe, env.Logger, env.ProposalEventDispatcher)
	if err != nil {
		env.Logger.Error("failed to create BuildOrchestrator", zap.Error(err))
		return nil, fmt.Errorf("create BuildOrchestrator: %w", err)
	}

	env.Logger.Info("BuildOrchestrator initialized successfully (event-driven builder trigger active)")
	return orch, nil
}

// launchAegisHub starts the AegisHub system microVM and returns the in-process
// MessageHub used by the IPC bridge, together with the VM ID.
func launchAegisHub(ctx context.Context, env *runtimeEnv) (*ipc.MessageHub, string, error) {
	rootfsPath := os.Getenv(aegisHubRootfsEnvKey)
	if rootfsPath == "" {
		rootfsPath = filepath.Join(filepath.Dir(env.Config.Rootfs.Template), "aegishub-rootfs.ext4")
	}
	if _, err := os.Stat(rootfsPath); err != nil {
		return nil, "", fmt.Errorf(
			"AegisHub rootfs not found at %q (build with: sudo ./scripts/build-microvms-docker.sh --target=aegishub): %w",
			rootfsPath, err,
		)
	}

	hubVMID := generateVMID("aegishub")
	spec := sandbox.SandboxSpec{
		ID:   hubVMID,
		Name: "aegishub",
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 256,
		},
		NetworkPolicy: sandbox.NetworkPolicy{
			NoNetwork:   true,
			DefaultDeny: true,
		},
		RootfsPath:  rootfsPath,
		KernelPath:  env.Config.Sandbox.KernelImage,
		InitPath:    "/sbin/aegishub",
		WorkspaceMB: 64,
	}

	if err := env.Runtime.Create(ctx, spec); err != nil {
		return nil, "", fmt.Errorf("create AegisHub VM: %w", err)
	}
	if err := env.Runtime.Start(ctx, hubVMID); err != nil {
		_ = env.Runtime.Delete(ctx, hubVMID)
		return nil, "", fmt.Errorf("start AegisHub VM: %w", err)
	}

	env.Logger.Info("AegisHub microVM started",
		zap.String("vm_id", hubVMID),
		zap.String("rootfs", rootfsPath),
	)

	hub := ipc.NewMessageHub(env.Kernel, env.Logger)

	if err := hub.RegisterVM(hubVMID, ipc.RoleHub); err != nil {
		_ = env.Runtime.Stop(ctx, hubVMID)
		_ = env.Runtime.Delete(ctx, hubVMID)
		return nil, "", fmt.Errorf("register AegisHub VM identity: %w", err)
	}

	if env.CompositionStore != nil {
		components := map[string]composition.Component{
			"aegishub": {
				Name:        "aegishub",
				Type:        composition.ComponentHub,
				Version:     "1",
				SandboxID:   hubVMID,
				ArtifactRef: rootfsPath,
				Health:      composition.HealthHealthy,
			},
		}
		if _, err := env.CompositionStore.Publish(components, "daemon", "AegisHub microVM launched"); err != nil {
			env.Logger.Warn("failed to register AegisHub in composition manifest", zap.Error(err))
		}
	}

	return hub, hubVMID, nil
}

func makeCourtVoteHandler(env *runtimeEnv, engine *court.Engine) api.Handler {
	_ = engine
	return makeUnimplementedHandler("court.vote")
}
