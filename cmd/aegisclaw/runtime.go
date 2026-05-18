package main

func initRuntime() (*runtimeEnv, error) {
	logger, _ := zap.NewProduction()

	// Phase 4.2: Drop unnecessary capabilities as early as possible
	if err := dropCapabilities(logger); err != nil {
		logger.Warn("Capability dropping failed (continuing)", zap.Error(err))
	}

	// ... rest of init ...
}
