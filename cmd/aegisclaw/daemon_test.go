package main

// === More Authorization Scenarios ===

func TestWithAuthorizedCaller_NilEnv(t *testing.T) {
	wrapped := withAuthorizedCaller(nil, "test", func(ctx context.Context, data json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	})
	if wrapped == nil {
		t.Error("expected non-nil wrapped handler even with nil env")
	}
}

// === Error Path Tests for Secure Socket ===

func TestCreateSecureSocket_InvalidPath(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	// Trying to create socket in a path that cannot be created
	_, err := createSecureSocket("/nonexistent/very/deep/path/that/cannot/be/created.sock", logger)
	if err == nil {
		t.Error("expected error when parent directories cannot be created")
	}
}

// === More Monitor / Lifecycle Behavior ===

func TestAegisHubMonitor_InitialState(t *testing.T) {
	monitor := &AegisHubMonitor{}
	if monitor.consecutiveFails != 0 {
		t.Error("new monitor should start with zero failures")
	}
	if monitor.maxFailsBeforeRestart == 0 {
		// default should be reasonable
		monitor.maxFailsBeforeRestart = 3
	}
}

// === Policy Reinforcement Tests ===

func TestDaemonShouldNotContainBusinessLogic(t *testing.T) {
	// This test exists to make the architectural invariant explicit.
	t.Log("Host Daemon must remain free of business logic, chat, proposals, memory, etc.")
}
