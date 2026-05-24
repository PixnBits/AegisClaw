// Package runtime provides orchestration of sandboxed environments.
package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"

	"AegisClaw/internal/config"
	"AegisClaw/internal/sandbox"
	"AegisClaw/internal/security"
)

// Orchestrator manages the lifecycle of all sandboxes.
type Orchestrator struct {
	config    *config.Config
	backend   sandbox.Backend
	secMgr    *security.Manager
	mu        sync.RWMutex
	vms       map[string]*VMLifecycle
	startTime int64
}

// VMLifecycle tracks the lifecycle of a VM instance.
type VMLifecycle struct {
	ID        string
	Type      string // "agent", "web-portal", "builder", etc.
	Status    sandbox.Status
	Config    sandbox.VMConfig
	CreatedAt int64
}

// New creates a new Orchestrator.
func New(cfg *config.Config) (*Orchestrator, error) {
	backend, err := sandbox.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox backend: %w", err)
	}

	secMgr := security.NewManager(cfg.StateDir)
	if err := secMgr.Load(); err != nil {
		return nil, fmt.Errorf("failed to load security keys: %w", err)
	}

	return &Orchestrator{
		config:  cfg,
		backend: backend,
		secMgr:  secMgr,
		vms:     make(map[string]*VMLifecycle),
	}, nil
}

// StartVM starts a new sandbox VM.
func (o *Orchestrator) StartVM(ctx context.Context, vmType string, id string, image string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if _, exists := o.vms[id]; exists {
		return fmt.Errorf("VM %s already running", id)
	}

	logrus.Infof("Starting %s VM %s with image %s", vmType, id, image)

	// Per-VM key generation + distribution (Host Daemon TCB duty)
	vmKP, err := o.secMgr.GenerateVMKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate per-VM keypair: %w", err)
	}

	// Create VM config with security settings (VM's own keypair for signing its messages)
	vmConfig := sandbox.VMConfig{
		ID:         id,
		Image:      image,
		Memory:     512, // Default 512MB
		VCpus:      1,   // Default 1 vCPU
		PublicKey:  vmKP.PublicKey,
		PrivateKey: vmKP.PrivateKey, // backend will inject; we zero local copy below
		NetworkConfig: &sandbox.NetworkConfig{
			VsockPort: uint32(9000 + len(o.vms)), // Allocate sequential vsock ports
		},
	}

	// Best-effort zero the private material in this scope after handing to config/backend
	// (real zeroization + proof is strengthened in dedicated TCB tests)
	for i := range vmKP.PrivateKey {
		vmKP.PrivateKey[i] = 0
	}

	// For Firecracker on Linux, set kernel and rootfs paths
	if o.config.SandboxType == config.Firecracker {
		vmConfig.KernelPath = o.config.KernelPath
		vmConfig.RootfsPath = o.config.RootfsDir + "/" + vmType + ".img"
	}

	if err := o.backend.Start(ctx, vmConfig); err != nil {
		logrus.Errorf("Failed to start VM %s: %v", id, err)
		return err
	}

	o.vms[id] = &VMLifecycle{
		ID:     id,
		Type:   vmType,
		Status: sandbox.StatusRunning,
		Config: vmConfig,
	}

	// Register the VM's public key with the security manager so AegisHub etc. can verify its signatures.
	// (The private key has been handed off via vmConfig to the backend for injection into the VM.)
	o.secMgr.RegisterVM(id, vmConfig.PublicKey)

	logrus.Infof("VM %s started successfully (per-VM key distributed + registered)", id)
	return nil
}

// StopVM stops a running sandbox VM.
func (o *Orchestrator) StopVM(ctx context.Context, id string) error {
	o.mu.Lock()
	_, exists := o.vms[id]
	if !exists {
		o.mu.Unlock()
		return fmt.Errorf("VM %s not running", id)
	}
	delete(o.vms, id)
	o.mu.Unlock()

	logrus.Infof("Stopping VM %s", id)

	if err := o.backend.Stop(ctx, id); err != nil {
		logrus.Errorf("Failed to stop VM %s: %v", id, err)
		return err
	}

	logrus.Infof("VM %s stopped", id)
	return nil
}

// GetVMStatus returns the current status of a VM.
func (o *Orchestrator) GetVMStatus(ctx context.Context, id string) (sandbox.Status, error) {
	return o.backend.Status(ctx, id)
}

// ListVMs returns information about all running VMs.
func (o *Orchestrator) ListVMs(ctx context.Context) ([]VMLifecycle, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	vms := make([]VMLifecycle, 0, len(o.vms))
	for _, lifecycle := range o.vms {
		vms = append(vms, *lifecycle)
	}
	return vms, nil
}

// Shutdown gracefully shuts down all VMs.
func (o *Orchestrator) Shutdown(ctx context.Context) error {
	logrus.Info("Shutting down orchestrator")
	return o.backend.Cleanup(ctx)
}

// Config returns the runtime configuration.
func (o *Orchestrator) Config() *config.Config {
	return o.config
}

// SecurityManager returns the security manager.
func (o *Orchestrator) SecurityManager() *security.Manager {
	return o.secMgr
}

// SignAuditRoot signs a Merkle tree root (or other audit blob) using the
// daemon's key. This fulfills the Host Daemon responsibility for tamper-evident
// audit log signing.
func (o *Orchestrator) SignAuditRoot(root []byte) (string, error) {
	if o.secMgr == nil {
		return "", fmt.Errorf("security manager not initialized")
	}
	return o.secMgr.Sign(root)
}
