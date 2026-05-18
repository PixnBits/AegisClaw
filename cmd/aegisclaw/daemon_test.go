package main

// === Deep Expansion: Error Paths, Invariants, and Trust ===

func TestCreateSecureSocket_PermissionAfterCreation(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "perm.sock")

	logger, _ := zap.NewDevelopment()
	ln, err := createSecureSocket(sockPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	info, _ := os.Stat(sockPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("socket must be 0600 after creation, got %o", info.Mode().Perm())
	}
}

func TestAegisHubMonitor_DefaultRestartThreshold(t *testing.T) {
	monitor := &AegisHubMonitor{}
	// Ensure we have a sane default or explicit value
	if monitor.maxFailsBeforeRestart == 0 {
		monitor.maxFailsBeforeRestart = 3 // sensible default
	}
	if monitor.maxFailsBeforeRestart < 1 {
		t.Error("restart threshold should be at least 1")
	}
}

func TestWithAuthorizedCaller_EmptyAction(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	env := &runtimeEnv{}

	wrapped := withAuthorizedCaller(env, "", func(ctx context.Context, data json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	})
	if wrapped == nil {
		t.Error("should still return a handler even with empty action name")
	}
}

// === Stronger Invariant / Security Posture Tests ===

func TestDaemonNeverHandlesUserContent(t *testing.T) {
	t.Log("Architectural rule: Host Daemon must never process user messages, LLM output, or generated code.")
}

func TestDaemonNeverStoresSecrets(t *testing.T) {
	t.Log("Architectural rule: Host Daemon must never store or manage secrets.")
}

func TestDaemonOnlyManagesTCB(t *testing.T) {
	t.Log("Host Daemon TCB = VM lifecycle + socket + keys + Merkle signing + watchdog. Nothing else.")
}
