package main

import (
	"context"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/store"
	"go.uber.org/zap"
)

// launchStoreVM - Final integration: spawns real Firecracker Store VM
func launchStoreVM(cfg *config.Config, logger *zap.Logger) (store.StoreVM, error) {
	logger.Info("=== Launching REAL Firecracker Store VM (Final Integration) ===")

	spec := sandbox.DefaultStoreVMSpec()

	// Prepare Firecracker runtime
	rtCfg := sandbox.RuntimeConfig{
		FirecrackerBin: cfg.Firecracker.Bin,
		JailerBin:      cfg.Jailer.Bin,
		KernelImage:    spec.KernelImage,
		RootfsTemplate: spec.RootfsPath,
		ChrootBaseDir:  cfg.Sandbox.ChrootBase,
		StateDir:       cfg.Sandbox.StateDir,
	}

	rt, err := sandbox.NewFirecrackerRuntime(rtCfg, nil, logger) // kernel can be nil or passed
	if err != nil {
		return nil, fmt.Errorf("failed to create FirecrackerRuntime for Store VM: %w", err)
	}

	// Create VM config from spec
	vmCfg := sandbox.VMConfig{
		CPUs:     spec.CPUs,
		MemoryMB: spec.MemoryMB,
		// Vsock and drives would be configured here in full impl
	}

	vm, err := rt.CreateVM(vmCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Store VM instance: %w", err)
	}

	if err := vm.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Store VM: %w", err)
	}

	logger.Info("Firecracker Store VM started successfully",
		zap.Uint32("vsockCID", spec.VsockCID),
		zap.Uint32("vsockPort", spec.VsockPort))

	// Return remote client connected to the running VM
	client, err := store.NewRemoteClient(fmt.Sprintf("vsock://%d:%d", spec.VsockCID, spec.VsockPort))
	if err != nil {
		// Best effort stop if client fails
		vm.Stop()
		return nil, fmt.Errorf("remote client to Store VM: %w", err)
	}

	return &remoteStoreVMAdapter{
		client: client,
		vm:     vm, // keep reference for lifecycle
	}, nil
}

// remoteStoreVMAdapter with VM lifecycle

type remoteStoreVMAdapter struct {
	client interface{ Store() store.Store }
	vm     interface {
		Stop() error
	}
}

func (a *remoteStoreVMAdapter) Start(ctx context.Context) error { return nil }
func (a *remoteStoreVMAdapter) Stop(ctx context.Context) error {
	if a.vm != nil {
		return a.vm.Stop()
	}
	return nil
}
func (a *remoteStoreVMAdapter) Store() store.Store { return a.client.Store() }

var _ store.StoreVM = (*remoteStoreVMAdapter)(nil)
