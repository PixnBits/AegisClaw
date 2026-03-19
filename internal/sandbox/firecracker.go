package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	log "github.com/sirupsen/logrus"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

const (
	minVsockCID      = 3
	tapDevicePrefix  = "fc-"
	subnetBase       = "10.0.0."
	subnetMask       = "/30"
	seccompLevel     = 2 // advanced seccomp filtering
	defaultWorkspace = 512
)

// FirecrackerRuntime manages Firecracker microVM sandboxes.
// It implements SandboxManager, routing every operation through the kernel
// for signing and audit logging.
type FirecrackerRuntime struct {
	cfg       RuntimeConfig
	kern      *kernel.Kernel
	logger    *zap.Logger
	sandboxes map[string]*managedSandbox
	mu        sync.RWMutex
	nextCID   uint32
}

type managedSandbox struct {
	info    SandboxInfo
	machine *firecracker.Machine
	cancel  context.CancelFunc
}

// NewFirecrackerRuntime creates a new runtime. It loads persisted state from
// the state directory so that sandboxes survive process restarts.
func NewFirecrackerRuntime(cfg RuntimeConfig, kern *kernel.Kernel, logger *zap.Logger) (*FirecrackerRuntime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid runtime config: %w", err)
	}

	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create state directory %s: %w", cfg.StateDir, err)
	}
	if err := os.MkdirAll(cfg.ChrootBaseDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create chroot base %s: %w", cfg.ChrootBaseDir, err)
	}

	rt := &FirecrackerRuntime{
		cfg:       cfg,
		kern:      kern,
		logger:    logger,
		sandboxes: make(map[string]*managedSandbox),
		nextCID:   minVsockCID,
	}

	if err := rt.loadState(); err != nil {
		logger.Warn("failed to load persisted sandbox state, starting fresh", zap.Error(err))
	}

	return rt, nil
}

// Create provisions a new sandbox from the spec without starting it.
func (r *FirecrackerRuntime) Create(ctx context.Context, spec SandboxSpec) error {
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("invalid sandbox spec: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sandboxes[spec.ID]; exists {
		return fmt.Errorf("sandbox %s already exists", spec.ID)
	}

	// Verify rootfs template exists
	if _, err := os.Stat(spec.RootfsPath); err != nil {
		return fmt.Errorf("rootfs not accessible at %s: %w", spec.RootfsPath, err)
	}

	// Prepare sandbox directories
	sandboxDir := filepath.Join(r.cfg.StateDir, spec.ID)
	if err := os.MkdirAll(sandboxDir, 0700); err != nil {
		return fmt.Errorf("failed to create sandbox dir %s: %w", sandboxDir, err)
	}

	// Copy rootfs template for this sandbox (each VM needs its own copy)
	sandboxRootfs := filepath.Join(sandboxDir, "rootfs.ext4")
	if err := copyFile(spec.RootfsPath, sandboxRootfs); err != nil {
		return fmt.Errorf("failed to copy rootfs for sandbox %s: %w", spec.ID, err)
	}

	// Create workspace overlay image
	workspaceMB := spec.WorkspaceMB
	if workspaceMB <= 0 {
		workspaceMB = defaultWorkspace
	}
	workspacePath := filepath.Join(sandboxDir, "workspace.ext4")
	if err := createExt4Image(workspacePath, workspaceMB); err != nil {
		return fmt.Errorf("failed to create workspace image: %w", err)
	}

	// Assign vsock CID
	if spec.VsockCID < minVsockCID {
		spec.VsockCID = r.allocateCID()
	}

	// Determine socket path for Firecracker API
	socketPath := filepath.Join(sandboxDir, "firecracker.sock")

	info := SandboxInfo{
		Spec:       spec,
		State:      StateCreated,
		SocketPath: socketPath,
	}

	r.sandboxes[spec.ID] = &managedSandbox{info: info}

	// Sign and log creation through kernel
	payload, _ := json.Marshal(spec)
	action := kernel.NewAction(kernel.ActionSandboxCreate, "kernel", payload)
	if _, err := r.kern.SignAndLog(action); err != nil {
		return fmt.Errorf("failed to log sandbox creation: %w", err)
	}

	if err := r.saveState(); err != nil {
		r.logger.Error("failed to persist state after create", zap.Error(err))
	}

	r.logger.Info("sandbox created",
		zap.String("id", spec.ID),
		zap.String("name", spec.Name),
		zap.Uint32("vsock_cid", spec.VsockCID),
	)
	return nil
}

// Start boots the Firecracker microVM for a created or stopped sandbox.
func (r *FirecrackerRuntime) Start(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ms, exists := r.sandboxes[id]
	if !exists {
		return fmt.Errorf("sandbox %s not found", id)
	}
	if ms.info.State == StateRunning {
		return fmt.Errorf("sandbox %s is already running", id)
	}
	if ms.info.State != StateCreated && ms.info.State != StateStopped {
		return fmt.Errorf("sandbox %s is in state %s, cannot start", id, ms.info.State)
	}

	spec := ms.info.Spec
	sandboxDir := filepath.Join(r.cfg.StateDir, spec.ID)

	// Set up network (tap device + nftables)
	tapName, hostIP, guestIP, err := r.setupNetwork(spec)
	if err != nil {
		ms.info.State = StateError
		ms.info.Error = fmt.Sprintf("network setup failed: %v", err)
		return fmt.Errorf("failed to set up network for sandbox %s: %w", id, err)
	}
	ms.info.TapDevice = tapName
	ms.info.HostIP = hostIP
	ms.info.GuestIP = guestIP

	// Apply nftables rules based on network policy
	if err := r.applyNetworkPolicy(spec.ID, tapName, guestIP, &spec.NetworkPolicy); err != nil {
		r.teardownNetwork(tapName, spec.ID)
		ms.info.State = StateError
		ms.info.Error = fmt.Sprintf("network policy failed: %v", err)
		return fmt.Errorf("failed to apply network policy for sandbox %s: %w", id, err)
	}

	// Build Firecracker configuration
	rootfsPath := filepath.Join(sandboxDir, "rootfs.ext4")
	workspacePath := filepath.Join(sandboxDir, "workspace.ext4")
	socketPath := ms.info.SocketPath

	// Remove stale socket
	os.Remove(socketPath)

	fcCfg := r.buildFirecrackerConfig(spec, socketPath, rootfsPath, workspacePath, tapName)

	// Configure jailer for UID/GID isolation
	jailerCfg := r.buildJailerConfig(spec)

	// Create VM with jailer
	vmCtx, vmCancel := context.WithCancel(context.Background())

	logEntry := log.NewEntry(log.New())
	logEntry.Logger.SetLevel(log.WarnLevel)

	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(logEntry.WithField("sandbox_id", spec.ID)),
	}

	// Use jailer if binary exists and is executable
	if _, err := os.Stat(r.cfg.JailerBin); err == nil {
		fcCfg.JailerCfg = &jailerCfg
	}

	machine, err := firecracker.NewMachine(vmCtx, fcCfg, machineOpts...)
	if err != nil {
		vmCancel()
		r.teardownNetwork(tapName, spec.ID)
		ms.info.State = StateError
		ms.info.Error = fmt.Sprintf("failed to create VM: %v", err)
		return fmt.Errorf("failed to create Firecracker VM for sandbox %s: %w", id, err)
	}

	if err := machine.Start(vmCtx); err != nil {
		vmCancel()
		r.teardownNetwork(tapName, spec.ID)
		ms.info.State = StateError
		ms.info.Error = fmt.Sprintf("failed to start VM: %v", err)
		return fmt.Errorf("failed to start Firecracker VM for sandbox %s: %w", id, err)
	}

	now := time.Now().UTC()
	ms.machine = machine
	ms.cancel = vmCancel
	ms.info.State = StateRunning
	ms.info.StartedAt = &now
	ms.info.StoppedAt = nil
	ms.info.Error = ""
	pid, pidErr := machine.PID()
	if pidErr != nil {
		r.logger.Warn("could not get VM PID", zap.String("id", id), zap.Error(pidErr))
	}
	ms.info.PID = pid

	// Sign and log start through kernel
	payload, _ := json.Marshal(map[string]interface{}{
		"id":         id,
		"pid":        ms.info.PID,
		"vsock_cid":  spec.VsockCID,
		"tap_device": tapName,
		"guest_ip":   guestIP,
	})
	action := kernel.NewAction(kernel.ActionSandboxStart, "kernel", payload)
	if _, signErr := r.kern.SignAndLog(action); signErr != nil {
		r.logger.Error("failed to log sandbox start", zap.Error(signErr))
	}

	if err := r.saveState(); err != nil {
		r.logger.Error("failed to persist state after start", zap.Error(err))
	}

	r.logger.Info("sandbox started",
		zap.String("id", id),
		zap.Int("pid", ms.info.PID),
		zap.String("tap", tapName),
		zap.String("guest_ip", guestIP),
	)
	return nil
}

// Stop gracefully shuts down a running sandbox.
func (r *FirecrackerRuntime) Stop(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ms, exists := r.sandboxes[id]
	if !exists {
		return fmt.Errorf("sandbox %s not found", id)
	}
	if ms.info.State != StateRunning {
		return fmt.Errorf("sandbox %s is not running (state: %s)", id, ms.info.State)
	}

	// Graceful shutdown via Firecracker API
	if ms.machine != nil {
		if err := ms.machine.Shutdown(ctx); err != nil {
			r.logger.Warn("graceful shutdown failed, forcing stop",
				zap.String("id", id),
				zap.Error(err),
			)
			if err := ms.machine.StopVMM(); err != nil {
				r.logger.Error("forced stop also failed",
					zap.String("id", id),
					zap.Error(err),
				)
			}
		}
	}

	if ms.cancel != nil {
		ms.cancel()
	}

	// Tear down network
	if ms.info.TapDevice != "" {
		r.teardownNetwork(ms.info.TapDevice, id)
	}

	now := time.Now().UTC()
	ms.info.State = StateStopped
	ms.info.StoppedAt = &now
	ms.info.PID = 0
	ms.machine = nil
	ms.cancel = nil

	payload, _ := json.Marshal(map[string]string{"id": id})
	action := kernel.NewAction(kernel.ActionSandboxStop, "kernel", payload)
	if _, signErr := r.kern.SignAndLog(action); signErr != nil {
		r.logger.Error("failed to log sandbox stop", zap.Error(signErr))
	}

	if err := r.saveState(); err != nil {
		r.logger.Error("failed to persist state after stop", zap.Error(err))
	}

	r.logger.Info("sandbox stopped", zap.String("id", id))
	return nil
}

// Delete removes a stopped sandbox and cleans up all resources.
func (r *FirecrackerRuntime) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ms, exists := r.sandboxes[id]
	if !exists {
		return fmt.Errorf("sandbox %s not found", id)
	}
	if ms.info.State == StateRunning {
		return fmt.Errorf("sandbox %s is still running, stop it first", id)
	}

	// Clean up sandbox directory
	sandboxDir := filepath.Join(r.cfg.StateDir, ms.info.Spec.ID)
	if err := os.RemoveAll(sandboxDir); err != nil {
		r.logger.Warn("failed to remove sandbox directory",
			zap.String("id", id),
			zap.String("dir", sandboxDir),
			zap.Error(err),
		)
	}

	// Clean up chroot jail
	chrootDir := filepath.Join(r.cfg.ChrootBaseDir, "firecracker", id)
	if err := os.RemoveAll(chrootDir); err != nil {
		r.logger.Warn("failed to remove chroot directory",
			zap.String("id", id),
			zap.String("dir", chrootDir),
			zap.Error(err),
		)
	}

	delete(r.sandboxes, id)

	payload, _ := json.Marshal(map[string]string{"id": id})
	action := kernel.NewAction(kernel.ActionSandboxDelete, "kernel", payload)
	if _, signErr := r.kern.SignAndLog(action); signErr != nil {
		r.logger.Error("failed to log sandbox delete", zap.Error(signErr))
	}

	if err := r.saveState(); err != nil {
		r.logger.Error("failed to persist state after delete", zap.Error(err))
	}

	r.logger.Info("sandbox deleted", zap.String("id", id))
	return nil
}

// List returns info for all tracked sandboxes.
func (r *FirecrackerRuntime) List(ctx context.Context) ([]SandboxInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]SandboxInfo, 0, len(r.sandboxes))
	for _, ms := range r.sandboxes {
		result = append(result, ms.info)
	}
	return result, nil
}

// Status returns the info for a single sandbox.
func (r *FirecrackerRuntime) Status(ctx context.Context, id string) (*SandboxInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ms, exists := r.sandboxes[id]
	if !exists {
		return nil, fmt.Errorf("sandbox %s not found", id)
	}
	info := ms.info
	return &info, nil
}

// buildFirecrackerConfig constructs the full Firecracker VM configuration.
func (r *FirecrackerRuntime) buildFirecrackerConfig(
	spec SandboxSpec,
	socketPath, rootfsPath, workspacePath, tapName string,
) firecracker.Config {
	kernelImage := spec.KernelPath
	if kernelImage == "" {
		kernelImage = r.cfg.KernelImage
	}

	drives := firecracker.NewDrivesBuilder(rootfsPath).
		WithRootDrive(rootfsPath, firecracker.WithReadOnly(true)).
		AddDrive(workspacePath, false).
		Build()

	return firecracker.Config{
		SocketPath:      socketPath,
		KernelImagePath: kernelImage,
		KernelArgs:      "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/guest-agent",
		Drives:          drives,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(spec.Resources.VCPUs),
			MemSizeMib: firecracker.Int64(spec.Resources.MemoryMB),
		},
		NetworkInterfaces: []firecracker.NetworkInterface{
			{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					MacAddress:  generateMAC(spec.VsockCID),
					HostDevName: tapName,
				},
			},
		},
		VsockDevices: []firecracker.VsockDevice{
			{
				ID:   "vsock0",
				Path: filepath.Join(filepath.Dir(socketPath), "vsock.sock"),
				CID:  spec.VsockCID,
			},
		},
		Seccomp: firecracker.SeccompConfig{
			Enabled: true,
		},
	}
}

// buildJailerConfig creates the jailer configuration for UID/GID isolation.
func (r *FirecrackerRuntime) buildJailerConfig(spec SandboxSpec) firecracker.JailerConfig {
	// Each sandbox gets a unique UID/GID based on its CID offset.
	// Starting from 10000 to avoid conflicts with system users.
	uid := int(10000 + spec.VsockCID)
	gid := uid

	kernelPath := spec.KernelPath
	if kernelPath == "" {
		kernelPath = r.cfg.KernelImage
	}

	return firecracker.JailerConfig{
		GID:            &gid,
		UID:            &uid,
		ID:             spec.ID,
		NumaNode:       firecracker.Int(0),
		ExecFile:       r.cfg.FirecrackerBin,
		JailerBinary:   r.cfg.JailerBin,
		ChrootBaseDir:  r.cfg.ChrootBaseDir,
		ChrootStrategy: firecracker.NewNaiveChrootStrategy(kernelPath),
	}
}

// setupNetwork creates a tap device and assigns point-to-point IPs.
// Each VM gets its own /30 subnet for L2 isolation.
func (r *FirecrackerRuntime) setupNetwork(spec SandboxSpec) (tapName, hostIP, guestIP string, err error) {
	index := int(spec.VsockCID - minVsockCID)
	subnetOffset := index * 4

	tapName = fmt.Sprintf("%s%s", tapDevicePrefix, spec.ID[:minInt(8, len(spec.ID))])
	hostIP = fmt.Sprintf("%s%d", subnetBase, subnetOffset+1)
	guestIP = fmt.Sprintf("%s%d", subnetBase, subnetOffset+2)

	// Create tap device
	if err := runCmd("ip", "tuntap", "add", "dev", tapName, "mode", "tap"); err != nil {
		return "", "", "", fmt.Errorf("failed to create tap device %s: %w", tapName, err)
	}

	// Assign host IP
	if err := runCmd("ip", "addr", "add", hostIP+subnetMask, "dev", tapName); err != nil {
		runCmd("ip", "link", "delete", tapName)
		return "", "", "", fmt.Errorf("failed to assign IP to %s: %w", tapName, err)
	}

	// Bring interface up
	if err := runCmd("ip", "link", "set", tapName, "up"); err != nil {
		runCmd("ip", "link", "delete", tapName)
		return "", "", "", fmt.Errorf("failed to bring up %s: %w", tapName, err)
	}

	r.logger.Info("network configured",
		zap.String("tap", tapName),
		zap.String("host_ip", hostIP),
		zap.String("guest_ip", guestIP),
	)
	return tapName, hostIP, guestIP, nil
}

// applyNetworkPolicy enforces nftables rules based on the sandbox's NetworkPolicy.
// Default policy is DROP; allowed hosts/ports are selectively permitted.
func (r *FirecrackerRuntime) applyNetworkPolicy(sandboxID, tapName, guestIP string, policy *NetworkPolicy) error {
	chainName := fmt.Sprintf("aegis_%s", sandboxID[:minInt(12, len(sandboxID))])

	// Create per-sandbox nftables chain with default drop
	if err := runCmd("nft", "add", "table", "ip", "aegisclaw"); err != nil {
		r.logger.Warn("nft table may already exist", zap.Error(err))
	}

	if err := runCmd("nft", "add", "chain", "ip", "aegisclaw", chainName,
		"{ type filter hook forward priority 0; policy drop; }"); err != nil {
		return fmt.Errorf("failed to create nftables chain %s: %w", chainName, err)
	}

	// Allow established/related connections
	if err := runCmd("nft", "add", "rule", "ip", "aegisclaw", chainName,
		"ct", "state", "established,related", "accept"); err != nil {
		return fmt.Errorf("failed to add conntrack rule: %w", err)
	}

	// Allow specific hosts and ports
	for _, host := range policy.AllowedHosts {
		for _, port := range policy.AllowedPorts {
			for _, proto := range effectiveProtocols(policy) {
				if err := runCmd("nft", "add", "rule", "ip", "aegisclaw", chainName,
					"ip", "saddr", guestIP,
					"ip", "daddr", host,
					proto, "dport", strconv.Itoa(int(port)),
					"accept"); err != nil {
					r.logger.Warn("failed to add allow rule",
						zap.String("host", host),
						zap.Uint16("port", port),
						zap.String("proto", proto),
						zap.Error(err),
					)
				}
			}
		}
	}

	r.logger.Info("network policy applied",
		zap.String("sandbox_id", sandboxID),
		zap.String("chain", chainName),
		zap.Int("allowed_hosts", len(policy.AllowedHosts)),
		zap.Int("allowed_ports", len(policy.AllowedPorts)),
	)
	return nil
}

// teardownNetwork removes the tap device and nftables rules for a sandbox.
func (r *FirecrackerRuntime) teardownNetwork(tapName, sandboxID string) {
	chainName := fmt.Sprintf("aegis_%s", sandboxID[:minInt(12, len(sandboxID))])

	if err := runCmd("nft", "delete", "chain", "ip", "aegisclaw", chainName); err != nil {
		r.logger.Warn("failed to delete nftables chain",
			zap.String("chain", chainName),
			zap.Error(err),
		)
	}

	if err := runCmd("ip", "link", "delete", tapName); err != nil {
		r.logger.Warn("failed to delete tap device",
			zap.String("tap", tapName),
			zap.Error(err),
		)
	}

	r.logger.Info("network torn down",
		zap.String("tap", tapName),
		zap.String("chain", chainName),
	)
}

// allocateCID returns the next available vsock CID.
func (r *FirecrackerRuntime) allocateCID() uint32 {
	cid := r.nextCID
	r.nextCID++

	// Scan for conflicts with existing sandboxes
	for {
		conflict := false
		for _, ms := range r.sandboxes {
			if ms.info.Spec.VsockCID == cid {
				conflict = true
				break
			}
		}
		if !conflict {
			break
		}
		cid++
		r.nextCID = cid + 1
	}
	return cid
}

// State persistence

type persistedState struct {
	Sandboxes map[string]SandboxInfo `json:"sandboxes"`
	NextCID   uint32                 `json:"next_cid"`
}

func (r *FirecrackerRuntime) statePath() string {
	return filepath.Join(r.cfg.StateDir, "sandboxes.json")
}

func (r *FirecrackerRuntime) saveState() error {
	state := persistedState{
		Sandboxes: make(map[string]SandboxInfo, len(r.sandboxes)),
		NextCID:   r.nextCID,
	}
	for id, ms := range r.sandboxes {
		state.Sandboxes[id] = ms.info
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	tmpPath := r.statePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	if err := os.Rename(tmpPath, r.statePath()); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

func (r *FirecrackerRuntime) loadState() error {
	data, err := os.ReadFile(r.statePath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	r.nextCID = state.NextCID
	if r.nextCID < minVsockCID {
		r.nextCID = minVsockCID
	}

	for id, info := range state.Sandboxes {
		// Running VMs from previous process are now stopped
		if info.State == StateRunning {
			now := time.Now().UTC()
			info.State = StateStopped
			info.StoppedAt = &now
			info.PID = 0
		}
		r.sandboxes[id] = &managedSandbox{info: info}
	}

	r.logger.Info("loaded persisted sandbox state",
		zap.Int("count", len(r.sandboxes)),
		zap.Uint32("next_cid", r.nextCID),
	)
	return nil
}

// Helpers

func effectiveProtocols(policy *NetworkPolicy) []string {
	if len(policy.AllowedProtocols) > 0 {
		return policy.AllowedProtocols
	}
	return []string{"tcp"}
}

func generateMAC(cid uint32) string {
	return fmt.Sprintf("02:FC:00:00:%02X:%02X", (cid>>8)&0xFF, cid&0xFF)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w (output: %s)", name, args, err, string(output))
	}
	return nil
}

func copyFile(src, dst string) error {
	// Use cp --reflink=auto for CoW on supported filesystems
	return runCmd("cp", "--reflink=auto", src, dst)
}

func createExt4Image(path string, sizeMB int) error {
	// Create a sparse file and format as ext4
	if err := runCmd("dd", "if=/dev/zero", "of="+path,
		"bs=1M", "count=0", "seek="+strconv.Itoa(sizeMB)); err != nil {
		return fmt.Errorf("failed to create sparse image: %w", err)
	}
	if err := runCmd("mkfs.ext4", "-F", "-q", path); err != nil {
		return fmt.Errorf("failed to format ext4: %w", err)
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
