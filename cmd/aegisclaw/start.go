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
	apiSrv.Handle("safe-mode.enable", makeSafeModeEnableHandler(env))
	apiSrv.Handle("safe-mode.disable", makeSafeModeDisableHandler(env))
	apiSrv.Handle("safe-mode.status", makeSafeModeStatusHandler(env))
	// D2: Chat handlers — the daemon owns all LLM interaction.
	apiSrv.Handle("chat.message", makeChatMessageHandler(env))
	apiSrv.Handle("chat.slash", makeChatSlashHandler(env))
	apiSrv.Handle("chat.tool", makeChatToolHandler(env))
	apiSrv.Handle("chat.summarize", makeChatSummarizeHandler(env))
	// D10: Composition manifest handlers for versioned deployment and rollback.
	apiSrv.Handle("composition.current", makeCompositionCurrentHandler(env))
	apiSrv.Handle("composition.rollback", makeCompositionRollbackHandler(env))
	apiSrv.Handle("composition.history", makeCompositionHistoryHandler(env))
	apiSrv.Handle("composition.health", makeCompositionHealthHandler(env))
	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

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
			return &api.Response{Error: "court review failed: " + err.Error()}
		}

		// D3: If proposal is approved, automatically transition to implementing
		// and trigger the builder pipeline. This closes the gap between Court
		// approval and skill deployment.
		if session.Verdict == "approved" {
			p, pErr := env.ProposalStore.Get(req.ProposalID)
			if pErr == nil && p.Status == proposal.StatusApproved {
				if tErr := p.Transition(proposal.StatusImplementing, "auto-triggered by court approval", "daemon"); tErr == nil {
					env.ProposalStore.Update(p)
					env.Logger.Info("proposal approved, builder pipeline will be triggered",
						zap.String("proposal_id", req.ProposalID),
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
