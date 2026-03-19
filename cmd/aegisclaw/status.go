package main

import (
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func runStatus(cmd *cobra.Command, args []string) error {
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()

	cfg, err := config.Load(logger)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	kern, err := kernel.GetInstance(logger, cfg.Audit.Dir)
	if err != nil {
		return fmt.Errorf("failed to get kernel instance: %w", err)
	}

	fmt.Printf("AegisClaw Kernel Status:\n")
	fmt.Printf("  Public Key: %x\n", kern.PublicKey())
	fmt.Printf("  Firecracker Binary: %s\n", cfg.Firecracker.Bin)
	fmt.Printf("  Jailer Binary: %s\n", cfg.Jailer.Bin)
	fmt.Printf("  Rootfs Template: %s\n", cfg.Rootfs.Template)
	fmt.Printf("  Audit Directory: %s\n", cfg.Audit.Dir)
	fmt.Printf("  Control Plane Listeners: %d\n", kern.ControlPlane().ActiveListeners())

	return nil
}
