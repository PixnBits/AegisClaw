package main

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	runtimeOnce     sync.Once
	runtimeInst     *sandbox.FirecrackerRuntime
	registryInst    *sandbox.SkillRegistry
	proposalInst    *proposal.Store
	compositionInst *composition.Store
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
	Court            *court.Engine
	LLMProxy         *llm.OllamaProxy
	ToolProxy        *ToolProxy
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
		LLMProxy:         llm.NewOllamaProxy(llm.AllowedModelsFromRegistry(), "", kern, logger),
	}, nil
}

// generateVMID produces a short, human-readable VM identifier with the given
// prefix (e.g. "aegishub", "agent", "court") and a random 8-character suffix.
// The format is: "<prefix>-<8-hex-chars>". All VM IDs in the daemon use this
// helper so the format stays consistent and is easy to change in one place.
func generateVMID(prefix string) string {
	return prefix + "-" + uuid.New().String()[:8]
}
