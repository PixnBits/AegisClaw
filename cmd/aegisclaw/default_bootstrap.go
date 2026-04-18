package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const defaultScriptRunnerSkill = "default-script-runner"

// scriptRunnerIdleTimeout is the period of inactivity after which the default
// script runner sandbox is automatically shut down to free resources.
// The sandbox is re-created on demand by ensureDefaultScriptRunnerActive the
// next time script.run is called, so shutting it down when idle is safe.
const scriptRunnerIdleTimeout = 15 * time.Minute

// scriptRunnerIdleCheckInterval controls how often the idle daemon polls.
const scriptRunnerIdleCheckInterval = 5 * time.Minute

// ensureDefaultScriptRunnerActive guarantees the built-in script runner exists
// as an active sandboxed skill so script.run can execute without manual setup.
func ensureDefaultScriptRunnerActive(ctx context.Context, env *runtimeEnv) error {
	if env == nil {
		return nil
	}
	if env.SafeMode.Load() {
		return nil
	}

	if existing, ok := env.Registry.Get(defaultScriptRunnerSkill); ok && existing != nil && existing.State == sandbox.SkillStateActive {
		info, err := env.Runtime.Status(ctx, existing.SandboxID)
		if err == nil && info != nil && info.State == sandbox.StateRunning {
			if err := waitForSandboxGuestReady(ctx, env, existing.SandboxID); err == nil {
				return nil
			}
		}
		if err := env.Registry.Deactivate(defaultScriptRunnerSkill); err != nil {
			env.Logger.Warn("bootstrap: failed to deactivate stale default script runner entry", zap.Error(err))
		}
	}

	sandboxID := generateVMID("skill")
	spec := sandbox.SandboxSpec{
		ID:   sandboxID,
		Name: fmt.Sprintf("skill-%s", defaultScriptRunnerSkill),
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 256,
		},
		NetworkPolicy: sandbox.NetworkPolicy{NoNetwork: true, DefaultDeny: true},
		RootfsPath:    env.Config.Rootfs.Template,
	}

	if err := env.Runtime.Create(ctx, spec); err != nil {
		env.Logger.Warn("bootstrap: failed to create default script runner sandbox", zap.Error(err))
		return err
	}
	if err := env.Runtime.Start(ctx, sandboxID); err != nil {
		_ = env.Runtime.Delete(ctx, sandboxID)
		env.Logger.Warn("bootstrap: failed to start default script runner sandbox", zap.Error(err))
		return err
	}
	if err := waitForSandboxGuestReady(ctx, env, sandboxID); err != nil {
		_ = env.Runtime.Stop(context.Background(), sandboxID)
		_ = env.Runtime.Delete(context.Background(), sandboxID)
		env.Logger.Warn("bootstrap: default script runner guest did not become ready", zap.Error(err))
		return err
	}

	info, statusErr := env.Runtime.Status(ctx, sandboxID)
	if statusErr != nil {
		env.Logger.Warn("bootstrap: failed to read default script runner sandbox status", zap.Error(statusErr))
	}

	entry, err := env.Registry.Register(defaultScriptRunnerSkill, sandboxID, map[string]string{
		"sandbox_name": spec.Name,
	})
	if err != nil {
		_ = env.Runtime.Stop(ctx, sandboxID)
		_ = env.Runtime.Delete(ctx, sandboxID)
		env.Logger.Warn("bootstrap: failed to register default script runner", zap.Error(err))
		return err
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"skill_name": defaultScriptRunnerSkill,
		"sandbox_id": sandboxID,
		"version":    entry.Version,
		"hash":       entry.MerkleHash,
	})
	action := kernel.NewAction(kernel.ActionSkillActivate, "system", payload)
	_, _ = env.Kernel.SignAndLog(action)

	fields := []zap.Field{
		zap.String("skill", defaultScriptRunnerSkill),
		zap.String("sandbox_id", sandboxID),
	}
	if statusErr == nil {
		fields = append(fields, zap.Int("pid", info.PID))
	}
	env.Logger.Info("bootstrap: default script runner activated", fields...)
	// Record activation time so the idle daemon starts the inactivity window
	// from now, not from the Unix epoch (which would cause immediate shutdown).
	env.ScriptRunnerLastUsed.Store(time.Now().UnixNano())
	return nil
}

// startScriptRunnerIdleDaemon launches a background goroutine that stops and
// destroys the default script runner sandbox after scriptRunnerIdleTimeout of
// inactivity.  This prevents a persistent microVM from consuming resources
// between chat sessions.  The sandbox is re-created on demand by
// ensureDefaultScriptRunnerActive the next time script.run is called.
func startScriptRunnerIdleDaemon(ctx context.Context, env *runtimeEnv) {
	if env == nil || env.Runtime == nil || env.Registry == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(scriptRunnerIdleCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				maybeStopIdleScriptRunner(ctx, env)
			}
		}
	}()
}

// maybeStopIdleScriptRunner stops and deletes the default script runner sandbox
// if it has been idle for longer than scriptRunnerIdleTimeout.
// It logs the shutdown to the immutable audit trail.
func maybeStopIdleScriptRunner(ctx context.Context, env *runtimeEnv) {
	if env == nil || env.Runtime == nil || env.Registry == nil {
		return
	}
	entry, ok := env.Registry.Get(defaultScriptRunnerSkill)
	if !ok || entry.State != sandbox.SkillStateActive {
		return // script runner not currently active; nothing to clean up
	}

	lastUsedNano := env.ScriptRunnerLastUsed.Load()
	if lastUsedNano == 0 {
		return // never used; no activation recorded, do not shut down
	}
	lastUsed := time.Unix(0, lastUsedNano)
	if time.Since(lastUsed) < scriptRunnerIdleTimeout {
		return // still within the idle window
	}

	idleDuration := time.Since(lastUsed).Round(time.Second)
	env.Logger.Info("script runner idle: shutting down to free resources",
		zap.Duration("idle_duration", idleDuration),
		zap.String("sandbox_id", entry.SandboxID),
	)

	// Stop the sandbox process, delete its state directory.
	if err := env.Runtime.Stop(ctx, entry.SandboxID); err != nil {
		env.Logger.Warn("script runner idle: stop failed",
			zap.String("sandbox_id", entry.SandboxID),
			zap.Error(err),
		)
	}
	if err := env.Runtime.Delete(ctx, entry.SandboxID); err != nil {
		env.Logger.Warn("script runner idle: delete failed",
			zap.String("sandbox_id", entry.SandboxID),
			zap.Error(err),
		)
	}

	// Mark the registry entry as stopped so ensureDefaultScriptRunnerActive
	// knows to create a fresh sandbox on the next script.run call.
	if err := env.Registry.Deactivate(defaultScriptRunnerSkill); err != nil {
		env.Logger.Warn("script runner idle: deactivate registry failed", zap.Error(err))
	}

	// Clear the last-used timestamp so we don't repeatedly try to stop an
	// already-stopped sandbox on subsequent idle checks.
	env.ScriptRunnerLastUsed.Store(0)

	// Audit-log the idle shutdown to the immutable Merkle trail.
	auditPayload, marshalErr := json.Marshal(map[string]interface{}{
		"skill_name":        defaultScriptRunnerSkill,
		"sandbox_id":        entry.SandboxID,
		"action":            "idle_shutdown",
		"idle_duration_sec": int(idleDuration.Seconds()),
	})
	if marshalErr != nil {
		env.Logger.Error("script runner idle: failed to marshal audit payload",
			zap.Error(marshalErr),
		)
	}
	act := kernel.NewAction(kernel.ActionSkillDeactivate, "daemon", auditPayload)
	if _, logErr := env.Kernel.SignAndLog(act); logErr != nil {
		env.Logger.Error("script runner idle: failed to write audit log entry",
			zap.String("sandbox_id", entry.SandboxID),
			zap.Error(logErr),
		)
	}

	env.Logger.Info("script runner idle: shutdown complete",
		zap.String("sandbox_id", entry.SandboxID),
	)
}

func waitForSandboxGuestReady(ctx context.Context, env *runtimeEnv, sandboxID string) error {
	if env == nil {
		return fmt.Errorf("runtime environment is nil")
	}
	const (
		attempts = 30
		delay    = 500 * time.Millisecond
	)
	for attempt := 0; attempt < attempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := env.Runtime.SendToVM(pingCtx, sandboxID, map[string]interface{}{
			"id":      uuid.New().String(),
			"type":    "status",
			"payload": map[string]interface{}{},
		})
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("guest-agent readiness check timed out for sandbox %s", sandboxID)
}
