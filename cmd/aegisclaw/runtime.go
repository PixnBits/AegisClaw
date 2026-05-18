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

// launchStoreVM decides between in-process and real Firecracker Store VM.
func launchStoreVM(cfg *config.Config, logger *zap.Logger) (store.StoreVM, error) {
	mode := os.Getenv("STORE_VM_MODE")

	if mode == "real" || mode == "firecracker" {
		logger.Info("Launching REAL Firecracker Store VM")
		return launchRealFirecrackerStoreVM(cfg, logger)
	}

	// Default: in-process (current safe path)
	logger.Info("Launching in-process Store VM (default)")
	return store.NewStoreVM(cfg, logger)
}

// launchRealFirecrackerStoreVM starts a real Store microVM.
// This is the beginning of real Firecracker integration.
func launchRealFirecrackerStoreVM(cfg *config.Config, logger *zap.Logger) (store.StoreVM, error) {
	spec := sandbox.DefaultStoreVMSpec()

	logger.Info("Would launch Firecracker Store VM",
		zap.Uint32("vsockCID", spec.VsockCID),
		zap.Uint32("vsockPort", spec.VsockPort),
		zap.String("rootfs", spec.RootfsPath))

	// TODO: Use sandbox.FirecrackerRuntime to actually create/start the VM
	// For now return a remote client that points at the future VM
	client, err := store.NewRemoteClient(fmt.Sprintf("vsock://%d:%d", spec.VsockCID, spec.VsockPort))
	if err != nil {
		return nil, fmt.Errorf("failed to create remote client for Store VM: %w", err)
	}

	return &remoteStoreVMAdapter{client: client}, nil
}

// remoteStoreVMAdapter (simple wrapper)
type remoteStoreVMAdapter struct {
	client interface {
		Store() store.Store
	}
}

func (a *remoteStoreVMAdapter) Start(ctx context.Context) error { return nil }
func (a *remoteStoreVMAdapter) Stop(ctx context.Context) error  { return nil }
func (a *remoteStoreVMAdapter) Store() store.Store { return a.client.Store() }

var _ store.StoreVM = (*remoteStoreVMAdapter)(nil)

// initRuntime and other functions remain as before...
