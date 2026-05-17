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
	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/events"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/lookup"
	"github.com/PixnBits/AegisClaw/internal/memory"
	aegispaths "github.com/PixnBits/AegisClaw/internal/paths"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"github.com/PixnBits/AegisClaw/internal/store"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/PixnBits/AegisClaw/internal/workspace"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	runtimeOnce     sync.Once
	runtimeInst     *sandbox.FirecrackerRuntime
	registryInst    *sandbox.SkillRegistry
	proposalInst    *proposal.Store
	prStoreInst     *pullrequest.Store
	compositionInst *composition.Store
	memoryInst      *memory.Store
	eventBusInst    *eventbus.Bus
	workerStoreInst *worker.Store
	// vaultInst removed — Host Daemon no longer opens or operates on secrets vault.
	// Secret handling is the responsibility of the Network Boundary VM only.
	lookupInst      *lookup.Store
	gitManagerInst  *gitmanager.Manager
	runtimeInitErr  error
)

type runtimeEnv struct {
	Logger   *zap.Logger
	Config   *config.Config
	Kernel   *kernel.Kernel
	Runtime  *sandbox.FirecrackerRuntime
	Registry *sandbox.SkillRegistry

	// Clean abstractions introduced in Phase 1
	Store         store.Store
	CourtClient   court.Client
	BuilderClient builder.Client

	// Court Engine removed from Host Daemon TCB.
	// All governance now routes through CourtClient to Court VMs + Court Scribe.
	// BuildOrchestrator removed; coordination lives in AegisHub / Builder VMs.
	ProposalEventDispatcher *events.ProposalEventDispatcher

	LLMProxy         *llm.OllamaProxy
	OllamaHTTPClient *http.Client
	ToolEvents       *ToolEventBuffer
	ThoughtEvents    *ThoughtEventBuffer
	SafeMode         atomic.Bool
	TestLLMTemperature *float64
	TestLLMSeed        int64

	TaskExecutor rtexec.TaskExecutor

	// Vault field removed — Host Daemon must never open or operate on the secrets vault.
	// All secret operations are delegated to the Network Boundary VM.

	EgressProxy *llm.EgressProxy
	Workspace   *workspace.Content
	GitManager  *gitmanager.Manager
	// Sessions is deprecated for daemon API surface (Phase 3.3 TCB reduction).
	// Session management fully moved to AegisHub + Session VMs.
	Sessions    *sessions.Store // Deprecated – only transitional references remain

	AgentVMID string
	agentVMMu sync.Mutex

	AegisHubVMID string

	PortalVMID string
	portalVMMu sync.Mutex

	TeamRegistry     *teamRegistry     // Deprecated (Phase 3.4): team logic moved to AegisHub / Store VM
	AutonomyRegistry *autonomyRegistry // Deprecated (Phase 3.4): autonomy grants moved out of daemon TCB
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
	if err := aegispaths.EnsureSecureDirectories(layoutFromConfig(cfg)); err != nil {
		return nil, fmt.Errorf("secure directory layout check failed: %w", err)
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
		prStorePath := filepath.Join(filepath.Dir(cfg.Audit.Dir), "pullrequests")
		prStoreInst, runtimeInitErr = pullrequest.NewStore(prStorePath, logger)
		if runtimeInitErr != nil {
			return
		}
		compositionInst, runtimeInitErr = composition.NewStore(cfg.Composition.Dir)
		if runtimeInitErr != nil {
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
		memoryInst, runtimeInitErr = memory.NewStore(memory.StoreConfig{
			Dir:          cfg.Memory.Dir,
			MaxSizeMB:    cfg.Memory.MaxSizeMB,
			DefaultTTL:   ttl,
			PIIRedaction: cfg.Memory.PIIRedaction,
		}, memIdentity)
		if runtimeInitErr != nil {
			return
		}
		eventBusInst, runtimeInitErr = eventbus.New(eventbus.Config{
			Dir:              cfg.EventBus.Dir,
			MaxPendingTimers: cfg.EventBus.MaxPendingTimers,
			MaxSubscriptions: cfg.EventBus.MaxSubscriptions,
		})
		if runtimeInitErr != nil {
			return
		}
		workerStoreInst, runtimeInitErr = worker.NewStore(cfg.Worker.Dir)
		if runtimeInitErr != nil {
			return
		}
		// Vault initialization removed from Host Daemon (TCB rule: never handle secrets).
		// The encrypted vault at cfg.Vault.Dir is now owned exclusively by Network Boundary VM.
		// Daemon only ensures the directory exists with correct 0700 permissions via EnsureSecureDirectories.
		lookupInst, runtimeInitErr = lookup.NewStore(lookup.StoreConfig{
			Dir:    cfg.Lookup.Dir,
			Logger: logger,
		})
		if runtimeInitErr != nil {
			return
		}
		gitBasePath := filepath.Join(filepath.Dir(cfg.Audit.Dir), "git")
		gitManagerInst, runtimeInitErr = gitmanager.NewManager(gitBasePath, kern, logger)
	})
	if runtimeInitErr != nil {
		return nil, fmt.Errorf("failed to initialize runtime: %w", runtimeInitErr)
	}

	unifiedStore := store.NewLocal(
		proposalInst,
		prStoreInst,
		compositionInst,
		memoryInst,
		workerStoreInst,
		nil,
	)

	courtClient := &court.StubClient{}
	builderClient := &builder.StubClient{}

	return &runtimeEnv{
		Logger:        logger,
		Config:        cfg,
		Kernel:        kern,
		Runtime:       runtimeInst,
		Registry:      registryInst,
		Store:         unifiedStore,
		CourtClient:   courtClient,
		BuilderClient: builderClient,
		// Vault: removed — secrets never touched by Host Daemon TCB
		EgressProxy:   llm.NewEgressProxy(logger),
		LookupStore:   lookupInst,
		GitManager:    gitManagerInst,
		LLMProxy:      llm.NewOllamaProxy(llm.AllowedModelsFromRegistry(), "", kern, logger),
		ToolEvents:    NewToolEventBuffer(400),
		ThoughtEvents: NewThoughtEventBuffer(600),
		Workspace:     loadWorkspace(cfg, logger),
		Sessions:      sessions.NewStore(),
	}, nil
}

func layoutFromConfig(cfg *config.Config) aegispaths.Layout {
	defaultLayout, _ := aegispaths.DefaultLayout()
	layout := defaultLayout
	if cfg == nil {
		return layout
	}
	layout.SocketPath = cfg.Daemon.SocketPath
	layout.AuditDir = cfg.Audit.Dir
	layout.SecretsDir = cfg.Vault.Dir
	layout.WorkspaceDir = cfg.Workspace.Dir
	layout.VMDir = filepath.Dir(cfg.Sandbox.StateDir)
	layout.RegistryDir = filepath.Dir(cfg.Sandbox.RegistryPath)
	layout.ProposalDir = cfg.Proposal.StoreDir
	layout.SBOMDir = cfg.Builder.SBOMDir
	return layout
}

func resetRuntimeSingletons() {
	runtimeOnce = sync.Once{}
	runtimeInst = nil
	registryInst = nil
	proposalInst = nil
	prStoreInst = nil
	compositionInst = nil
	memoryInst = nil
	eventBusInst = nil
	workerStoreInst = nil
	vaultInst = nil
	lookupInst = nil
	gitManagerInst = nil
	runtimeInitErr = nil
}

func loadWorkspace(cfg *config.Config, logger *zap.Logger) *workspace.Content {
	dir := cfg.Workspace.Dir
	if dir == "" {
		return &workspace.Content{}
	}
	c, err := workspace.Load(dir)
	if err != nil {
		logger.Warn("workspace load failed; continuing without workspace content",
			zap.String("dir", dir), zap.Error(err))
		return &workspace.Content{}
	}
	if !c.IsEmpty() {
		logger.Info("workspace content loaded",
			zap.String("dir", dir),
			zap.Bool("agents", c.Agents != ""),
			zap.Bool("soul", c.Soul != ""),
			zap.Bool("tools", c.Tools != ""),
			zap.Bool("skill", c.Skill != ""),
		)
	}
	return c
}

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

func generateVMID(prefix string) string {
	return prefix + "-" + uuid.New().String()[:8]
}
