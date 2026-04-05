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

	sandboxID := uuid.New().String()
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

	info, err := env.Runtime.Status(ctx, sandboxID)
	if err != nil {
		env.Logger.Warn("bootstrap: failed to read default script runner sandbox status", zap.Error(err))
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
	if err == nil {
		fields = append(fields, zap.Int("pid", info.PID))
	}
	env.Logger.Info("bootstrap: default script runner activated", fields...)
	return nil
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
