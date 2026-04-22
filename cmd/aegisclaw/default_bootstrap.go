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

// kbBuiltInSkills defines the names and metadata for the two built-in
// Knowledge Base skills that are registered at daemon startup.
// They appear in `aegisclaw skill list` and the dashboard Skills page.
var kbBuiltInSkills = []struct {
	name        string
	description string
	schedule    string
}{
	{
		name:        "kb-compiler",
		description: "Compiles raw KB documents into structured Markdown wiki pages on a configurable timer (default: every 6 hours).",
		schedule:    "6h",
	},
	{
		name:        "kb-linter",
		description: "Scans the KB wiki for contradictions, stale info, orphaned pages, and gaps on a daily schedule.",
		schedule:    "24h",
	},
}

// ensureKBSkillsRegistered registers the built-in KB Compiler and Linter
// skills in the skill registry so they appear in 'aegisclaw skill list' and
// on the dashboard Skills page.  Unlike VM-backed skills, the KB skills are
// managed entirely within the daemon process; their sandbox IDs are the
// canonical built-in identifiers below.
func ensureKBSkillsRegistered(env *runtimeEnv) {
	if env == nil || env.Registry == nil {
		return
	}

	for _, s := range kbBuiltInSkills {
		if existing, ok := env.Registry.Get(s.name); ok && existing != nil &&
			existing.State == sandbox.SkillStateActive {
			continue
		}

		// Use a deterministic, human-readable sandbox ID for built-in skills.
		sandboxID := "builtin-" + s.name
		entry, err := env.Registry.Register(s.name, sandboxID, map[string]string{
			"description": s.description,
			"schedule":    s.schedule,
			"type":        "builtin-kb",
			"model":       env.Config.KnowledgeBase.CompilerModel,
		})
		if err != nil {
			env.Logger.Warn("bootstrap: failed to register KB skill",
				zap.String("skill", s.name), zap.Error(err))
			continue
		}

		payload, _ := json.Marshal(map[string]interface{}{
			"skill_name": s.name,
			"sandbox_id": sandboxID,
			"version":    entry.Version,
			"hash":       entry.MerkleHash,
			"type":       "builtin-kb",
		})
		action := kernel.NewAction(kernel.ActionSkillActivate, "system", payload)
		_, _ = env.Kernel.SignAndLog(action)

		env.Logger.Info("bootstrap: KB skill registered",
			zap.String("skill", s.name),
			zap.String("sandbox_id", sandboxID),
		)
	}
}

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
