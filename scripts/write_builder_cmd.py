#!/usr/bin/env python3
"""Writes cmd/aegisclaw/builder_cmd.go — builder status CLI command."""
import os

code = r'''package main

import (
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// builderCmd is the top-level builder command group
var builderCmd = &cobra.Command{
	Use:   "builder",
	Short: "Manage builder sandboxes",
	Long:  `Commands for managing dedicated builder Firecracker sandboxes used for code generation.`,
}

// builderStatusCmd shows builder sandbox status
var builderStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show builder sandbox status",
	Long:  `Displays the current status of all builder sandboxes including active builds.`,
	RunE:  runBuilderStatus,
}

func init() {
	builderCmd.AddCommand(builderStatusCmd)
}

func initBuilderRuntime(env *runtimeEnv) (*builder.BuilderRuntime, error) {
	cfg := builder.BuilderConfig{
		RootfsTemplate:      env.Config.Builder.RootfsTemplate,
		WorkspaceBaseDir:    env.Config.Builder.WorkspaceBaseDir,
		MaxConcurrentBuilds: env.Config.Builder.MaxConcurrentBuilds,
		BuildTimeout:        time.Duration(env.Config.Builder.BuildTimeoutMinutes) * time.Minute,
	}
	return builder.NewBuilderRuntime(cfg, env.Runtime, env.Kernel, env.Logger)
}

func runBuilderStatus(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	br, err := initBuilderRuntime(env)
	if err != nil {
		return fmt.Errorf("failed to initialize builder runtime: %w", err)
	}

	builders := br.ListBuilders()
	active := br.ActiveBuilders()

	env.Logger.Info("builder status retrieved",
		zap.Int("total", len(builders)),
		zap.Int("active", len(active)),
	)

	fmt.Printf("Builder Sandboxes: %d total, %d active\n\n", len(builders), len(active))

	if len(builders) == 0 {
		fmt.Println("No builder sandboxes found.")
		return nil
	}

	fmt.Printf("%-36s  %-12s  %-36s  %-20s\n", "ID", "STATE", "PROPOSAL", "STARTED")
	fmt.Printf("%-36s  %-12s  %-36s  %-20s\n", "------------------------------------", "------------", "------------------------------------", "--------------------")

	for _, b := range builders {
		started := "-"
		if b.StartedAt != nil {
			started = b.StartedAt.Format(time.RFC3339)
		}
		fmt.Printf("%-36s  %-12s  %-36s  %-20s\n", b.ID, b.State, b.ProposalID, started)
		if b.Error != "" {
			fmt.Printf("  Error: %s\n", b.Error)
		}
	}

	return nil
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'cmd', 'aegisclaw', 'builder_cmd.go')
outpath = os.path.abspath(outpath)
os.makedirs(os.path.dirname(outpath), exist_ok=True)
with open(outpath, 'w') as f:
    f.write(code)
print(f"builder_cmd.go: {len(code)} bytes -> {outpath}")
