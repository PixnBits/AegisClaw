package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"go.uber.org/zap"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// StoreVM is the boundary for persistent state ownership.
// The Host Daemon must not own or directly create persistent stores.
// All store access goes through StoreVM.Store().
// Concrete implementation (in-process for now, remote Store VM later) lives in this package.
type StoreVM interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Store() Store
}

// storeVM is the unexported concrete implementation.
// Daemon code must not depend on this type directly (only the interface).
type storeVM struct {
	store  Store
	logger *zap.Logger
}

// NewStoreVM creates the StoreVM that owns all persistent stores.
// This encapsulates store creation completely outside of daemon code.
func NewStoreVM(cfg *config.Config, logger *zap.Logger) (StoreVM, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	proposalStore, err := proposal.NewStore(cfg.Proposal.StoreDir, logger)
	if err != nil {
		return nil, fmt.Errorf("create proposal store: %w", err)
	}

	prStorePath := filepath.Join(filepath.Dir(cfg.Audit.Dir), "pullrequests")
	prStore, err := pullrequest.NewStore(prStorePath, logger)
	if err != nil {
		return nil, fmt.Errorf("create PR store: %w", err)
	}

	compositionStore, err := composition.NewStore(cfg.Composition.Dir)
	if err != nil {
		return nil, fmt.Errorf("create composition store: %w", err)
	}

	memIdentity, err := loadOrCreateMemoryIdentity(cfg.Memory.Dir)
	if err != nil {
		return nil, fmt.Errorf("load memory identity: %w", err)
	}

	ttl := memory.TTLTier(cfg.Memory.DefaultTTL)
	if ttl == "" {
		ttl = memory.TTL90d
	}

	memoryStore, err := memory.NewStore(memory.StoreConfig{
		Dir:          cfg.Memory.Dir,
		MaxSizeMB:    cfg.Memory.MaxSizeMB,
		DefaultTTL:   ttl,
		PIIRedaction: cfg.Memory.PIIRedaction,
	}, memIdentity)
	if err != nil {
		return nil, fmt.Errorf("create memory store: %w", err)
	}

	workerStore, err := worker.NewStore(cfg.Worker.Dir)
	if err != nil {
		return nil, fmt.Errorf("create worker store: %w", err)
	}

	unified := NewLocal(
		proposalStore,
		prStore,
		compositionStore,
		memoryStore,
		workerStore,
		nil, // EventStore placeholder
	)

	return &storeVM{
		store:  unified,
		logger: logger,
	}, nil
}

func (vm *storeVM) Start(ctx context.Context) error {
	if vm.logger != nil {
		vm.logger.Info("StoreVM started (in-process transitional)")
	}
	return nil
}

func (vm *storeVM) Stop(ctx context.Context) error {
	if vm.logger != nil {
		vm.logger.Info("StoreVM stopped")
	}
	return nil
}

func (vm *storeVM) Store() Store {
	return vm.store
}

// Compile-time check
var _ StoreVM = (*storeVM)(nil)

// loadOrCreateMemoryIdentity lives here temporarily.
// In a real remote Store VM this will be internal to the VM.
func loadOrCreateMemoryIdentity(dir string) (*age.X25519Identity, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", dir, err)
	}
	identityPath := filepath.Join(dir, ".memory-age-identity")
	data, readErr := os.ReadFile(identityPath)
	if readErr == nil {
		id, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("parse memory identity: %w", err)
		}
		return id, nil
	}
	if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("read memory identity: %w", readErr)
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generate memory identity: %w", err)
	}
	if err := os.WriteFile(identityPath, []byte(id.String()+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("persist memory identity: %w", err)
	}
	return id, nil
}
