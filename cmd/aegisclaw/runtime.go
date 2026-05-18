package main

import (
	"context"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/aegishub"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

// launchAegisHub now performs actual Firecracker launch (Phase 3.5 priority).
func launchAegisHub(logger *zap.Logger) (*AegisHubMonitor, error) {
	logger.Info("Phase 3.5: Launching real Firecracker AegisHub VM")

	spec := sandbox.DefaultAegisHubVMSpec()

	rtCfg := sandbox.RuntimeConfig{
		FirecrackerBin: "/usr/local/bin/firecracker",
		JailerBin:      "/usr/local/bin/jailer",
		KernelImage:    spec.KernelImage,
		RootfsTemplate: spec.RootfsPath,
		ChrootBaseDir:  "/var/lib/aegisclaw/jailer",
		StateDir:       "/var/lib/aegisclaw/vm/aegishub",
	}

	rt, err := sandbox.NewFirecrackerRuntime(rtCfg, nil, logger)
	if err != nil {
		return nil, fmt.Errorf("create FirecrackerRuntime for AegisHub: %w", err)
	}

	vmCfg := sandbox.VMConfig{
		CPUs:     spec.CPUs,
		MemoryMB: spec.MemoryMB,
	}

	vm, err := rt.CreateVM(vmCfg)
	if err != nil {
		return nil, fmt.Errorf("create AegisHub VM: %w", err)
	}

	if err := vm.Start(); err != nil {
		return nil, fmt.Errorf("start AegisHub VM: %w", err)
	}

	logger.Info("AegisHub Firecracker VM started", zap.Uint32("cid", spec.VsockCID))

	// Now connect client
	client, err := aegishub.NewClient("")
	if err != nil {
		vm.Stop()
		return nil, fmt.Errorf("connect to AegisHub after launch: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	monitor := &AegisHubMonitor{
		client: client,
		logger: logger,
		cancel: cancel,
		vm:     vm, // store for later shutdown
	}

	monitor.wg.Add(1)
	go monitor.healthLoop(ctx)

	return monitor, nil
}
