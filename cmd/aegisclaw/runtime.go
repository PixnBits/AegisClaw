package main

func initRuntime() (*runtimeEnv, error) {
	logger, _ := zap.NewProduction()

	// Phase 4.2
	_ = dropCapabilities(logger)

	// Phase 4.3: Apply seccomp-bpf filter
	if err := applySeccompFilter(logger); err != nil {
		logger.Warn("Failed to apply seccomp filter (continuing)", zap.Error(err))
	}

	// ... rest of initialization ...
}
