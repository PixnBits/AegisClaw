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
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/lookup"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"github.com/PixnBits/AegisClaw/internal/store"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	runtimeOnce    sync.Once
	runtimeInst    *sandbox.FirecrackerRuntime
	registryInst   *sandbox.SkillRegistry
	storeInst      store.Store
	runtimeInitErr error
)

type runtimeEnv struct {
	Logger  *zap.Logger
	Config  *config.Config
	Kernel  *kernel.Kernel
	Runtime *sandbox.FirecrackerRuntime
	// Registry manages skill sandboxes for VM lifecycle (core TCB responsibility).
	Registry *sandbox.SkillRegistry

	// Store is the single source of truth for all persistent state access.
	// All proposals, PRs, composition, memory, workers, and events must be
	// accessed exclusively via env.Store.* methods. Direct store fields have
	// been removed to enforce the abstraction and prepare for Store VM ownership.
	Store store.Store

	// CourtClient and BuilderClient are thin seams for work that lives outside
	// the Host Daemon TCB (Court VMs + Scribe, Builder VMs). The daemon never
	// runs governance or build orchestration logic itself.
	CourtClient   CourtClient
	BuilderClient BuilderClient

	// AgentVMID / AegisHubVMID track the core microVMs the daemon launches and
	// watches. This is the extent of the daemon's VM lifecycle responsibility.
	AegisHubVMID string
	AgentVMID    string
	agentVMMu    sync.Mutex

	// SafeMode is retained for minimal operational control during startup.
	SafeMode atomic.Bool

	// team/autonomy shims removed in final cleanup; referenced by extended
	// handlers are now nil to keep build green while surface is reduced.
	TeamRegistry     *teamRegistry
	AutonomyRegistry *autonomyRegistry

	// Additional legacy shims retained only to keep the tree buildable
	// during aggressive surface reduction. These will be removed in Phase 4.
	Sessions   *sessions.Store
	portalVMMu sync.Mutex
	PortalVMID string

	// ProposalStore shim retained for remaining dashboard handler references
	// during surface reduction.
	ProposalStore *proposal.Store
	PRStore       *pullrequest.Store
	MemoryStore   *memory.Store
	EventBus      *eventbus.Bus
	GitManager    *gitmanager.Manager
	LookupStore   *lookup.Store
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

		// Create all persistent stores and aggregate them behind the single
		// store.Store interface. This is the ONLY path for persistent state
		// access from the Host Daemon. Direct store fields have been removed.
		propStore, err := proposal.NewStore(cfg.Proposal.StoreDir, logger)
		if err != nil {
			runtimeInitErr = err
			return
		}
		prStorePath := filepath.Join(filepath.Dir(cfg.Audit.Dir), "pullrequests")
		prStore, err := pullrequest.NewStore(prStorePath, logger)
		if err != nil {
			runtimeInitErr = err
			return
		}
		compStore, err := composition.NewStore(cfg.Composition.Dir)
		if err != nil {
			runtimeInitErr = err
			return
		}
		memIdentity, memIDErr := loadOrCreateMemoryIdentity(cfg.Memory.Dir)
		if memIDErr != nil {
			runtimeInitErr = memIDErr
			return
		}
		ttl := memory.TTLTier(cfg.Memory.DefaultTTL)
		if ttl == "" {
			ttl = memory.TTL90d
		}
		memStore, err := memory.NewStore(memory.StoreConfig{
			Dir:          cfg.Memory.Dir,
			MaxSizeMB:    cfg.Memory.MaxSizeMB,
			DefaultTTL:   ttl,
			PIIRedaction: cfg.Memory.PIIRedaction,
		}, memIdentity)
		if err != nil {
			runtimeInitErr = err
			return
		}
		evtBus, err := eventbus.New(eventbus.Config{
			Dir:              cfg.EventBus.Dir,
			MaxPendingTimers: cfg.EventBus.MaxPendingTimers,
			MaxSubscriptions: cfg.EventBus.MaxSubscriptions,
		})
		if err != nil {
			runtimeInitErr = err
			return
		}
		wrkStore, err := worker.NewStore(cfg.Worker.Dir)
		if err != nil {
			runtimeInitErr = err
			return
		}

		// Vault is intentionally NOT created here. Per host-daemon.md the
		// daemon must never handle secrets; any secret paths are stubbed.

		// Aggregate behind the single Store interface.
		storeInst = store.NewLocal(propStore, prStore, compStore, memStore, wrkStore, evtBus)
	})
	if runtimeInitErr != nil {
		return nil, fmt.Errorf("failed to initialize runtime: %w", runtimeInitErr)
	}

	return &runtimeEnv{
		Logger:        logger,
		Config:        cfg,
		Kernel:        kern,
		Runtime:       runtimeInst,
		Registry:      registryInst,
		Store:         storeInst,
		CourtClient:   noopCourtClient{},
		BuilderClient: noopBuilderClient{},
	}, nil
}

// resetRuntimeSingletons zeros all package-level singleton state so that a
// subsequent initRuntime call starts fresh.  This is used by live integration
// tests that must run multiple scenarios in the same process without sharing
// state from a prior initRuntime invocation.
//
// Must be called before kernel.ResetInstance() because the kernel itself is
// tracked outside this package.
func resetRuntimeSingletons() {
	runtimeOnce = sync.Once{}
	runtimeInst = nil
	registryInst = nil
	storeInst = nil
	runtimeInitErr = nil
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

// BuilderClient is the thin seam for build requests. The real Builder
// orchestrator and pipeline execution now live outside the Host Daemon TCB
// (in AegisHub + Builder VMs). The daemon only holds the client interface.
type BuilderClient interface {
	// RequestBuild is a no-op in the minimal TCB; actual builds are
	// dispatched by AegisHub after proposal approval.
	RequestBuild(proposalID string) error
}

type noopBuilderClient struct{}

func (noopBuilderClient) RequestBuild(string) error { return nil }
