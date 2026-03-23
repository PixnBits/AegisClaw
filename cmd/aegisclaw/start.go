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
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
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
	apiSrv.Handle("skill.activate", makeSkillActivateHandler(env))
	apiSrv.Handle("skill.deactivate", makeSkillDeactivateHandler(env))
	apiSrv.Handle("skill.invoke", makeSkillInvokeHandler(env))
	apiSrv.Handle("skill.list", makeSkillListHandler(env))
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

// makeSkillActivateHandler returns an API handler that activates a skill by
// spinning up a Firecracker microVM inside the daemon process.
func makeSkillActivateHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
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

		sandboxID := uuid.New().String()
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
			RootfsPath: env.Config.Rootfs.Template,
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

		entry, err := env.Registry.Register(req.Name, sandboxID, map[string]string{
			"sandbox_name": spec.Name,
			"guest_ip":     info.GuestIP,
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

		return &api.Response{Success: true}
	}
}

// makeSkillInvokeHandler sends a tool invocation request to a running skill VM.
func makeSkillInvokeHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
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
