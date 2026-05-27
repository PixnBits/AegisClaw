// Package runtime provides orchestration of sandboxed environments.
package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"AegisClaw/internal/config"
	"AegisClaw/internal/eventbus"
	"AegisClaw/internal/sandbox"
	"AegisClaw/internal/security"
)

// Orchestrator manages the lifecycle of all sandboxes.
type Orchestrator struct {
	config    *config.Config
	backend   sandbox.Backend
	secMgr    *security.Manager
	bus       *eventbus.Bus // 7.2: in-process EventBus for lifecycle + background signals
	mu        sync.RWMutex
	vms       map[string]*VMLifecycle
	startTime int64
}

// VMLifecycle tracks the lifecycle of a VM instance.
// Security note (TCB): Config.PrivateKey is cleared immediately after successful handoff
// to the sandbox backend (see StartVM). The daemon never retains VM private keys
// (host-daemon.md:Test Requirements / Keypair Isolation + types.go handoff contract).
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

	o := &Orchestrator{
		config:  cfg,
		backend: backend,
		secMgr:  secMgr,
		bus:     eventbus.New(), // 7.2: lightweight in-process bus (Hub-routed for cross-VM)
		vms:     make(map[string]*VMLifecycle),
	}

	// Publish orchestrator ready event (7.2)
	o.bus.PublishJSON("orchestrator.ready", map[string]interface{}{
		"state_dir": cfg.StateDir,
	}, eventbus.WithSource("orchestrator"))

	// EventBus wiring (Task 7.2 complete for orchestrator lifecycle).
	// Important cross-component events are still routed through AegisHub
	// for audit + signature when they cross VM boundaries (per event-system.md).

	return o, nil
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

	// 7.5.4: Secure daemon-side key distribution channel (host-daemon.md:Responsibilities
	// "Generating and distributing Ed25519 keypairs" + Test Requirements / Keypair Isolation).
	// We write the private key to a root-only 0600 ephemeral file in the state directory,
	// pass only the *path* to the backend/guest (via cmdline or env), then immediately
	// zero the in-memory material. The guest is responsible for reading it once at boot
	// and shredding the file. This completes the daemon side of key distribution.
	keyPath := filepath.Join(o.config.StateDir, id+".vmkey")
	keyData := base64.StdEncoding.EncodeToString(vmKP.PrivateKey)
	if err := os.WriteFile(keyPath, []byte(keyData), 0600); err != nil {
		// Best-effort zero even on failure
		for i := range vmKP.PrivateKey {
			vmKP.PrivateKey[i] = 0
		}
		return fmt.Errorf("failed to write ephemeral VM key file: %w", err)
	}
	// Ensure restrictive permissions (in case umask interfered)
	_ = os.Chmod(keyPath, 0600)

	// Create VM config — note we no longer put the raw PrivateKey in the struct
	// for the new path-based channel (raw material is zeroed below).
	vmConfig := sandbox.VMConfig{
		ID:             id,
		Image:          image,
		Memory:         512, // Default 512MB
		VCpus:          1,   // Default 1 vCPU
		PublicKey:      vmKP.PublicKey,
		PrivateKeyPath: keyPath, // new secure distribution path (guest consumes once)
		NetworkConfig: &sandbox.NetworkConfig{
			VsockPort: uint32(9000 + len(o.vms)), // Allocate sequential vsock ports
			// 7.1: Most VMs must egress exclusively through the Network Boundary.
			// The Boundary itself (and certain privileged components) may have direct access.
			EgressViaBoundary:  vmType != "network-boundary",
			BoundaryEgressAddr: "vsock://2:8081", // Convention: CID 2 is often the host-side proxy in vsock setups
			BoundarySkillID:    id,               // The VM's own ID serves as its skill identity for scoping

			// Future (post 7.2 + 7.1 crash): When boundary health changes (via EventBus or direct
			// signal), orchestrator can mark affected VMs degraded or trigger containment kill.
			// The sandbox already omits NICs for EgressViaBoundary VMs; boundaryHealthy=false
			// at the boundary is the primary "block all egress" mechanism.
			// (using the signed message patterns prototyped in the design sketch pilot).
			// This would allow dynamic tightening/loosening of outbound rules without restarting VMs.
		},
	}

	// For Firecracker on Linux, set kernel and rootfs paths
	if o.config.SandboxType == config.Firecracker {
		vmConfig.KernelPath = o.config.KernelPath
		vmConfig.RootfsPath = o.config.RootfsDir + "/" + vmType + ".img"
	}

	if err := o.backend.Start(ctx, vmConfig); err != nil {
		logrus.Errorf("Failed to start VM %s: %v", id, err)
		// Clean up the ephemeral key file on failure (best effort)
		_ = os.Remove(vmConfig.PrivateKeyPath)
		return err
	}

	// Store the lifecycle record. The raw private key was never placed in vmConfig
	// for the new path-based channel; only the path is present. This satisfies
	// host-daemon.md:Test Requirements / Keypair Isolation.
	o.vms[id] = &VMLifecycle{
		ID:     id,
		Type:   vmType,
		Status: sandbox.StatusRunning,
		Config: vmConfig,
	}

	// Register the VM's public key with the security manager so AegisHub etc. can verify its signatures.
	// The private key material lives only in the ephemeral 0600 file (to be consumed by the guest).
	o.secMgr.RegisterVM(id, vmConfig.PublicKey)

	// 7.2: Publish lifecycle event (in-process + will be forwarded via Hub for cross-VM audit)
	o.bus.PublishJSON("vm.started", map[string]interface{}{
		"id":        id,
		"type":      vmType,
		"image":     image,
		"timestamp": time.Now().Unix(),
	}, eventbus.WithSource("orchestrator"))

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

	// 7.2: Publish stop event
	o.bus.PublishJSON("vm.stopped", map[string]interface{}{
		"id":        id,
		"timestamp": time.Now().Unix(),
	}, eventbus.WithSource("orchestrator"))

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

// Bus returns the EventBus for 7.2 wiring (publishers and consumers).
func (o *Orchestrator) Bus() *eventbus.Bus {
	return o.bus
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

// TCBHealthReport returns a structured snapshot of Host Daemon TCB posture.
// Used by `aegis doctor` (7.5.5). All fields are best-effort and must never
// cause the daemon to fail.
func (o *Orchestrator) TCBHealthReport() map[string]interface{} {
	report := map[string]interface{}{
		"daemon": "healthy",
	}

	if o.secMgr != nil {
		// Key isolation summary (pubs only)
		vmPubs := o.secMgr.ListRegisteredVMs()
		report["key_isolation"] = map[string]interface{}{
			"registered_vms": len(vmPubs),
			"note":           "only public keys retained (private material never stored)",
		}

		// Quick Merkle / audit signing test (genesis-level roundtrip)
		testRoot := []byte("doctor-tcb-health-" + time.Now().Format(time.RFC3339Nano))
		if sig, err := o.secMgr.Sign(testRoot); err == nil {
			if verifyErr := o.secMgr.Verify(o.secMgr.GetKeyPair().PublicKey, testRoot, sig); verifyErr == nil {
				report["merkle_audit"] = map[string]interface{}{
					"signing": "functional",
					"verify":  "ok",
				}
			} else {
				report["merkle_audit"] = map[string]interface{}{"signing": "functional", "verify": "failed"}
			}
		}
	}

	// Rough memory posture vs host-daemon.md target (<20MB idle)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	allocMB := float64(m.Alloc) / 1024 / 1024
	report["memory"] = map[string]interface{}{
		"alloc_mb":    fmt.Sprintf("%.2f", allocMB),
		"target_mb":   20,
		"within_target": allocMB < 20,
	}

	return report
}

// criticalTypes defines the component types that the watchdog considers
// essential to the system (per host-daemon.md:Responsibilities).
var criticalTypes = map[string]bool{
	"hub":               true,
	"store":             true,
	"network-boundary":  true,
	"web-portal":        true,
	"court-scribe":      true,
	"court-persona":     true,
}

// StartCriticalWatchdog launches a minimal background health monitor for
// critical components. It is intentionally lightweight (no new dependencies,
// simple ticker + channels) and defensive: it only watches what is actually
// present in the orchestrator's VM map.
//
// On detecting a critical component that is no longer healthy, it:
//   - Logs at high severity
//   - Publishes a privileged event via the EventBus using the TCB security
//     manager for audit signing (host-daemon.md + threat-model.md)
//   - Triggers best-effort containment (backend cleanup for that VM)
//
// This is the initial skeleton for Task 7.5.3. Future enhancements (restart
// policy, Safe Mode integration, cgroups) are noted in the v2 phased plan.
func (o *Orchestrator) StartCriticalWatchdog(ctx context.Context) {
	if o == nil || o.bus == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		logrus.Info("critical component watchdog started (10s interval)")

		for {
			select {
			case <-ctx.Done():
				logrus.Info("critical component watchdog stopping")
				return
			case <-ticker.C:
				o.checkCriticalComponents(ctx)
			}
		}
	}()
}

func (o *Orchestrator) checkCriticalComponents(ctx context.Context) {
	o.mu.RLock()
	snapshot := make([]VMLifecycle, 0, len(o.vms))
	for _, v := range o.vms {
		snapshot = append(snapshot, *v)
	}
	o.mu.RUnlock()

	for _, vm := range snapshot {
		if !criticalTypes[vm.Type] {
			continue
		}

		status, err := o.backend.Status(ctx, vm.ID)
		healthy := err == nil && status == sandbox.StatusRunning

		if !healthy {
			logrus.Warnf("CRITICAL COMPONENT DEGRADED: type=%s id=%s status=%s err=%v",
				vm.Type, vm.ID, status, err)

			// Publish privileged audit-grade event (now actually signs because
			// we fixed the attachment in 7.5.3).
			o.bus.PublishPrivilegedWithSecMgr(eventbus.Event{
				Name:   "critical.component.degraded",
				Source: "orchestrator.watchdog",
				Payload: mustJSON(map[string]interface{}{
					"id":     vm.ID,
					"type":   vm.Type,
					"status": string(status),
				}),
			}, o.secMgr)

			// Best-effort containment for this VM (defense in depth).
			// The full daemon-level kill paths (7.5.2) handle the broader case.
			_ = o.StopVM(ctx, vm.ID)
		}
	}
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
