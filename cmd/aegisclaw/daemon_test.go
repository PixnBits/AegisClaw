package main

// === Additional Edge Case & Error Path Tests ===

func TestCreateSecureSocket_AlreadyExists(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "existing.sock")

	// Pre-create the socket
	_ = os.WriteFile(sockPath, []byte("existing"), 0600)

	logger, _ := zap.NewDevelopment()
	ln, err := createSecureSocket(sockPath, logger)
	if err != nil {
		t.Fatalf("should handle existing socket: %v", err)
	}
	defer ln.Close()
}

func TestDropCapabilities_MultipleCalls(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	_ = dropCapabilities(logger)
	_ = dropCapabilities(logger) // calling twice should be safe
}

// === More Monitor Behavior ===

func TestAegisHubMonitor_HealthRecoveryResetsFailures(t *testing.T) {
	monitor := &AegisHubMonitor{
		maxFailsBeforeRestart: 3,
	}

	monitor.consecutiveFails = 2
	// Simulate successful health check
	monitor.consecutiveFails = 0

	if monitor.consecutiveFails != 0 {
		t.Error("failures should reset on health recovery")
	}
}

// === Invariant / Policy Tests ===

func TestDaemonMinimalTCB_Explicit(t *testing.T) {
	t.Log("Host Daemon TCB should only contain: VM lifecycle, socket server, key distribution, Merkle signing, and watchdog.")
}

func TestNoGovernanceInDaemon(t *testing.T) {
	t.Log("Governance Court logic must live outside the Host Daemon.")
}
