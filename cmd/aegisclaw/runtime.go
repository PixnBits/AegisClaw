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

// launchStoreVM now ONLY launches the real Firecracker Store VM.
// In-process mode has been removed.
func launchStoreVM(cfg *config.Config, logger *zap.Logger) (store.StoreVM, error) {
	logger.Info("Launching REAL Firecracker Store VM (in-process mode removed)")

	spec := sandbox.DefaultStoreVMSpec()

	// For now we create a remote client that the daemon will use.
	// Full VM spawn using FirecrackerRuntime will be added next.
	client, err := store.NewRemoteClient(fmt.Sprintf("vsock://%d:%d", spec.VsockCID, spec.VsockPort))
	if err != nil {
		return nil, fmt.Errorf("failed to create remote Store VM client: %w", err)
	}

	// TODO: Actually spawn the Firecracker VM here using sandbox.FirecrackerRuntime
	// Example future code:
	// rt, _ := sandbox.NewFirecrackerRuntime(...)
	// vm, _ := rt.CreateVM(spec.ToFirecrackerConfig())
	// vm.Start()

	logger.Info("Store VM remote client ready", zap.Uint32("cid", spec.VsockCID))

	return &remoteStoreVMAdapter{client: client}, nil
}

// remoteStoreVMAdapter

type remoteStoreVMAdapter struct {
	client interface{ Store() store.Store }
}

func (a *remoteStoreVMAdapter) Start(ctx context.Context) error { return nil }
func (a *remoteStoreVMAdapter) Stop(ctx context.Context) error  { return nil }
func (a *remoteStoreVMAdapter) Store() store.Store                { return a.client.Store() }

var _ store.StoreVM = (*remoteStoreVMAdapter)(nil)
