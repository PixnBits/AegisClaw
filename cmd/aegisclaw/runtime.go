package main

import (
	"context"
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

// ... (var declarations and runtimeEnv struct remain similar)

// launchStoreVM is the Phase 2.10 integration point.
// Currently starts the in-process StoreVM.
// Future: Will launch a real Firecracker Store microVM and return a remote client.
func launchStoreVM(cfg *config.Config, logger *zap.Logger) (store.StoreVM, error) {
	logger.Info("Launching Store VM (Phase 2.10)")

	// For now we use the dual-mode NewStoreVM (supports in-process + remote hook)
	svm, err := store.NewStoreVM(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create StoreVM: %w", err)
	}

	if err := svm.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("StoreVM start failed: %w", err)
	}

	logger.Info("Store VM launched successfully")
	return svm, nil
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
		// ... existing sandbox, registry, eventbus, lookup, git setup ...
	})

	// Phase 2.10: Launch Store VM via dedicated function
	svm, err := launchStoreVM(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to launch Store VM: %w", err)
	}

	// ... rest of initRuntime (courtClient, builderClient, return runtimeEnv with StoreVM: svm) ...

	courtClient := &court.StubClient{}
	builderClient := &builder.StubClient{}

	return &runtimeEnv{
		// ... existing fields ...
		Store:   svm.Store(),
		StoreVM: svm,
		// ...
	}, nil
}

// Graceful shutdown helper (can be called from main shutdown path)
func shutdownStoreVM(svm store.StoreVM, logger *zap.Logger) {
	if svm != nil {
		logger.Info("Shutting down Store VM...")
		_ = svm.Stop(context.Background())
	}
}
