package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

// builderVMManager manages the lifecycle of the builder microVM.
// It ensures the builder VM is always running and restarts it if it crashes.
type builderVMManager struct {
	env            *runtimeEnv
	launcher       *builder.FirecrackerBuilderLauncher
	mu             sync.Mutex
	sandboxID      string
	running        bool
	stopCh         chan struct{}
	checkInterval  time.Duration
	restartBackoff time.Duration
	maxRestarts    int
	restartCount   int
	lastRestartKey string
}

// newBuilderVMManager creates a manager for the builder VM lifecycle.
func newBuilderVMManager(env *runtimeEnv) (*builderVMManager, error) {
	// Validate configuration
	if env.Config.Builder.WorkspaceBaseDir == "" {
		return nil, fmt.Errorf("builder workspace directory not configured")
	}
	if env.Config.Builder.RootfsTemplate == "" {
		return nil, fmt.Errorf("builder rootfs template not configured")
	}

	// Create runtime config for builder
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

	// Create builder launcher
	launcher := builder.NewFirecrackerBuilderLauncher(
		env.Runtime,
		rtCfg,
		env.LLMProxy,
		env.Logger,
	)

	return &builderVMManager{
		env:            env,
		launcher:       launcher,
		running:        false,
		stopCh:         make(chan struct{}),
		checkInterval:  30 * time.Second,
		restartBackoff: 10 * time.Second,
		maxRestarts:    5,
		restartCount:   0,
	}, nil
}

// Start launches the builder VM and begins monitoring it.
func (m *builderVMManager) Start(ctx context.Context) error {
	// Launch initial builder VM
	if err := m.launchVM(ctx); err != nil {
		return err
	}

	// Start monitoring goroutine
	go m.monitorLoop(ctx)

	return nil
}

// Stop shuts down the builder VM gracefully.
func (m *builderVMManager) Stop(ctx context.Context) error {
	close(m.stopCh)

	m.mu.Lock()
	sandboxID := m.sandboxID
	m.mu.Unlock()

	if sandboxID != "" {
		return m.launcher.StopBuilder(ctx, sandboxID)
	}

	return nil
}

// launchVM launches the builder VM and updates state.
func (m *builderVMManager) launchVM(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If already running, stop it first
	if m.sandboxID != "" {
		m.env.Logger.Info("stopping existing builder VM before relaunch",
			zap.String("sandbox_id", m.sandboxID),
		)
		if err := m.launcher.StopBuilder(ctx, m.sandboxID); err != nil {
			m.env.Logger.Warn("failed to stop existing builder VM",
				zap.String("sandbox_id", m.sandboxID),
				zap.Error(err),
			)
		}
		m.sandboxID = ""
		m.running = false
	}

	m.env.Logger.Info("launching builder microVM",
		zap.Int("restart_count", m.restartCount),
	)

	// Launch the builder VM
	sandboxID, err := m.launcher.LaunchBuilder(ctx)
	if err != nil {
		m.env.Logger.Error("failed to launch builder VM",
			zap.Error(err),
			zap.Int("restart_count", m.restartCount),
		)
		return fmt.Errorf("failed to launch builder VM: %w", err)
	}

	m.sandboxID = sandboxID
	m.running = true

	m.env.Logger.Info("builder microVM launched successfully",
		zap.String("sandbox_id", sandboxID),
		zap.Int("restart_count", m.restartCount),
	)

	return nil
}

// monitorLoop periodically checks if the builder VM is running and restarts if needed.
func (m *builderVMManager) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.env.Logger.Info("builder VM monitor shutting down (context cancelled)")
			return
		case <-m.stopCh:
			m.env.Logger.Info("builder VM monitor shutting down (stop signal)")
			return
		case <-ticker.C:
			if err := m.checkAndRestart(ctx); err != nil {
				m.env.Logger.Error("builder VM monitor error", zap.Error(err))
			}
		}
	}
}

// checkAndRestart verifies the builder VM is running and restarts if needed.
func (m *builderVMManager) checkAndRestart(ctx context.Context) error {
	m.mu.Lock()
	sandboxID := m.sandboxID
	m.mu.Unlock()

	if sandboxID == "" {
		// No VM running, try to launch
		m.env.Logger.Warn("builder VM not running, attempting launch")
		return m.attemptRestart(ctx, "no VM running")
	}

	// Check if VM is still alive
	status, err := m.launcher.GetStatus(ctx, sandboxID)
	if err != nil || status != "running" {
		m.env.Logger.Warn("builder VM health check failed",
			zap.String("sandbox_id", sandboxID),
			zap.String("status", status),
			zap.Error(err),
		)
		return m.attemptRestart(ctx, fmt.Sprintf("health check failed: %v", err))
	}

	// Check if there are implementing proposals but builder isn't responding
	// (This is a secondary check - the builder agent should be polling)
	if err := m.checkForStalledProposals(ctx); err != nil {
		m.env.Logger.Warn("detected stalled proposals, may need restart",
			zap.Error(err),
		)
	}

	// Reset restart count on successful check
	m.mu.Lock()
	if m.running {
		m.restartCount = 0
	}
	m.mu.Unlock()

	return nil
}

// attemptRestart tries to restart the builder VM with backoff.
func (m *builderVMManager) attemptRestart(ctx context.Context, reason string) error {
	m.mu.Lock()
	// Generate a restart key based on time window to prevent rapid restart loops
	restartKey := time.Now().Truncate(5 * time.Minute).Format(time.RFC3339)
	if restartKey != m.lastRestartKey {
		// New time window, reset count
		m.restartCount = 0
		m.lastRestartKey = restartKey
	}
	m.restartCount++
	count := m.restartCount
	m.mu.Unlock()

	if count > m.maxRestarts {
		return fmt.Errorf("builder VM exceeded max restarts (%d) in current time window", m.maxRestarts)
	}

	m.env.Logger.Info("restarting builder VM",
		zap.String("reason", reason),
		zap.Int("attempt", count),
		zap.Int("max_restarts", m.maxRestarts),
	)

	// Backoff before restart
	select {
	case <-time.After(m.restartBackoff):
	case <-ctx.Done():
		return ctx.Err()
	}

	return m.launchVM(ctx)
}

// checkForStalledProposals checks if there are proposals in StatusImplementing
// that haven't been picked up by the builder (possible stall indicator).
func (m *builderVMManager) checkForStalledProposals(ctx context.Context) error {
	summaries, err := m.env.ProposalStore.List()
	if err != nil {
		return fmt.Errorf("failed to list proposals: %w", err)
	}

	stalledCount := 0
	for _, summary := range summaries {
		if summary.Status != proposal.StatusImplementing {
			continue
		}

		// Load full proposal to check build metadata
		prop, err := m.env.ProposalStore.Get(summary.ID)
		if err != nil {
			continue
		}

		// If proposal is implementing but hasn't been picked up in 2 minutes, it's stalled
		if prop.BuildStartedAt == nil {
			elapsed := time.Since(prop.UpdatedAt)
			if elapsed > 2*time.Minute {
				stalledCount++
				m.env.Logger.Warn("detected stalled proposal",
					zap.String("proposal_id", prop.ID),
					zap.Duration("elapsed", elapsed),
				)
			}
		}
	}

	if stalledCount > 0 {
		return fmt.Errorf("found %d stalled proposals", stalledCount)
	}

	return nil
}

// EnsureRunning checks if the builder VM is running and launches it if not.
// This is called by external triggers (e.g., when a proposal transitions to implementing).
func (m *builderVMManager) EnsureRunning(ctx context.Context) error {
	m.mu.Lock()
	running := m.running
	sandboxID := m.sandboxID
	m.mu.Unlock()

	if running && sandboxID != "" {
		// Already running, verify it's healthy
		status, err := m.launcher.GetStatus(ctx, sandboxID)
		if err == nil && status == "running" {
			return nil
		}
	}

	// Not running or unhealthy, launch it
	return m.launchVM(ctx)
}
