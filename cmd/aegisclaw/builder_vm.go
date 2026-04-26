package main

import (
	"context"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

// launchBuilderVM starts a builder agent in an isolated microVM.
// The builder VM monitors for proposals in "implementing" status and executes
// the pipeline to generate code, similar to how reviewer personas run in VMs.
func launchBuilderVM(ctx context.Context, env *runtimeEnv) error {
	// Validate configuration
	if env.Config.Builder.WorkspaceBaseDir == "" {
		env.Logger.Warn("builder workspace directory not configured, builder VM disabled")
		return nil
	}
	if env.Config.Builder.RootfsTemplate == "" {
		env.Logger.Warn("builder rootfs template not configured, builder VM disabled")
		return nil
	}

	env.Logger.Info("launching builder microVM",
		zap.String("workspace_dir", env.Config.Builder.WorkspaceBaseDir),
		zap.String("rootfs_template", env.Config.Builder.RootfsTemplate),
	)

	// Create runtime config for builder (same pattern as reviewer launcher)
	rtCfg := sandbox.RuntimeConfig{
		FirecrackerBin: env.Config.Firecracker.Bin,
		JailerBin:      env.Config.Jailer.Bin,
		KernelImage:    env.Config.Sandbox.KernelImage,
		RootfsTemplate: env.Config.Builder.RootfsTemplate,
		ChrootBaseDir:  env.Config.Sandbox.ChrootBase,
		StateDir:       env.Config.Sandbox.StateDir,
	}

	// Ensure LLM proxy is available
	if env.LLMProxy == nil {
		allowedModels := llm.AllowedModelsFromRegistry()
		env.LLMProxy = llm.NewOllamaProxyWithHTTPClient(allowedModels, "", env.OllamaHTTPClient, env.Kernel, env.Logger)
	}

	// Create builder launcher (similar to FirecrackerLauncher for reviewers)
	launcher := builder.NewFirecrackerBuilderLauncher(
		env.Runtime,
		rtCfg,
		env.LLMProxy,
		env.Logger,
	)

	// Launch the builder VM
	sandboxID, err := launcher.LaunchBuilder(ctx)
	if err != nil {
		return fmt.Errorf("failed to launch builder VM: %w", err)
	}

	env.Logger.Info("builder microVM launched successfully",
		zap.String("sandbox_id", sandboxID),
	)

	// The builder VM now runs autonomously, polling for work
	// It will handle proposals in "implementing" status automatically
	// No need to manage it further here - it runs until the daemon stops

	return nil
}
