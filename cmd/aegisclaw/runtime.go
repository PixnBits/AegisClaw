package main

func initRuntime() (*runtimeEnv, error) {
	logger, _ := zap.NewProduction()

	// Phase 4.1: Cleanup any VMs left from previous crashes
	cleanupStaleVMsOnStartup(logger)

	// ... existing AegisHub launch logic ...

	env := &runtimeEnv{
		// ...
	}

	// Phase 4.1: Setup aggressive lifecycle containment
	setupLifecycleContainment(env, logger)

	return env, nil
}
