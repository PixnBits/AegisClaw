package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/provision"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func runStart(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

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

	// Initialize and start the message-hub
	hub := ipc.NewMessageHub(env.Kernel, env.Logger)
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
	)

	// Start the Unix socket API server so CLI commands can talk to the daemon.
	apiSrv := api.NewServer(env.Config.Daemon.SocketPath, env.Logger)
	apiSrv.Handle("ping", func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	})

	// Create the court engine once and share it across handlers so session
	// state persists between review and vote calls.
	courtEngine, err := initCourtEngine(env)
	if err != nil {
		hub.Stop()
		return fmt.Errorf("failed to init court engine: %w", err)
	}
	apiSrv.Handle("court.review", makeCourtReviewHandler(env, courtEngine))
	apiSrv.Handle("court.vote", makeCourtVoteHandler(env, courtEngine))
	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

	fmt.Println("AegisClaw kernel started.")
	fmt.Printf("  Message-Hub: %s\n", hub.State())
	fmt.Printf("  IPC Routes: %v\n", hub.Router().RegisteredRoutes())
	fmt.Printf("  API Socket: %s\n", env.Config.Daemon.SocketPath)

	// Wait for shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
			return &api.Response{Error: "court review failed: " + err.Error()}
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

		respData, _ := json.Marshal(session)
		return &api.Response{Success: true, Data: respData}
	}
}
