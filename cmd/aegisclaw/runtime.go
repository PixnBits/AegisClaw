package main

import (
	"fmt"
	"net/http"
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
	"github.com/PixnBits/AegisClaw/internal/vault"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"github.com/PixnBits/AegisClaw/internal/store"
	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/PixnBits/AegisClaw/internal/workspace"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/lookup"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	runtimeOnce       sync.Once
	runtimeInst       *sandbox.FirecrackerRuntime
	registryInst      *sandbox.SkillRegistry
	storeInst         store.Store
	proposalStoreShim *proposal.Store
	prStoreShim       *pullrequest.Store
	compStoreShim     *composition.Store
	memStoreShim      *memory.Store
	evtBusShim        *eventbus.Bus
	wrkStoreShim      *worker.Store
	runtimeInitErr    error
)

type runtimeEnv struct {
	Logger  *zap.Logger
	Config  *config.Config
	Kernel  *kernel.Kernel
	Runtime *sandbox.FirecrackerRuntime
	// Registry manages skill sandboxes for VM lifecycle (core TCB responsibility).
	Registry *sandbox.SkillRegistry

	// Store is the single source of truth for all persistent state access.
	// Code should prefer env.Store.Proposals() etc. Direct fields below are
	// deprecated shims retained only during the Phase 1 migration so that
	// the large number of existing call sites (chat, skill_cmd, handlers_*.go)
	// continue to compile while we migrate them. They will be removed in Phase 2.
	Store store.Store

	// Deprecated direct stores - shim to Store internals for compat.
	ProposalStore    *proposal.Store
	PRStore          *pullrequest.Store
	CompositionStore *composition.Store
	MemoryStore      *memory.Store
	EventBus         *eventbus.Bus
	WorkerStore      *worker.Store

	// Deprecated chat/session/event buffers - non-TCB but retained for compat during refactor.
	Sessions      *sessions.Store
	ToolEvents    *ToolEventBuffer
	ThoughtEvents *ThoughtEventBuffer
	LLMProxy      *llm.OllamaProxy

	// Test / workspace shims retained for test and handler compat.
	TestLLMTemperature *float64
	TestLLMSeed        int64
	TaskExecutor       rtexec.TaskExecutor
	Workspace          *workspace.Content

	// Team/Autonomy registries shim for extended handlers compat (non core TCB).
	TeamRegistry     *teamRegistry
	AutonomyRegistry *autonomyRegistry

	// Git and lookup shims (non-TCB but used by handlers).
	GitManager  *gitmanager.Manager
	LookupStore *lookup.Store

	// Test-only shims for live/chat tests
	OllamaHTTPClient *http.Client
	Court            *court.Engine
	Vault            *vault.Vault

	// CourtClient and BuilderClient are thin seams for work that lives outside
	// the Host Daemon TCB (Court VMs + Scribe, Builder VMs). The daemon never
	// runs governance or build orchestration logic itself.
	CourtClient  CourtClient
	BuilderClient BuilderClient

	// AgentVMID / AegisHubVMID track the core microVMs the daemon launches and
	// watches. PortalVMID and dashboard concerns are non-TCB and removed.
	AegisHubVMID string
	AgentVMID    string
	agentVMMu    sync.Mutex

	// Portal shims for dashboard (non-TCB, to be removed when dashboard stubbed).
	PortalVMID string
	portalVMMu sync.Mutex

	// SafeMode is retained for minimal operational control.
	SafeMode atomic.Bool
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

		// All persistent stores are now created here only to be aggregated into
		// the canonical store.Store. Direct access via ProposalStore etc. is
		// forbidden; use env.Store.Proposals() (and siblings) everywhere.
		// This is the enforcement point for the Store abstraction in Phase 1.
		proposalStoreShim, err = proposal.NewStore(cfg.Proposal.StoreDir, logger)
		if err != nil {
			runtimeInitErr = err
			return
		}
		prStorePath := filepath.Join(filepath.Dir(cfg.Audit.Dir), "pullrequests")
		prStoreShim, err = pullrequest.NewStore(prStorePath, logger)
		if err != nil {
			runtimeInitErr = err
			return
		}
		compStoreShim, err = composition.NewStore(cfg.Composition.Dir)
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
		memStoreShim, err = memory.NewStore(memory.StoreConfig{
			Dir:          cfg.Memory.Dir,
			MaxSizeMB:    cfg.Memory.MaxSizeMB,
			DefaultTTL:   ttl,
			PIIRedaction: cfg.Memory.PIIRedaction,
		}, memIdentity)
		if err != nil {
			runtimeInitErr = err
			return
		}
		evtBusShim, err = eventbus.New(eventbus.Config{
			Dir:              cfg.EventBus.Dir,
			MaxPendingTimers: cfg.EventBus.MaxPendingTimers,
			MaxSubscriptions: cfg.EventBus.MaxSubscriptions,
		})
		if err != nil {
			runtimeInitErr = err
			return
		}
		wrkStoreShim, err = worker.NewStore(cfg.Worker.Dir)
		if err != nil {
			runtimeInitErr = err
			return
		}

		// IMPORTANT: Vault (secret store) is deliberately NOT initialized here.
		// Per host-daemon.md the daemon must never handle secrets. Any secret
		// paths are stubbed or moved to AegisHub / dedicated VMs.

		// Wrap the concrete stores behind the single Store interface.
		// This becomes env.Store and is the only way to access state.
		storeInst = store.NewLocal(proposalStoreShim, prStoreShim, compStoreShim, memStoreShim, wrkStoreShim, evtBusShim)
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
		Store:            storeInst,
		ProposalStore:    proposalStoreShim,
		PRStore:          prStoreShim,
		CompositionStore: compStoreShim,
		MemoryStore:      memStoreShim,
		EventBus:         evtBusShim,
		WorkerStore:      wrkStoreShim,
		CourtClient:      noopCourtClient{},
		BuilderClient:    noopBuilderClient{},
		Sessions:         sessions.NewStore(),
		ToolEvents:       NewToolEventBuffer(400),
		ThoughtEvents:    NewThoughtEventBuffer(600),
		LLMProxy:           llm.NewOllamaProxy(llm.AllowedModelsFromRegistry(), "", kern, logger),
		TestLLMTemperature: nil,
		TestLLMSeed:        0,
		TaskExecutor:       nil,
		Workspace:          loadWorkspaceIfNeeded(cfg, logger),
		GitManager:         nil, // set later if needed
		LookupStore:        nil,
		OllamaHTTPClient:   &http.Client{},
		Court:              nil,
		Vault:              nil,
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

// loadWorkspaceIfNeeded is a minimal stub retained for shim compat during TCB refactor.
func loadWorkspaceIfNeeded(cfg *config.Config, logger *zap.Logger) *workspace.Content {
	return &workspace.Content{}
}
