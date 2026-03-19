package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func runStatus(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	fmt.Printf("AegisClaw Kernel Status:\n")
	fmt.Printf("  Public Key: %x\n", env.Kernel.PublicKey())
	fmt.Printf("  Firecracker Binary: %s\n", env.Config.Firecracker.Bin)
	fmt.Printf("  Jailer Binary: %s\n", env.Config.Jailer.Bin)
	fmt.Printf("  Rootfs Template: %s\n", env.Config.Rootfs.Template)
	fmt.Printf("  Kernel Image: %s\n", env.Config.Sandbox.KernelImage)
	fmt.Printf("  Audit Directory: %s\n", env.Config.Audit.Dir)
	fmt.Printf("  Sandbox State: %s\n", env.Config.Sandbox.StateDir)
	fmt.Printf("  Control Plane Listeners: %d\n", env.Kernel.ControlPlane().ActiveListeners())

	sandboxes, err := env.Runtime.List(context.Background())
	if err == nil {
		running := 0
		for _, sb := range sandboxes {
			if sb.State == "running" {
				running++
			}
		}
		fmt.Printf("  Sandboxes: %d total, %d running\n", len(sandboxes), running)
	}

	skills := env.Registry.List()
	active := 0
	for _, sk := range skills {
		if sk.State == "active" {
			active++
		}
	}
	fmt.Printf("  Skills: %d registered, %d active\n", len(skills), active)
	rootHash := env.Registry.RootHash()
	if rootHash != "" {
		fmt.Printf("  Registry Root: %s\n", rootHash[:16])
	}

	// Merkle audit chain info
	auditLog := env.Kernel.AuditLog()
	fmt.Printf("  Audit Entries: %d\n", auditLog.EntryCount())
	if lastHash := auditLog.LastHash(); lastHash != "" {
		fmt.Printf("  Audit Chain Head: %s\n", lastHash[:16])
	}

	return nil
}
