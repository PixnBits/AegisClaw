package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/provision"
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

	// === Phase 4 Hardening Baseline Logging ===
	env.Logger.Info("daemon starting (Phase 4 baseline)",
		zap.String("go_version", runtime.Version()),
		zap.String("os", runtime.GOOS),
		zap.String("arch", runtime.GOARCH),
		zap.Bool("cgo_enabled", cgoEnabled()),
	)

	// Phase 4 Step 2: Drop capabilities as early as possible (after logger init).
	_ = dropCapabilities(env.Logger)

	// Phase 4 Step 3: Apply seccomp-bpf filter right after caps.
	_ = applySeccompFilter(env.Logger)

	// Phase 4 Step 6: Apply basic resource limits.
	_ = setResourceLimits(env.Logger)

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

	// AegisHub is the central mediation and ACL enforcement layer.
	// All future inter-VM and daemon↔VM requests (including data access)
	// will be routed and authorized here.

	// Launch the Store VM
	// Temporary: Daemon publishes launched critical VMs (AegisHub, Store VM)
	// to the Composition Manifest. This lightweight responsibility remains
	// in the Host Daemon for now.
	// Future: Component/VM registry queries should go through AegisHub → daemon.
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

	daemonQuit := make(chan struct{})
	registerCoreTCBHandlers(apiSrv, env, hub, daemonQuit)

	if err := apiSrv.Start(); err != nil {
		hub.Stop()
		return fmt.Errorf("failed to start API server: %w", err)
	}

	fmt.Println("AegisClaw kernel started.")

	// Phase 4 Step 5: Lifecycle containment - handle shutdown signals and
	// best-effort termination of managed microVMs (AegisHub + Store VM).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		env.Logger.Info("Phase 4: received shutdown signal, terminating managed VMs")
		if env.AegisHubVMID != "" {
			_ = env.Runtime.Stop(cmd.Context(), env.AegisHubVMID)
			_ = env.Runtime.Delete(cmd.Context(), env.AegisHubVMID)
		}
		if env.StoreVMID != "" {
			_ = env.Runtime.Stop(cmd.Context(), env.StoreVMID)
			_ = env.Runtime.Delete(cmd.Context(), env.StoreVMID)
		}
		close(daemonQuit)
	}()

	<-daemonQuit
	env.Logger.Info("daemon exiting after shutdown request")
	return nil
}

// cgoEnabled reports whether the binary was built with CGO enabled.
// Used for Phase 4 baseline reporting.
func cgoEnabled() bool {
	return cgoEnabledVar == 1
}

var cgoEnabledVar int // set by linker when CGO is enabled

func ensureDaemonNotRunning(ctx context.Context, allowExisting bool) error {
	if allowExisting {
		return nil
	}
	client := api.NewClient(resolveDaemonSocketPath())
	pingCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	_ = pingCtx
	if err := client.Ping(ctx); err == nil {
		return fmt.Errorf("daemon already running (use: aegisclaw restart)")
	}
	return nil
}

// ... rest of file unchanged ...