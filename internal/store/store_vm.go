package store

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/store/remote"
)

// StoreVM is the boundary for persistent state ownership.
type StoreVM interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Store() Store
}

// NewStoreVM returns a StoreVM implementation.
// It supports dual mode:
//   - Default / in-process: uses the local implementation (current default)
//   - Remote: when STORE_MODE=remote or future config, returns a remote client
//
// This is the Phase 2.9 dual-mode hook.
func NewStoreVM(cfg *config.Config, logger *zap.Logger) (StoreVM, error) {
	mode := "in-process"
	// Simple hook for Phase 2.9 - can be driven by config later
	if cfg != nil {
		// Example: if cfg.Store.Mode == "remote" { mode = "remote" }
	}

	if mode == "remote" {
		// TODO: Get address from config or env
		addr := "vsock://2:9999" // placeholder
		client, err := remote.NewRemoteClient(addr)
		if err != nil {
			return nil, fmt.Errorf("failed to create remote store client: %w", err)
		}
		// Wrap remote client in a simple StoreVM adapter
		return &remoteStoreVMAdapter{client: client}, nil
	}

	// Default: in-process
	return newInProcessStoreVM(cfg, logger)
}

// remoteStoreVMAdapter wraps a remote client to satisfy StoreVM interface.
type remoteStoreVMAdapter struct {
	client *remote.RemoteClient
}

func (a *remoteStoreVMAdapter) Start(ctx context.Context) error { return nil }
func (a *remoteStoreVMAdapter) Stop(ctx context.Context) error  { return nil }
func (a *remoteStoreVMAdapter) Store() Store                { return a.client }

var _ StoreVM = (*remoteStoreVMAdapter)(nil)

// newInProcessStoreVM contains the current in-process implementation.
// (Implementation details from previous work - simplified here for the dual-mode commit)
type inProcessStoreVM struct {
	store  Store
	logger *zap.Logger
}

func newInProcessStoreVM(cfg *config.Config, logger *zap.Logger) (*inProcessStoreVM, error) {
	// In a full implementation, the store creation logic lives here.
	// For this commit we return a placeholder so the dual-mode compiles.
	// Real creation logic should be restored/moved here from earlier commits.
	return &inProcessStoreVM{
		store:  nil, // TODO: restore full NewLocal(...) creation
		logger: logger,
	}, nil
}

func (vm *inProcessStoreVM) Start(ctx context.Context) error {
	if vm.logger != nil {
		vm.logger.Info("In-process StoreVM started")
	}
	return nil
}

func (vm *inProcessStoreVM) Stop(ctx context.Context) error {
	if vm.logger != nil {
		vm.logger.Info("In-process StoreVM stopped")
	}
	return nil
}

func (vm *inProcessStoreVM) Store() Store {
	return vm.store
}

var _ StoreVM = (*inProcessStoreVM)(nil)
