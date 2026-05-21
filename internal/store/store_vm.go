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
type StoreVM interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Store() Store
}

// NewStoreVM returns a StoreVM implementation (dual-mode ready).
// Default: in-process. Remote mode can be enabled via future config.
func NewStoreVM(cfg *config.Config, logger *zap.Logger) (StoreVM, error) {
	// Phase 2.9+ hook for remote mode
	if os.Getenv("STORE_MODE") == "remote" {
		addr := "vsock://2:9999"                  // placeholder
		client, err := remoteClientFromAddr(addr) // helper below
		if err != nil {
			return nil, err
		}
		return &remoteStoreVMAdapter{client: client}, nil
	}

	return newInProcessStoreVM(cfg, logger)
}

// newInProcessStoreVM creates the full in-process implementation.
func newInProcessStoreVM(cfg *config.Config, logger *zap.Logger) (*inProcessStoreVM, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}

	// Recreate the stores (logic restored during cleanup pass)
	proposalStore, err := proposal.NewStore(cfg.Proposal.StoreDir, logger)
	if err != nil {
		return nil, fmt.Errorf("proposal store: %w", err)
	}

	auditDir := cfg.Audit.Dir
	if auditDir == "" {
		return nil, fmt.Errorf("cfg.Audit.Dir is required and must not be empty")
	}
	if !filepath.IsAbs(auditDir) {
		return nil, fmt.Errorf("cfg.Audit.Dir must be an absolute path, got: %q", auditDir)
	}
	// The PR store lives as a sibling of the audit directory under the shared
	// data root (e.g. /data/pullrequests alongside /data/audit).
	prStorePath := filepath.Join(filepath.Dir(auditDir), "pullrequests")
	prStore, err := pullrequest.NewStore(prStorePath, logger)
	if err != nil {
		return nil, fmt.Errorf("pr store: %w", err)
	}

	compositionStore, err := composition.NewStore(cfg.Composition.Dir)
	if err != nil {
		return nil, fmt.Errorf("composition store: %w", err)
	}

	memIdentity, err := loadOrCreateMemoryIdentity(cfg.Memory.Dir)
	if err != nil {
		return nil, fmt.Errorf("memory identity: %w", err)
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
		return nil, fmt.Errorf("memory store: %w", err)
	}

	workerStore, err := worker.NewStore(cfg.Worker.Dir)
	if err != nil {
		return nil, fmt.Errorf("worker store: %w", err)
	}

	unified := NewLocal(
		proposalStore,
		prStore,
		compositionStore,
		memoryStore,
		workerStore,
		nil,
	)

	return &inProcessStoreVM{
		store:  unified,
		logger: logger,
	}, nil
}

type inProcessStoreVM struct {
	store  Store
	logger *zap.Logger
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

// remoteStoreVMAdapter (from Phase 2.9)
type remoteStoreVMAdapter struct {
	client interface{ Store() Store }
}

func (a *remoteStoreVMAdapter) Start(ctx context.Context) error { return nil }
func (a *remoteStoreVMAdapter) Stop(ctx context.Context) error  { return nil }
func (a *remoteStoreVMAdapter) Store() Store                    { return a.client.Store() }

var _ StoreVM = (*remoteStoreVMAdapter)(nil)

// Helper for remote (Phase 2.8/2.9)
func remoteClientFromAddr(addr string) (interface{ Store() Store }, error) {
	// Placeholder - real implementation uses remote.NewRemoteClient
	return nil, fmt.Errorf("remote mode not fully implemented yet (use STORE_MODE=in-process)")
}

// loadOrCreateMemoryIdentity (kept here for self-contained in-process creation)
func loadOrCreateMemoryIdentity(dir string) (*age.X25519Identity, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create memory dir %s: %w", dir, err)
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
