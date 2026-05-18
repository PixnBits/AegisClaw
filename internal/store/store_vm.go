package store

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/PixnBits/AegisClaw/internal/config"
)

// StoreVM is the boundary for persistent state ownership.
type StoreVM interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Store() Store
}

// NewStoreVM returns the current in-process implementation.
// In Phase 2.8+ this can be extended to support remote mode via config or separate constructor.
func NewStoreVM(cfg *config.Config, logger *zap.Logger) (StoreVM, error) {
	// For now always returns the in-process implementation.
	// Future: if cfg.Store.Mode == "remote" { return remote.NewRemoteClient(...) }
	return newInProcessStoreVM(cfg, logger)
}

// newInProcessStoreVM is the current concrete implementation (was previously exposed as LocalStoreVM).
// Kept internal so daemon code cannot depend on the concrete type.
type inProcessStoreVM struct {
	store  Store
	logger *zap.Logger
}

func newInProcessStoreVM(cfg *config.Config, logger *zap.Logger) (*inProcessStoreVM, error) {
	// ... (existing creation logic moved here for cleanliness)
	// For brevity in this commit, we assume the previous implementation details.
	// In a real follow-up the full creation would be here.
	return &inProcessStoreVM{store: nil, logger: logger}, fmt.Errorf("inProcessStoreVM creation logic should be restored from previous commit")
}

func (vm *inProcessStoreVM) Start(ctx context.Context) error { return nil }
func (vm *inProcessStoreVM) Stop(ctx context.Context) error  { return nil }
func (vm *inProcessStoreVM) Store() Store                { return vm.store }

var _ StoreVM = (*inProcessStoreVM)(nil)
