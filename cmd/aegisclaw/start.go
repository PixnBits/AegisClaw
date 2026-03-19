package main

import (
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func runStart(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("failed to initialize kernel: %w", err)
	}

	action := kernel.NewAction(kernel.ActionKernelStart, "kernel", nil)
	if _, err := kern.SignAndLog(action); err != nil {
		return fmt.Errorf("failed to log kernel start: %w", err)
	}

	logger.Info("AegisClaw kernel started successfully",
		zap.String("public_key", fmt.Sprintf("%x", kern.PublicKey())))

	fmt.Println("AegisClaw kernel started.")
	return nil
}
