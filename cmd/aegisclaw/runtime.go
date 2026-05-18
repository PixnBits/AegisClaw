package main

// runtimeEnv now holds AegisHubMonitor for lifecycle control.
type runtimeEnv struct {
	AegisHubMonitor *AegisHubMonitor
	AegisHubClient  aegishub.Client
	// ... other fields
}

func initRuntime() (*runtimeEnv, error) {
	logger, _ := zap.NewProduction()

	monitor, err := launchAegisHub(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to launch AegisHub: %w", err)
	}

	env := &runtimeEnv{
		AegisHubMonitor: monitor,
		AegisHubClient:  monitor.client,
	}

	return env, nil
}
