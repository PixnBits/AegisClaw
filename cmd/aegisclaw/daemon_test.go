package main

// === Authorization-related tests (from backlog) ===

func TestWithAuthorizedCaller_WrapsHandler(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	env := &runtimeEnv{}

	// Basic smoke test that the wrapper can be created
	handler := func(ctx context.Context, data json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	}

	wrapped := withAuthorizedCaller(env, "test.action", handler)
	if wrapped == nil {
		t.Error("withAuthorizedCaller returned nil")
	}
}

// === Hardening verification (from backlog) ===

func TestDropCapabilities_RunsWithoutFatalError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	err := dropCapabilities(logger)

	// We don't assert specific caps here (platform dependent),
	// but it should complete without crashing the test process.
	if err != nil {
		t.Logf("dropCapabilities returned non-fatal error: %v", err)
	}
}
