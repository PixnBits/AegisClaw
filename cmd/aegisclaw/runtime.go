package main

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

var (
	runtimeOnce      sync.Once
	runtimeInst      *sandbox.FirecrackerRuntime
	registryInst     *sandbox.SkillRegistry
	proposalInst     *proposal.Store
	compositionInst  *composition.Store
	runtimeInitErr   error
)

type runtimeEnv struct {
	Logger           *zap.Logger
	Config           *config.Config
	Kernel           *kernel.Kernel
	Runtime          *sandbox.FirecrackerRuntime
	Registry         *sandbox.SkillRegistry
	ProposalStore    *proposal.Store
	CompositionStore *composition.Store
	SafeMode         atomic.Bool
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
	}, nil
}
