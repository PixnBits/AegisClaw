package main

import (
	"context"
	"encoding/json"
	"fmt"

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

	apiSrv.Handle("court.review", makeCourtReviewHandler(env, courtEngine))
	apiSrv.Handle("court.vote", makeCourtVoteHandler(env, courtEngine))

	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

	fmt.Println("AegisClaw kernel started.")
	<-make(chan struct{})
	return nil
}

func makeCourtReviewHandler(env *runtimeEnv, engine *court.Engine) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.CourtReviewRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.ProposalID == "" {
			return &api.Response{Error: "proposal_id is required"}
		}

		session, err := engine.Review(ctx, req.ProposalID)
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
