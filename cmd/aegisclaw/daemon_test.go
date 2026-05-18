package main

// === Contract / Interface Existence Checks (per testing strategy) ===

func TestAegisHubClientInterface(t *testing.T) {
	// Ensures the AegisHubClient seam exists and can be referenced.
	// Full contract testing would use mocks in integration tests.
	var _ AegisHubClient = (*stubAegisHubClient)(nil)
	t.Log("AegisHubClient interface is defined and implemented by stub")
}

func TestToolRegistryClientInterface(t *testing.T) {
	var _ ToolRegistryClient = (*stubToolRegistryClient)(nil)
	t.Log("ToolRegistryClient interface exists")
}

// === Enhanced Lifecycle & Containment Tests ===

func TestAegisHubMonitor_StopDoesNotPanic(t *testing.T) {
	monitor := &AegisHubMonitor{}
	// Should be safe to call Stop even if not fully initialized
	monitor.Stop()
}

func TestCleanupStaleVMsOnStartup_DoesNotPanic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cleanupStaleVMsOnStartup(logger) // best-effort, should not panic
}

// === Hardening State Verification ===

func TestSecureSocket_RemovesStaleSocket(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "stale.sock")

	// Create a stale socket file
	_ = os.WriteFile(sockPath, []byte("old"), 0600)

	logger, _ := zap.NewDevelopment()
	ln, err := createSecureSocket(sockPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// If we reach here without error, the stale socket was handled
	t.Log("createSecureSocket successfully handled stale socket")
}
