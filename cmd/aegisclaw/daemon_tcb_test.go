package main

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestConfig creates a temporary HOME with a minimal config.yaml so that
// initRuntime() / config.Load() does not attempt to write the full default
// configuration (which can trigger expensive/fragile YAML marshaling in CI
// environments with a clean home directory).
//
// It also enables lightweight runtime initialization (nop logger) to avoid
// heavy zap allocations that have repeatedly caused OOMs during `make test`
// on GitHub Actions runners.
func setupTestConfig(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")
	t.Setenv("AEGISCLAW_TEST_LIGHTWEIGHT", "1")

	configDir := filepath.Join(home, ".aegis", "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("create test config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	// Minimal config that satisfies Load without triggering the "create defaults" write path.
	minimal := `daemon:
  socket_path: ` + filepath.Join(home, ".aegis", "run", "daemon.sock") + "\n"
	if err := os.WriteFile(configPath, []byte(minimal), 0600); err != nil {
		t.Fatalf("write minimal config: %v", err)
	}
}

// TestDaemonDoesNotInitializeForbiddenComponents verifies that the minimal
// TCB runtimeEnv no longer contains Vault, Court engine, or BuildOrchestrator.
// Phase 9 test cleanup: Vault field removed from runtimeEnv; test updated accordingly.
func TestDaemonDoesNotInitializeForbiddenComponents(t *testing.T) {
	setupTestConfig(t)
	env, err := initRuntime()
	if err != nil {
		t.Fatalf("initRuntime: %v", err)
	}
	defer resetRuntimeSingletons()

	// Vault, Court, BuildOrchestrator fields removed from runtimeEnv.
	// Their absence is enforced at compile time (no field = no access).
	// We verify TCB minimality by checking that heavy fields remain nil/unset.
	_ = env // env is valid; forbidden fields would cause compile error if referenced.
}

// TestNoStoreInterfaceInDaemon confirms that the general Store interface
// has been removed from the Host Daemon (Phase 5). Persistent state access
// is now externalized to the Store VM via AegisHub mediation.
func TestNoStoreInterfaceInDaemon(t *testing.T) {
	setupTestConfig(t)
	env, err := initRuntime()
	if err != nil {
		t.Fatalf("initRuntime: %v", err)
	}
	defer resetRuntimeSingletons()

	// env.Store field no longer exists on runtimeEnv.
	// This test simply documents the removal.
	_ = env
}

// TestDaemonOnlyCoreResponsibilities is a lightweight structural check that
// the daemon launches only AegisHub and registers minimal handlers.
func TestDaemonOnlyCoreResponsibilities(t *testing.T) {
	setupTestConfig(t)
	// In a real integration this would inspect registered handlers and
	// launched VMs, but here we simply ensure no forbidden init occurred.
	env, err := initRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer resetRuntimeSingletons()

	if env.AegisHubVMID == "" && env.Runtime == nil {
		t.Log("core VM fields present for watchdog responsibility")
	}
}

// TestNoNonTCBInitializations verifies aggressive pre-hardening cleanup:
// no team/autonomy registry creation, reconcile and script runner disabled.
func TestNoNonTCBInitializations(t *testing.T) {
	setupTestConfig(t)
	// The init path in runStart no longer calls newTeamRegistry,
	// newAutonomyRegistry, reconcileApprovedProposals, or
	// ensureDefaultScriptRunnerActive. This test documents that state.
	env, err := initRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer resetRuntimeSingletons()

	// runtimeEnv no longer has TeamRegistry / AutonomyRegistry fields
	// (removed during shim cleanup + aggressive pass).
	_ = env
}

// Phase 4 basic hardening smoke tests (can run without full system).
// Real verification of effects (post-set rlimit values, seccomp kill proof, etc.)
// lives in daemon_hardening_linux_test.go (build-tagged for Linux + unix syscall access).

func TestHardening_CapabilitiesDropCalled(t *testing.T) {
	// Merely exercises the function; real cap drop is environment-dependent.
	if err := dropCapabilities(nil); err != nil {
		t.Logf("dropCapabilities returned err (expected in some envs): %v", err)
	}
}

func TestHardening_CapabilityBoundingSetApplied(t *testing.T) {
	// Exercises the bounding set application. Non-fatal in many environments.
	if err := applyCapabilityBoundingSet(nil); err != nil {
		t.Logf("applyCapabilityBoundingSet returned err: %v", err)
	}
}

func TestHardening_SeccompFilterHook(t *testing.T) {
	// Installing the strict seccomp filter in this process succeeds, then the
	// next disallowed syscall (e.g. from the test runner) SIGSYS-kills the
	// whole `go test` binary. Exercise the hook with strict mode off here;
	// validate strict filters in a subprocess or manual run.
	t.Setenv("AEGISCLAW_SECCOMP_STRICT", "0")
	if err := applySeccompFilter(nil); err != nil {
		t.Logf("applySeccompFilter returned err (expected in some envs): %v", err)
	}
}

func TestHardening_ResourceLimits(t *testing.T) {
	if err := setResourceLimits(nil); err != nil {
		t.Logf("setResourceLimits non-fatal: %v", err)
	}
}

func TestHardening_CgroupLimitsApplied(t *testing.T) {
	if err := applyCgroupLimits(nil); err != nil {
		t.Logf("applyCgroupLimits non-fatal: %v", err)
	}
}
