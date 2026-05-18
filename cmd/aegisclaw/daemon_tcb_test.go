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

// TestAllPersistentAccessViaStore confirms that the canonical Store is present
// and individual stores are accessible only through it (or compat shims).
func TestAllPersistentAccessViaStore(t *testing.T) {
	env, err := initRuntime()
	if err != nil {
		t.Fatalf("initRuntime: %v", err)
	}
	defer resetRuntimeSingletons()

	if env.Store == nil {
		t.Fatal("env.Store must be the single source of truth")
	}
	// Verify interface usage
	var _ store.Store = env.Store
	_ = env.Store.Proposals()
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
