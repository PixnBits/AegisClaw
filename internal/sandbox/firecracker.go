//go:build linux
// +build linux

package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// FirecrackerBackend implements Backend for Linux using Firecracker microVMs.
type FirecrackerBackend struct {
	stateDir string
	mu       sync.RWMutex
	vms      map[string]*firecrackerVM
}

type firecrackerVM struct {
	config    VMConfig
	cmd       *exec.Cmd
	startTime time.Time
	sockPath  string
}

// NewFirecrackerBackend creates a new Firecracker backend.
func NewFirecrackerBackend(stateDir string) *FirecrackerBackend {
	return &FirecrackerBackend{
		stateDir: stateDir,
		vms:      make(map[string]*firecrackerVM),
	}
}

// Start creates and starts a Firecracker microVM.
func (fb *FirecrackerBackend) Start(ctx context.Context, config VMConfig) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	if _, exists := fb.vms[config.ID]; exists {
		return fmt.Errorf("VM %s already running", config.ID)
	}

	sockPath := filepath.Join(fb.stateDir, "fc-"+config.ID+".sock")
	configPath := filepath.Join(fb.stateDir, "fc-"+config.ID+".json")

	// Build Firecracker configuration
	fcConfig := map[string]interface{}{
		"boot-source": map[string]interface{}{
			"kernel_image_path": config.KernelPath,
			"boot_args":         buildBootArgs(config),
		},
		"drives": []map[string]interface{}{
			{
				"drive_id":       "rootfs",
				"path_on_host":   config.RootfsPath,
				"is_root_device": true,
				"is_read_only":   false,
			},
		},
		"machine-config": map[string]interface{}{
			"vcpu_count":   config.VCpus,
			"mem_size_mib": config.Memory,
			"ht_enabled":   false,
		},
		"iommu": false,
	}

	// Add vsock device if port is specified
	if config.NetworkConfig != nil && config.NetworkConfig.VsockPort > 0 {
		fcConfig["vsock"] = map[string]interface{}{
			"vsock_id": config.NetworkConfig.VsockPort,
		}
	}

	// 7.1 Network Boundary integration (in progress)
	// When EgressViaBoundary is true, this VM must have **no** direct outbound
	// network interfaces. All egress must go through the Network Boundary
	// over vsock (the boundary listens on a vsock port and performs allowlist
	// enforcement, secret injection, and audit).
	//
	// The guest is expected to:
	//   - Have no tap/network interfaces for outbound.
	//   - Use the vsock address passed on the kernel cmdline (aegis.egress_boundary)
	//     to connect to the boundary's vsock listener and send egress traffic.
	if config.NetworkConfig != nil && config.NetworkConfig.EgressViaBoundary {
		logrus.Infof("VM %s configured with EgressViaBoundary=true (skill=%s, boundary=%s) — no direct network interfaces; guest must use vsock egress to boundary",
			config.ID,
			config.NetworkConfig.BoundarySkillID,
			config.NetworkConfig.BoundaryEgressAddr)
	} else {
		// For VMs that are allowed direct egress (e.g. the Boundary itself),
		// we would configure a tap interface here. Currently left to defaults.
	}

	configBytes, _ := json.MarshalIndent(fcConfig, "", "  ")
	if err := os.WriteFile(configPath, configBytes, 0644); err != nil {
		return fmt.Errorf("failed to write Firecracker config: %w", err)
	}

	logrus.Infof("Starting Firecracker VM %s with kernel %s, rootfs %s",
		config.ID, config.KernelPath, config.RootfsPath)

	cmd := exec.CommandContext(ctx, "firecracker",
		"--api-sock", sockPath,
		"--config-file", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Firecracker: %w", err)
	}

	logrus.Infof("Firecracker process started for VM %s, PID %d", config.ID, cmd.Process.Pid)

	// Wait for socket to be created
	if err := waitForSocket(sockPath, 10*time.Second); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("failed to wait for Firecracker socket: %w", err)
	}

	// Configure the VM via API
	if err := fb.configureVM(sockPath); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("failed to configure VM: %w", err)
	}

	fb.vms[config.ID] = &firecrackerVM{
		config:    config,
		cmd:       cmd,
		startTime: time.Now(),
		sockPath:  sockPath,
	}

	logrus.Infof("VM %s started successfully", config.ID)
	return nil
}

// Stop terminates a running Firecracker VM.
func (fb *FirecrackerBackend) Stop(ctx context.Context, vmID string) error {
	fb.mu.Lock()
	vm, exists := fb.vms[vmID]
	if !exists {
		fb.mu.Unlock()
		return fmt.Errorf("VM %s not found", vmID)
	}
	delete(fb.vms, vmID)
	fb.mu.Unlock()

	logrus.Infof("Stopping VM %s", vmID)

	// Try graceful shutdown via API first
	fb.sendVMAction(vm.sockPath, "InstanceHalt")
	time.Sleep(2 * time.Second)

	// Force kill if still running
	if vm.cmd.Process != nil {
		vm.cmd.Process.Kill()
	}

	// Clean up socket and config files
	_ = os.Remove(vm.sockPath)
	_ = os.Remove(filepath.Join(fb.stateDir, "fc-"+vmID+".json"))

	logrus.Infof("VM %s stopped", vmID)
	return nil
}

// Status returns the current status of a VM.
func (fb *FirecrackerBackend) Status(ctx context.Context, vmID string) (Status, error) {
	fb.mu.RLock()
	vm, exists := fb.vms[vmID]
	fb.mu.RUnlock()

	if !exists {
		return StatusStopped, nil
	}

	if vm.cmd.Process == nil {
		return StatusError, nil
	}

	if err := vm.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return StatusError, nil
	}

	return StatusRunning, nil
}

// List returns information about all running VMs.
func (fb *FirecrackerBackend) List(ctx context.Context) ([]VMInfo, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	var vms []VMInfo
	now := time.Now()

	for vmID, vm := range fb.vms {
		uptime := int64(now.Sub(vm.startTime).Seconds())
		vms = append(vms, VMInfo{
			ID:        vmID,
			Status:    StatusRunning,
			Uptime:    uptime,
			Memory:    vm.config.Memory,
			CreatedAt: vm.startTime.Unix(),
		})
	}

	return vms, nil
}

// Cleanup stops all running VMs (called on daemon shutdown).
func (fb *FirecrackerBackend) Cleanup(ctx context.Context) error {
	fb.mu.Lock()
	vmIDs := make([]string, 0, len(fb.vms))
	for id := range fb.vms {
		vmIDs = append(vmIDs, id)
	}
	fb.mu.Unlock()

	for _, vmID := range vmIDs {
		_ = fb.Stop(ctx, vmID)
	}

	return nil
}

// Helper functions

func waitForSocket(sockPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("socket not created within timeout")
}

// buildBootArgs constructs the kernel command line, injecting 7.1 egress information
// when the VM is configured to route all outbound through the Network Boundary.
func buildBootArgs(config VMConfig) string {
	base := "console=ttyS0 reboot=k panic=1 pci=off nomodules"

	if config.NetworkConfig != nil && config.NetworkConfig.EgressViaBoundary {
		// Pass boundary details to the guest via cmdline.
		// The guest (or future init system) is expected to use this for its
		// outbound proxy instead of a direct default route.
		egress := fmt.Sprintf(" aegis.egress_boundary=%s aegis.skill_id=%s",
			config.NetworkConfig.BoundaryEgressAddr,
			config.NetworkConfig.BoundarySkillID)
		base += egress
	}

	return base
}

func (fb *FirecrackerBackend) configureVM(sockPath string) error {
	// Send InstanceStart action
	return fb.sendVMAction(sockPath, "InstanceStart")
}

func (fb *FirecrackerBackend) sendVMAction(sockPath string, actionType string) error {
	addr := &net.UnixAddr{Name: sockPath, Net: "unix"}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	action := map[string]interface{}{
		"action_type": actionType,
	}
	payload, _ := json.Marshal(action)

	if _, err := conn.Write(payload); err != nil {
		return err
	}

	// Read response
	buf := make([]byte, 1024)
	if _, err := conn.Read(buf); err != nil && err != io.EOF {
		return err
	}

	return nil
}
