package main

func initRuntime() (*runtimeEnv, error) {
	logger, _ := zap.NewProduction()

	logger.Info("Starting Host Daemon hardening (Phase 4)")

	if err := dropCapabilities(logger); err != nil {
		logger.Warn("Capability dropping completed with warnings", zap.Error(err))
	} else {
		logger.Info("Capabilities successfully dropped to minimal set")
	}

	if err := applySeccompFilter(logger); err != nil {
		logger.Warn("seccomp filter applied with warnings", zap.Error(err))
	} else {
		logger.Info("seccomp-bpf filter successfully applied")
	}

	// ... rest of init ...
}
