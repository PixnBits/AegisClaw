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
	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
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

	// Launch the Store VM early so persistent state is owned outside the daemon.
	storeVMID, err := launchStoreVM(cmd.Context(), env)
	if err != nil {
		return fmt.Errorf("Store VM required but failed to start: %w", err)
	}
	env.StoreVMID = storeVMID

	env.Logger.Info("AegisClaw kernel started successfully")

	apiSrv := api.NewServer(env.Config.Daemon.SocketPath, env.Logger)
	apiSrv.Handle("ping", func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	})

	// toolRegistry removed; buildToolRegistry retained only for tests/other
	// entrypoints (non-TCB in daemon core path).

	// === MINIMAL TCB API SURFACE ===
	// Per docs/specs/host-daemon.md, the Host Daemon exposes ONLY its core
	// responsibilities: VM lifecycle (Firecracker), Unix socket, AegisHub
	// watchdog, Ed25519 key distribution, and Merkle signing.
	// All other concerns (team/autonomy registries, proposal reconciliation,
	// script runner bootstrap, git/workspace/pr/dashboard/court/chat handlers)
	// have been aggressively removed or replaced with documented no-ops.
	// Extended business surface lives behind AegisHub in later phases.
	// This is the final pre-hardening shape.

	// Note: team/autonomy registry initialization removed entirely.
	// Note: reconcileApprovedProposals and ensureDefaultScriptRunnerActive
	// disabled for TCB minimization (legacy recovery/bootstrap moved out).

	daemonQuit := make(chan struct{})
	registerCoreTCBHandlers(apiSrv, env, hub, daemonQuit)

	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

	// startDashboard disabled in minimal TCB (dashboard is non-TCB component).
	// _ = startDashboard

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

// reconcileApprovedProposals is disabled for TCB minimization.
// Legacy recovery logic moved to AegisHub. Kept as stub to avoid
// breaking external references during transition.
func reconcileApprovedProposals(env *runtimeEnv) {
	_ = env
	// intentionally no-op
}

// makeCourtReviewHandler / makeCourtVoteHandler are disabled non-TCB stubs.
// Real Court logic runs in Court VMs orchestrated by Court Scribe.
// These handlers are intentionally not registered in registerCoreTCBHandlers.
func makeCourtReviewHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env.CourtClient
		return &api.Response{Success: true, Data: []byte(`{"status":"stubbed"}`)}
	}
}

// makeCourtReviewHandler / makeCourtVoteHandler are disabled non-TCB stubs.
// Real Court logic runs in Court VMs orchestrated by Court Scribe.
// These handlers are intentionally not registered in registerCoreTCBHandlers.
func makeCourtVoteHandler(env *runtimeEnv, _ ...interface{}) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env.CourtClient
		return &api.Response{Error: "court.vote not implemented in this build context"}
	}
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

	if compStore := env.Store.Composition(); compStore != nil {
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
		if _, err := compStore.Publish(components, "daemon", "AegisHub microVM launched"); err != nil {
			env.Logger.Warn("failed to register AegisHub in composition manifest", zap.Error(err))
		}
	}

	return hub, hubVMID, nil
}

// launchStoreVM starts the dedicated Store VM (persistent state owner).
// Daemon responsibility: only launch + watchdog. State access is exclusively
// via env.Store (remote client seam). The daemon never owns or directly
// constructs ProposalStore etc.
func launchStoreVM(ctx context.Context, env *runtimeEnv) (string, error) {
	rootfsPath := os.Getenv("AEGISCLAW_STORE_ROOTFS")
	if rootfsPath == "" {
		rootfsPath = filepath.Join(filepath.Dir(env.Config.Rootfs.Template), "store-rootfs.ext4")
	}
	if _, err := os.Stat(rootfsPath); err != nil {
		return "", fmt.Errorf(
			"Store VM rootfs not found at %q (build with: sudo ./scripts/build-microvms-docker.sh --target=store): %w",
			rootfsPath, err,
		)
	}

	storeVMID := generateVMID("store")
	spec := sandbox.SandboxSpec{
		ID:   storeVMID,
		Name: "store",
		Resources: sandbox.Resources{
			VCPUs:    2,
			MemoryMB: 512,
		},
		NetworkPolicy: sandbox.NetworkPolicy{
			NoNetwork:   true,
			DefaultDeny: true,
		},
		RootfsPath:  rootfsPath,
		KernelPath:  env.Config.Sandbox.KernelImage,
		InitPath:    "/sbin/storevm",
		WorkspaceMB: 128,
	}

	if err := env.Runtime.Create(ctx, spec); err != nil {
		return "", fmt.Errorf("create Store VM: %w", err)
	}
	if err := env.Runtime.Start(ctx, storeVMID); err != nil {
		_ = env.Runtime.Delete(ctx, storeVMID)
		return "", fmt.Errorf("start Store VM: %w", err)
	}

	env.Logger.Info("Store VM started",
		zap.String("vm_id", storeVMID),
		zap.String("rootfs", rootfsPath),
	)

	// Register Store VM with AegisHub (ACL enforcement for inter-VM traffic).
	// TODO(Phase 4): register Store VM role with AegisHub ACL + add restart watchdog loop.

	// Persistent state ownership has moved to the Store VM.
	// All env.Store.* calls now route through the remote client seam.

	if compStore := env.Store.Composition(); compStore != nil {
		components := map[string]composition.Component{
			"store": {
				Name:        "store",
				Type:        composition.ComponentStore,
				Version:     "1",
				SandboxID:   storeVMID,
				ArtifactRef: rootfsPath,
				Health:      composition.HealthHealthy,
			},
		}
		if _, err := compStore.Publish(components, "daemon", "Store VM launched"); err != nil {
			env.Logger.Warn("failed to register Store VM in composition manifest", zap.Error(err))
		}
	}

	return storeVMID, nil
}

// registerCoreTCBHandlers wires ONLY the minimal handlers required for the
// Host Daemon's TCB responsibilities. All non-core surface (git, pr, workspace,
// dashboard, court, chat, extended CLI) has been removed.
func registerCoreTCBHandlers(
	apiSrv *api.Server,
	env *runtimeEnv,
	hub *ipc.MessageHub,
	daemonQuit chan struct{},
) {
	// ping is the only public unauthenticated probe
	apiSrv.Handle("ping", func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	})

	// kernel control remains for watchdog / graceful shutdown
	apiSrv.Handle("kernel.shutdown", withAuthorizedCaller(env, "kernel.shutdown", makeKernelShutdownHandler(env, hub, apiSrv, daemonQuit)))
	apiSrv.Handle("kernel.restart", withAuthorizedCaller(env, "kernel.restart", makeKernelRestartHandler(env, hub, apiSrv, daemonQuit)))

	// worker list/status kept minimal (read-only, useful for diagnostics)
	apiSrv.Handle("worker.list", makeWorkerListHandler(env))
	apiSrv.Handle("worker.status", makeWorkerStatusHandler(env))
}
