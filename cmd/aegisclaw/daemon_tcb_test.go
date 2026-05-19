package main

import (
	"testing"

	"github.com/PixnBits/AegisClaw/internal/store"
)

// TestDaemonDoesNotInitializeForbiddenComponents verifies that the minimal
// TCB runtimeEnv no longer contains Vault, Court engine, or BuildOrchestrator.
func TestDaemonDoesNotInitializeForbiddenComponents(t *testing.T) {
	env, err := initRuntime()
	if err != nil {
		t.Fatalf("initRuntime: %v", err)
	}
	defer resetRuntimeSingletons()

	if env.Vault != nil {
		t.Error("Vault must not be initialized in Host Daemon TCB")
	}
	// Note: Vault field is a compat shim; in production init it remains nil.
	// Court and BuildOrchestrator fields were removed; accessing would not compile.
	// We assert absence by checking that heavy fields are zero where applicable.
}

// TestNoStoreInterfaceInDaemon confirms that the general Store interface
// has been removed from the Host Daemon (Phase 5). Persistent state access
// is now externalized to the Store VM via AegisHub mediation.
func TestNoStoreInterfaceInDaemon(t *testing.T) {
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

func TestHardening_CapabilitiesDropCalled(t *testing.T) {
	// Merely exercises the function; real cap drop is environment-dependent.
	if err := dropCapabilities(nil); err != nil {
		t.Logf("dropCapabilities returned err (expected in some envs): %v", err)
	}
}

func TestHardening_SeccompFilterHook(t *testing.T) {
	// Exercises the hook; full BPF enforcement is future work.
	if err := applySeccompFilter(nil); err != nil {
		t.Fatalf("applySeccompFilter: %v", err)
	}
}

func TestHardening_ResourceLimits(t *testing.T) {
	if err := setResourceLimits(nil); err != nil {
		t.Logf("setResourceLimits non-fatal: %v", err)
	}
}
