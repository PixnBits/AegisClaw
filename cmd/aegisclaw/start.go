package main

import (
	"fmt"

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
	return nil
}
