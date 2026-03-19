package main

import (
	"fmt"

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

	// Initialize kernel singleton
	kern, err := kernel.GetInstance(logger)
	if err != nil {
		return fmt.Errorf("failed to initialize kernel: %w", err)
	}

	logger.Info("AegisClaw kernel started successfully",
		zap.String("public_key", fmt.Sprintf("%x", kern.PublicKey())))

	// TODO: Start message-hub microVM
	// This will be implemented in later tasks

	fmt.Println("AegisClaw kernel started. Message-hub initialization pending.")

	return nil
}
