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
	proposalInst    *proposal.Store // Deprecated (Minimal Phase 2): owned by StoreVM
	prStoreInst     *pullrequest.Store // Deprecated
	compositionInst *composition.Store // Deprecated
	memoryInst      *memory.Store      // Deprecated
	eventBusInst    *eventbus.Bus
	workerStoreInst *worker.Store // Deprecated
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

	// Clean abstractions
	Store         store.Store
	CourtClient   court.Client
	BuilderClient builder.Client

	ProposalEventDispatcher *events.ProposalEventDispatcher

	LLMProxy         *llm.OllamaProxy
	OllamaHTTPClient *http.Client
	ToolEvents       *ToolEventBuffer
	ThoughtEvents    *ThoughtEventBuffer
	SafeMode         atomic.Bool
	TestLLMTemperature *float64
	TestLLMSeed        int64

	TaskExecutor rtexec.TaskExecutor

	EgressProxy *llm.EgressProxy
	Workspace   *workspace.Content
	GitManager  *gitmanager.Manager
	Sessions    *sessions.Store // Deprecated – Phase 3.3

	AgentVMID string
	agentVMMu sync.Mutex

	AegisHubVMID string

	PortalVMID string
	portalVMMu sync.Mutex

	TeamRegistry     *teamRegistry     // Deprecated
	AutonomyRegistry *autonomyRegistry // Deprecated

	// StoreVM owns persistent state creation
	StoreVM store.StoreVM
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
		// NOTE: All persistent stores are now created inside LocalStoreVM
		// The old direct creation calls have been removed.
		eventBusInst, runtimeInitErr = eventbus.New(eventbus.Config{
			Dir:              cfg.EventBus.Dir,
			MaxPendingTimers: cfg.EventBus.MaxPendingTimers,
			MaxSubscriptions: cfg.EventBus.MaxSubscriptions,
		})
		if runtimeInitErr != nil {
			return
		}
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

	// Create StoreVM which now owns persistent store creation
	localStoreVM, err := store.NewLocalStoreVM(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create LocalStoreVM: %w", err)
	}

	courtClient := &court.StubClient{}
	builderClient := &builder.StubClient{}

	return &runtimeEnv{
		Logger:        logger,
		Config:        cfg,
		Kernel:        kern,
		Runtime:       runtimeInst,
		Registry:      registryInst,
		Store:         localStoreVM.Store(),
		CourtClient:   courtClient,
		BuilderClient: builderClient,
		EgressProxy:   llm.NewEgressProxy(logger),
		LookupStore:   lookupInst,
		GitManager:    gitManagerInst,
		LLMProxy:      llm.NewOllamaProxy(llm.AllowedModelsFromRegistry(), "", kern, logger),
		ToolEvents:    NewToolEventBuffer(400),
		ThoughtEvents: NewThoughtEventBuffer(600),
		Workspace:     loadWorkspace(cfg, logger),
		Sessions:      sessions.NewStore(),
		StoreVM:       localStoreVM,
	}, nil
}

// ... rest of the file remains the same (layoutFromConfig, resetRuntimeSingletons, etc.)
