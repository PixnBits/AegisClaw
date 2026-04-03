package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"filippo.io/age"
	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	runtimeOnce     sync.Once
	runtimeInst     *sandbox.FirecrackerRuntime
	registryInst    *sandbox.SkillRegistry
	proposalInst    *proposal.Store
	compositionInst *composition.Store
	memoryInst      *memory.Store
	eventBusInst    *eventbus.Bus
	workerStoreInst *worker.Store
	runtimeInitErr  error
)

type runtimeEnv struct {
	Logger           *zap.Logger
	Config           *config.Config
	Kernel           *kernel.Kernel
	Runtime          *sandbox.FirecrackerRuntime
	Registry         *sandbox.SkillRegistry
	ProposalStore    *proposal.Store
	CompositionStore *composition.Store
	MemoryStore      *memory.Store
	EventBus         *eventbus.Bus
	WorkerStore      *worker.Store
	Court            *court.Engine
	LLMProxy         *llm.OllamaProxy
	SafeMode         atomic.Bool

	// AgentVMID is the ID of the main agent microVM. Protected by agentVMMu.
	// Set once by ensureAgentVM on the first chat.message request.
	AgentVMID string
	agentVMMu sync.Mutex

	// AegisHubVMID is the ID of the AegisHub system microVM launched at daemon
	// startup. AegisHub is the sole IPC router for the system; all inter-VM
	// traffic routes through it for ACL enforcement and audit logging.
	// The daemon registers it before starting any other VM.
	AegisHubVMID string
}

func initRuntime() (*runtimeEnv, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	cfg, err := config.Load(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	kern, err := kernel.GetInstance(logger, cfg.Audit.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize kernel: %w", err)
	}

	runtimeOnce.Do(func() {
		rtCfg := sandbox.RuntimeConfig{
			FirecrackerBin: cfg.Firecracker.Bin,
			JailerBin:      cfg.Jailer.Bin,
			KernelImage:    cfg.Sandbox.KernelImage,
			RootfsTemplate: cfg.Rootfs.Template,
			ChrootBaseDir:  cfg.Sandbox.ChrootBase,
			StateDir:       cfg.Sandbox.StateDir,
		}
		runtimeInst, runtimeInitErr = sandbox.NewFirecrackerRuntime(rtCfg, kern, logger)
		if runtimeInitErr != nil {
			return
		}
		registryInst, runtimeInitErr = sandbox.NewSkillRegistry(cfg.Sandbox.RegistryPath)
		if runtimeInitErr != nil {
			return
		}
		proposalInst, runtimeInitErr = proposal.NewStore(cfg.Proposal.StoreDir, logger)
		if runtimeInitErr != nil {
			return
		}
		compositionInst, runtimeInitErr = composition.NewStore(cfg.Composition.Dir)
		if runtimeInitErr != nil {
			return
		}
		// Memory Store: load or create the age identity from the memory directory.
		memIdentity, memIDErr := loadOrCreateMemoryIdentity(cfg.Memory.Dir)
		if memIDErr != nil {
			runtimeInitErr = memIDErr
			return
		}
		ttl := memory.TTLTier(cfg.Memory.DefaultTTL)
		if ttl == "" {
			ttl = memory.TTL90d
		}
		memoryInst, runtimeInitErr = memory.NewStore(memory.StoreConfig{
			Dir:          cfg.Memory.Dir,
			MaxSizeMB:    cfg.Memory.MaxSizeMB,
			DefaultTTL:   ttl,
			PIIRedaction: cfg.Memory.PIIRedaction,
		}, memIdentity)
		if runtimeInitErr != nil {
			return
		}
		// Event Bus: persistent timer/subscription/approval store.
		eventBusInst, runtimeInitErr = eventbus.New(eventbus.Config{
			Dir:              cfg.EventBus.Dir,
			MaxPendingTimers: cfg.EventBus.MaxPendingTimers,
			MaxSubscriptions: cfg.EventBus.MaxSubscriptions,
		})
		if runtimeInitErr != nil {
			return
		}
		// Worker Store: persist worker lifecycle records.
		workerStoreInst, runtimeInitErr = worker.NewStore(cfg.Worker.Dir)
	})
	if runtimeInitErr != nil {
		return nil, fmt.Errorf("failed to initialize runtime: %w", runtimeInitErr)
	}

	return &runtimeEnv{
		Logger:           logger,
		Config:           cfg,
		Kernel:           kern,
		Runtime:          runtimeInst,
		Registry:         registryInst,
		ProposalStore:    proposalInst,
		CompositionStore: compositionInst,
		MemoryStore:      memoryInst,
		EventBus:         eventBusInst,
		WorkerStore:      workerStoreInst,
		LLMProxy:         llm.NewOllamaProxy(llm.AllowedModelsFromRegistry(), "", kern, logger),
	}, nil
}

// loadOrCreateMemoryIdentity loads the age X25519 identity for the memory store
// from <dir>/.memory-age-identity, creating a new one if it doesn't exist.
// This is the same pattern used by the secrets vault.
func loadOrCreateMemoryIdentity(dir string) (*age.X25519Identity, error) {
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
	// First time: generate and persist a new identity.
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generate memory age identity: %w", err)
	}
	if err := os.WriteFile(identityPath, []byte(id.String()+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("write memory age identity: %w", err)
	}
	return id, nil
}

// generateVMID produces a short, human-readable VM identifier with the given
// prefix (e.g. "aegishub", "agent", "court") and a random 8-character suffix.
// The format is: "<prefix>-<8-hex-chars>". All VM IDs in the daemon use this
// helper so the format stays consistent and is easy to change in one place.
func generateVMID(prefix string) string {
	return prefix + "-" + uuid.New().String()[:8]
}
