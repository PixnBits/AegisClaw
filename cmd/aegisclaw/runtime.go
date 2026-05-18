package main

func (m *AegisHubMonitor) restart() {
	m.logger.Info("Attempting to restart AegisHub VM")

	// Stop existing VM if present
	if m.vm != nil {
		_ = m.vm.Stop()
	}

	// Re-launch AegisHub
	spec := sandbox.DefaultAegisHubVMSpec()

	rtCfg := sandbox.RuntimeConfig{
		FirecrackerBin: "/usr/local/bin/firecracker",
		JailerBin:      "/usr/local/bin/jailer",
		KernelImage:    spec.KernelImage,
		RootfsTemplate: spec.RootfsPath,
		ChrootBaseDir:  "/var/lib/aegisclaw/jailer",
		StateDir:       "/var/lib/aegisclaw/vm/aegishub",
	}

	rt, err := sandbox.NewFirecrackerRuntime(rtCfg, nil, m.logger)
	if err != nil {
		m.logger.Error("Failed to create runtime for restart", zap.Error(err))
		return
	}

	vmCfg := sandbox.VMConfig{
		CPUs:     spec.CPUs,
		MemoryMB: spec.MemoryMB,
	}

	newVM, err := rt.CreateVM(vmCfg)
	if err != nil {
		m.logger.Error("Failed to create new VM during restart", zap.Error(err))
		return
	}

	if err := newVM.Start(); err != nil {
		m.logger.Error("Failed to start new AegisHub VM", zap.Error(err))
		return
	}

	m.vm = newVM
	m.logger.Info("AegisHub VM successfully restarted")
}
