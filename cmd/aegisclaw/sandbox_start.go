package main

import (
	"context"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func runSandboxStart(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	ctx := context.Background()

	spec := sandbox.SandboxSpec{
		ID:   uuid.New().String(),
		Name: name,
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
		return fmt.Errorf("failed to create sandbox: %w", err)
	}

	if err := env.Runtime.Start(ctx, spec.ID); err != nil {
		return fmt.Errorf("failed to start sandbox: %w", err)
	}

	info, err := env.Runtime.Status(ctx, spec.ID)
	if err != nil {
		return fmt.Errorf("failed to get sandbox status: %w", err)
	}

	env.Logger.Info("sandbox started",
		zap.String("name", name),
		zap.String("id", spec.ID),
		zap.Int("pid", info.PID),
	)

	fmt.Printf("Sandbox '%s' started (id=%s, pid=%d)\n", name, spec.ID, info.PID)
	return nil
}
