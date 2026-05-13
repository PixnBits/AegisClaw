// initBuildOrchestrator is a stub that allows the daemon to start while the full
// Pipeline + BuilderRuntime wiring is completed in follow-up commits.
// It returns nil for now so CI builds pass. The orchestrator is started only
// if successfully initialized.
func initBuildOrchestrator(env *runtimeEnv) (*builder.BuildOrchestrator, error) {
	// TODO(full wiring): Construct real Pipeline with BuilderRuntime, CodeGenerator, etc.
	// and return NewBuildOrchestrator(...)
	env.Logger.Info("BuildOrchestrator initialization skipped (stub) - full wiring in follow-up")
	return nil, nil
}