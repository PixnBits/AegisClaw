package store

import (
	"context"
	"fmt"
	"path/filepath"

	"filippo.io/age"
	"go.uber.org/zap"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// LocalStoreVM is the transitional in-process StoreVM.
// It owns the creation of all persistent stores.
// This makes the Host Daemon no longer responsible for direct store construction.
// In the future this will be replaced by a real Firecracker-based StoreVM
// that exposes stores over vsock.
type LocalStoreVM struct {
	store Store
	logger *zap.Logger
}

func NewLocalStoreVM(cfg *config.Config, logger *zap.Logger) (StoreVM, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	// Create individual stores (this logic was moved from initRuntime)
	proposalStore, err := proposal.NewStore(cfg.Proposal.StoreDir, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create proposal store: %w", err)
	}

	prStorePath := filepath.Join(filepath.Dir(cfg.Audit.Dir), "pullrequests")
	prStore, err := pullrequest.NewStore(prStorePath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR store: %w", err)
	}

	compositionStore, err := composition.NewStore(cfg.Composition.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create composition store: %w", err)
	}

	// Memory identity + config
	memIdentity, err := loadOrCreateMemoryIdentity(cfg.Memory.Dir) // will be moved to store_vm later if needed
	if err != nil {
		return nil, fmt.Errorf("failed to load memory identity: %w", err)
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
		return nil, fmt.Errorf("failed to create memory store: %w", err)
	}

	workerStore, err := worker.NewStore(cfg.Worker.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create worker store: %w", err)
	}

	localStore := NewLocal(
		proposalStore,
		prStore,
		compositionStore,
		memoryStore,
		workerStore,
		nil, // events - to be added later
	)

	return &LocalStoreVM{
		store:  localStore,
		logger: logger,
	}, nil
}

func (vm *LocalStoreVM) Start(ctx context.Context) error {
	vm.logger.Info("LocalStoreVM started (transitional)")
	return nil
}

func (vm *LocalStoreVM) Stop(ctx context.Context) error {
	vm.logger.Info("LocalStoreVM stopped")
	return nil
}

// Store returns the unified store interface.
func (vm *LocalStoreVM) Store() Store {
	return vm.store
}

// Compile-time check
var _ StoreVM = (*LocalStoreVM)(nil)

// loadOrCreateMemoryIdentity is duplicated temporarily for LocalStoreVM.
// In the full Store VM it will live inside the VM.
func loadOrCreateMemoryIdentity(dir string) (*age.X25519Identity, error) {
	// This function will be removed or moved once memory store creation is fully inside StoreVM
	// For now it is kept here to keep LocalStoreVM self-contained.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create memory dir %s: %w", dir, err)
	}
	identityPath := filepath.Join(dir, ".memory-age-identity")
	data, readErr := os.ReadFile(identityPath)
	if readErr == nil {
		id, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("parse memory age identity: %w", err)
		}
		return id, nil
	}
	if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("read memory age identity: %w", readErr)
	}
	// First time
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generate memory age identity: %w", err)
	}
	if err := os.WriteFile(identityPath, []byte(id.String()+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("write memory age identity: %w", err)
	}
	return id, nil
}
