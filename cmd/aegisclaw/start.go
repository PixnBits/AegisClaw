package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func runStart(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

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

	fmt.Println("AegisClaw kernel started.")
	fmt.Printf("  Message-Hub: %s\n", hub.State())
	fmt.Printf("  IPC Routes: %v\n", hub.Router().RegisteredRoutes())

	// Wait for shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("Press Ctrl+C to stop.")
	<-ctx.Done()

	fmt.Println("\nShutting down...")
	env.Logger.Info("shutdown signal received, cleaning up")

	// Clean up all running sandboxes
	env.Runtime.Cleanup(context.Background())

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
