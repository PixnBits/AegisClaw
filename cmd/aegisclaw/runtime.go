package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"filippo.io/age"
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
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	runtimeOnce    sync.Once
	runtimeInst    *sandbox.FirecrackerRuntime
	registryInst   *sandbox.SkillRegistry
	runtimeInitErr error
)

type runtimeEnv struct {
	Logger  *zap.Logger
	Config  *config.Config
	Kernel  *kernel.Kernel
	Runtime *sandbox.FirecrackerRuntime
	// Registry manages skill sandboxes for VM lifecycle (core TCB responsibility).
	Registry *sandbox.SkillRegistry

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

	// StoreVMID tracks the dedicated Store VM launched by the daemon.
	// The daemon is only responsible for launch + watchdog. Persistent state
	// access is now fully external (Store VM via AegisHub mediation).
	StoreVMID string

	// SafeMode is retained for minimal operational control during startup.
	SafeMode atomic.Bool

	// Legacy shims retained temporarily for handler compatibility during
	// the transition to remote-only Store VM. Direct store creation was
	// removed in Phase 3; these fields are nil or unused in normal paths.
	// They will be fully removed once all call sites are stubbed (Phase 4 prep).
	TeamRegistry     *teamRegistry
	AutonomyRegistry *autonomyRegistry
	Sessions         *sessions.Store
	portalVMMu       sync.Mutex
	PortalVMID       string
	ProposalStore    *proposal.Store
	PRStore          *pullrequest.Store
	MemoryStore      *memory.Store
	EventBus         *eventbus.Bus
	GitManager       *gitmanager.Manager
	LookupStore      *lookup.Store
}

func initRuntime() (*runtimeEnv, error) {
	var logger *zap.Logger
	var err error

	if os.Getenv("AEGISCLAW_TEST_LIGHTWEIGHT") != "" {
		// Lightweight mode for structural TCB tests and E2E children.
		// Avoids heavy zap sampler allocations and production logging overhead
		// that have repeatedly caused OOMs in CI on GitHub runners.
		logger = zap.NewNop()
	} else {
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
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

		// Phase 5: No general Store interface or remoteStore remains in the daemon.
		// The Host Daemon only manages VM lifecycle and lightweight Composition Manifest.
		// All persistent state (proposals, workers, events, etc.) lives in the Store VM
		// and is accessed via AegisHub mediation.
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
