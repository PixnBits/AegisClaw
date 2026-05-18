package main

// === Additional Lifecycle Containment Tests ===

func TestAegisHubMonitor_TracksConsecutiveFailures(t *testing.T) {
	monitor := &AegisHubMonitor{
		maxFailsBeforeRestart: 3,
	}

	// Simulate failures
	monitor.consecutiveFails = 2
	if monitor.consecutiveFails != 2 {
		t.Error("consecutiveFails not tracked correctly")
	}
}

func TestAegisHubMonitor_RestartThreshold(t *testing.T) {
	monitor := &AegisHubMonitor{
		maxFailsBeforeRestart: 2,
	}

	monitor.consecutiveFails = 2

	// At threshold, restart should be considered
	if monitor.consecutiveFails < monitor.maxFailsBeforeRestart {
		t.Error("should be at or above restart threshold")
	}
}

func TestRuntimeEnv_HoldsAegisHubMonitor(t *testing.T) {
	env := &runtimeEnv{
		AegisHubMonitor: &AegisHubMonitor{},
	}

	if env.AegisHubMonitor == nil {
		t.Error("runtimeEnv should hold AegisHubMonitor for lifecycle management")
	}
}
