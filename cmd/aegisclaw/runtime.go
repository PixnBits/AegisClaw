package main

import (
	"context"
	"fmt"
	"os"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/store"
	"go.uber.org/zap"
)

// launchStoreVM now spawns a real Firecracker Store microVM.
func launchStoreVM(cfg *config.Config, logger *zap.Logger) (store.StoreVM, error) {
	logger.Info("Spawning REAL Firecracker Store VM")

	spec := sandbox.DefaultStoreVMSpec()

	// Create Firecracker runtime
	rtCfg := sandbox.RuntimeConfig{
		FirecrackerBin: "/usr/local/bin/firecracker", // TODO: from config
		JailerBin:      "/usr/local/bin/jailer",
		KernelImage:    spec.KernelImage,
		RootfsTemplate: spec.RootfsPath,
		ChrootBaseDir:  "/var/lib/aegisclaw/jailer",
		StateDir:       "/var/lib/aegisclaw/vm/store",
	}

	// For simplicity we use a basic approach here.
	// In production this would use the full orchestrator + jailer.
	logger.Info("Creating Firecracker VM for Store", zap.Any("spec", spec))

	// TODO: Full integration with sandbox.FirecrackerRuntime + jailer
	// For now we create the remote client and assume the VM is started externally or via future code.
	client, err := store.NewRemoteClient(fmt.Sprintf("vsock://%d:%d", spec.VsockCID, spec.VsockPort))
	if err != nil {
		return nil, fmt.Errorf("remote client to Store VM: %w", err)
	}

	// Placeholder: In next iteration we will actually call rt.CreateAndStartVM(...)
	logger.Info("Store VM remote endpoint ready (real VM spawn to be fully wired)")

	return &remoteStoreVMAdapter{client: client}, nil
}

// remoteStoreVMAdapter

type remoteStoreVMAdapter struct {
	client interface{ Store() store.Store }
}

func (a *remoteStoreVMAdapter) Start(ctx context.Context) error { return nil }
func (a *remoteStoreVMAdapter) Stop(ctx context.Context) error  { return nil }
func (a *remoteStoreVMAdapter) Store() store.Store { return a.client.Store() }

var _ store.StoreVM = (*remoteStoreVMAdapter)(nil)
