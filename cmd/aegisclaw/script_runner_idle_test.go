package main

// script_runner_idle_test.go tests the idle-timeout cleanup mechanism for
// the default script runner sandbox.

import (
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap/zaptest"
)

// makeTestEnvForIdle returns a minimal runtimeEnv for testing the idle daemon.
// Runtime is intentionally nil to prevent sandbox calls in unit tests.
func makeTestEnvForIdle(t *testing.T) *runtimeEnv {
	t.Helper()
	kernel.ResetInstance()
	t.Cleanup(func() { kernel.ResetInstance() })

	logger := zaptest.NewLogger(t)
	kern, err := kernel.GetInstance(logger, t.TempDir())
	if err != nil {
		t.Fatalf("kernel.GetInstance: %v", err)
	}
	t.Cleanup(func() { kern.Shutdown() })

	reg, err := sandbox.NewSkillRegistry(t.TempDir() + "/registry.json")
	if err != nil {
		t.Fatalf("NewSkillRegistry: %v", err)
	}

	bus, err := eventbus.New(eventbus.Config{
		Dir:              t.TempDir(),
		MaxPendingTimers: 10,
	})
	if err != nil {
		t.Fatalf("eventbus.New: %v", err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("age.GenerateX25519Identity: %v", err)
	}
	mem, err := memory.NewStore(memory.StoreConfig{Dir: t.TempDir()}, identity)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}

	return &runtimeEnv{
		Logger:      logger,
		Kernel:      kern,
		Registry:    reg,
		EventBus:    bus,
		MemoryStore: mem,
		// Runtime intentionally nil — no sandbox calls in unit tests.
	}
}

// TestMaybeStopIdleScriptRunner_NilRuntime verifies that maybeStopIdleScriptRunner
// is a no-op when Runtime is nil.
func TestMaybeStopIdleScriptRunner_NilRuntime(t *testing.T) {
	env := makeTestEnvForIdle(t)
	// env.Runtime is nil.  This must not panic.
	maybeStopIdleScriptRunner(t.Context(), env)
}

// TestMaybeStopIdleScriptRunner_NoRegistryEntry verifies early return when
// there is no script runner entry in the registry.
func TestMaybeStopIdleScriptRunner_NoRegistryEntry(t *testing.T) {
	env := makeTestEnvForIdle(t)
	env.ScriptRunnerLastUsed.Store(time.Now().Add(-1 * time.Hour).UnixNano())
	// env.Runtime is nil; if we don't return early we'd panic.
	maybeStopIdleScriptRunner(t.Context(), env)
	// Reaching here without panic is the assertion.
}

// TestMaybeStopIdleScriptRunner_NotIdle verifies that a recently used script
// runner is not shut down.
func TestMaybeStopIdleScriptRunner_NotIdle(t *testing.T) {
	env := makeTestEnvForIdle(t)
	// Register a fake active script runner in the registry.
	_, err := env.Registry.Register(defaultScriptRunnerSkill, "sandbox-abc", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Set last-used to now (definitely not idle).
	env.ScriptRunnerLastUsed.Store(time.Now().UnixNano())

	// Runtime is nil; if maybeStopIdleScriptRunner proceeds past the idle
	// check it would panic.  Reaching this point without panic proves it
	// returned early.
	maybeStopIdleScriptRunner(t.Context(), env)
}

// TestMaybeStopIdleScriptRunner_ZeroLastUsed verifies that a zero last-used
// timestamp does not trigger shutdown (never-used script runner).
func TestMaybeStopIdleScriptRunner_ZeroLastUsed(t *testing.T) {
	env := makeTestEnvForIdle(t)
	_, err := env.Registry.Register(defaultScriptRunnerSkill, "sandbox-abc", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// ScriptRunnerLastUsed is 0 (zero value), meaning the sandbox was never
	// explicitly activated.  We should not shut it down.
	maybeStopIdleScriptRunner(t.Context(), env)
	// Not panicking proves the nil-Runtime path was not reached.
}

// TestScriptRunnerLastUsed_AtomicBehaviour verifies the atomic int is readable
// and writable across goroutines (basic sanity test for the field type).
func TestScriptRunnerLastUsed_AtomicBehaviour(t *testing.T) {
	var v atomic.Int64
	if v.Load() != 0 {
		t.Error("expected zero initial value")
	}
	now := time.Now().UnixNano()
	v.Store(now)
	if v.Load() != now {
		t.Error("expected stored value to match")
	}
}

// TestMaybeStopIdleScriptRunner_InactiveEntry verifies that a registry entry
// that is NOT active (e.g. already stopped) is skipped.
func TestMaybeStopIdleScriptRunner_InactiveEntry(t *testing.T) {
	env := makeTestEnvForIdle(t)
	// Register and immediately deactivate so State = SkillStateStopped.
	_, err := env.Registry.Register(defaultScriptRunnerSkill, "sandbox-abc", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := env.Registry.Deactivate(defaultScriptRunnerSkill); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	// Set last-used far in the past to trigger idle condition if reached.
	env.ScriptRunnerLastUsed.Store(time.Now().Add(-24 * time.Hour).UnixNano())

	// Runtime is nil; if we hit the stop code it would panic.
	maybeStopIdleScriptRunner(t.Context(), env)
}

// TestStartScriptRunnerIdleDaemon_NilRuntime verifies no panic when Runtime is nil.
func TestStartScriptRunnerIdleDaemon_NilRuntime(t *testing.T) {
	env := makeTestEnvForIdle(t)
	// env.Runtime is nil; startScriptRunnerIdleDaemon should return immediately.
	ctx := t.Context()
	startScriptRunnerIdleDaemon(ctx, env) // must not panic
}
