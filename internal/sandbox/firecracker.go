//go:build linux
// +build linux

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	logPath := filepath.Join(fb.stateDir, "fc-"+config.ID+".log")
	consoleLogPath := filepath.Join(fb.stateDir, "fc-"+config.ID+"-console.log")
	vsockUdsPath := filepath.Join(fb.stateDir, "fc-"+config.ID+"-vsock.sock")
	keyPath := filepath.Join(fb.stateDir, config.ID+".vmkey") // ephemeral private key from orchestrator

	// Clean up any stale artifacts from previous crashed/killed attempts.
	// This is required for reliable restarts: Firecracker refuses to bind if the
	// .sock already exists ("FailedToBindSocket ... Check that it is not already used").
	_ = os.Remove(sockPath)
	_ = os.Remove(configPath)
	_ = os.Remove(logPath)
	_ = os.Remove(consoleLogPath)
	_ = os.Remove(vsockUdsPath)
	_ = os.Remove(keyPath)

	// Build Firecracker configuration

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
			"smt":          false,
		},
		"iommu": false,
		// Capture guest kernel + init console output to a file next to the other fc-* artifacts.
		// This is invaluable when the VM reaches "process started" but then fails to become
		// ready (the exact symptom seen with connection refused on the API socket).
		"console": map[string]interface{}{
			"console_type": "File",
			"file_path":    consoleLogPath,
		},
	}

	// Add vsock device if allocated.
	// Modern Firecracker (v1.8+, including main builds) --config-file schema requires:
	//   - "vsock_id": string (device identifier)
	//   - "guest_cid": integer (the CID the guest will use; small values like 3-10)
	// The previous code put a large integer directly into "vsock_id", which produces
	// "invalid type: integer `9000`, expected a string".
	if config.NetworkConfig != nil && config.NetworkConfig.VsockPort > 0 {
		// Firecracker v1.16+ (main) --config-file requires `uds_path` for vsock devices.
		// This tells Firecracker where to create the host-side Unix domain socket for
		// the vsock device. We generate a per-VM path alongside the other fc-* artifacts.
		// (The actual vsock communication model in AegisClaw uses guest-initiated
		// connections to CID 2 or to the Network Boundary's listener; the UDS is
		// required for the VMM config to be accepted.)
		fcConfig["vsock"] = map[string]interface{}{
			"vsock_id":  fmt.Sprintf("vsock-%s", config.ID),
			"guest_cid": config.NetworkConfig.VsockPort,
			"uds_path":  vsockUdsPath,
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
	if os.Getenv("AEGIS_DEBUG") != "" {
		logrus.Debugf("Firecracker command: firecracker --api-sock %s --config-file %s --log-path %s --level Debug",
			sockPath, configPath, logPath)
		logrus.Debugf("Firecracker config for %s:\n%s", config.ID, string(configBytes))
	}

	cmd := exec.CommandContext(ctx, "firecracker",
		"--api-sock", sockPath,
		"--config-file", configPath,
		"--log-path", logPath,
		"--level", "Debug")

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}

	// Capture Firecracker's own logs (very important for diagnosing boot failures)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		logrus.Errorf("Firecracker stdout:\n%s", stdout.String())
		logrus.Errorf("Firecracker stderr:\n%s", stderr.String())
		dumpFirecrackerLog(logPath, config.ID)
		cleanupVMArtifacts(sockPath, configPath, logPath, consoleLogPath, vsockUdsPath, config.PrivateKeyPath)
		return fmt.Errorf("failed to start Firecracker: %w", err)
	}

	logrus.Infof("Firecracker process started for VM %s, PID %d", config.ID, cmd.Process.Pid)

	// Wait for socket to be created (confirms the Firecracker API is responsive).
	// With a complete --config-file the microVM is started automatically by
	// Firecracker during process startup. We must NOT send an extra "InstanceStart"
	// action afterwards (it produces HTTP 400 "not supported after starting the microVM").
	if err := waitForSocket(sockPath, 10*time.Second); err != nil {
		cmd.Process.Kill()
		logrus.Errorf("Firecracker stdout for %s:\n%s", config.ID, stdout.String())
		logrus.Errorf("Firecracker stderr for %s:\n%s", config.ID, stderr.String())
		dumpFirecrackerLog(logPath, config.ID)
		dumpConsoleLog(consoleLogPath, config.ID)
		cleanupVMArtifacts(sockPath, configPath, logPath, consoleLogPath, vsockUdsPath, config.PrivateKeyPath)
		return fmt.Errorf("failed to wait for Firecracker socket: %w", err)
	}

	// NOTE: We deliberately do NOT call configureVM / send InstanceStart here.
	// When using --config-file with a full boot-source + drives + machine-config,
	// Firecracker starts the microVM automatically (see successful "build microvm
	// from one single json" + "Successfully started microvm" in the VMM log).
	// Sending InstanceStart afterwards is rejected with HTTP 400.

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

	// Clean up all per-VM artifacts (including the vsock UDS that Firecracker creates)
	stateDir := filepath.Dir(vm.sockPath)
	_ = os.Remove(vm.sockPath)
	_ = os.Remove(filepath.Join(stateDir, "fc-"+vmID+".json"))
	_ = os.Remove(filepath.Join(stateDir, "fc-"+vmID+".log"))
	_ = os.Remove(filepath.Join(stateDir, "fc-"+vmID+"-console.log"))
	_ = os.Remove(filepath.Join(stateDir, "fc-"+vmID+"-vsock.sock"))

	// 7.5.4: Best-effort cleanup of the ephemeral VM private key file
	// (defense in depth — the guest should have already shredded it).
	if vm.config.PrivateKeyPath != "" {
		_ = os.Remove(vm.config.PrivateKeyPath)
	}

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
		// Real readiness: the socket file must exist *and* be connectable.
		// Stat-only checks are racy because Firecracker creates the file before
		// the listener is fully accepting (or before the guest is far enough
		// for the VMM API to be responsive).
		if _, err := os.Stat(sockPath); err == nil {
			conn, dialErr := net.DialTimeout("unix", sockPath, 200*time.Millisecond)
			if dialErr == nil {
				conn.Close()
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("socket not accepting connections within timeout")
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

	// 7.5.4: Pass the ephemeral private key path to the guest (daemon-side
	// secure distribution channel). The guest init is expected to read the
	// file once, load the key, and then shred + unlink it.
	// See VMConfig.PrivateKeyPath and host-daemon.md key distribution rules.
	if config.PrivateKeyPath != "" {
		base += fmt.Sprintf(" aegis.vm_private_key_path=%s", config.PrivateKeyPath)
	}

	// Group 3 (Court): Support persona identity injection for the 7 court-persona VMs
	// via kernel cmdline. The thin court-persona binary already parses aegis.persona=
	// (and AEGIS_COURT_PERSONA env) so a single court-persona.img works for all personas.
	if config.ExtraBootArgs != "" {
		base += " " + strings.TrimSpace(config.ExtraBootArgs)
	}

	// Group 3 Court support (governance-court.md §Architecture):
	// If this is a court-persona-* VM, auto-inject the persona identity via kernel cmdline.
	// The thin court-persona binary (cmd/court-persona/main.go) parses aegis.persona=
	// from /proc/cmdline at startup. This lets us use a single court-persona.img for all 7 personas.
	if strings.HasPrefix(config.ID, "court-persona-") {
		persona := strings.TrimPrefix(config.ID, "court-persona-")
		if persona != "" {
			base += fmt.Sprintf(" aegis.persona=%s", persona)
		}
	}

	return base
}

func (fb *FirecrackerBackend) configureVM(sockPath string) error {
	// Send InstanceStart action via the proper Firecracker HTTP-over-unix-socket API.
	// The previous raw JSON write was not a valid HTTP request and commonly resulted
	// in "connection refused" or immediate close even when the socket file existed.
	return fb.sendVMAction(sockPath, "InstanceStart")
}

// sendVMAction sends a Firecracker action (InstanceStart, InstanceHalt, etc.)
// using a proper HTTP PUT to /actions over the unix socket. This matches the
// documented Firecracker API (used by the official SDK and all recent versions).
func (fb *FirecrackerBackend) sendVMAction(sockPath string, actionType string) error {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("unix", sockPath)
		},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   8 * time.Second,
	}

	body := bytes.NewBufferString(`{"action_type": "` + actionType + `"}`)
	req, err := http.NewRequest("PUT", "http://localhost/actions", body)
	if err != nil {
		return fmt.Errorf("failed to create action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Accept 200/204 (success) or 400 with specific messages in some versions.
	// Any non-2xx is treated as error for now (caller will log + kill).
	if resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("firecracker action %s failed: HTTP %d: %s", actionType, resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}
	return nil
}

// dumpFirecrackerLog reads and logs the tail of the VMM log file (the one passed to
// --log-path). This is the most useful artifact when a VM reaches "process started"
// but then fails the readiness or action steps (guest boot failure, kernel panic,
// bad rootfs, missing /init, etc.).
func dumpFirecrackerLog(logPath, vmID string) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		logrus.Debugf("no Firecracker VMM log yet for %s at %s: %v", vmID, logPath, err)
		return
	}
	lines := strings.Split(string(data), "\n")
	start := len(lines) - 60
	if start < 0 {
		start = 0
	}
	tail := strings.Join(lines[start:], "\n")
	logrus.Errorf("Firecracker VMM log tail for %s (from %s):\n%s", vmID, logPath, tail)
}

// dumpConsoleLog does the same for the guest serial console log we configured in the
// Firecracker JSON. This shows the actual kernel boot messages + whatever /init prints.
func dumpConsoleLog(consolePath, vmID string) {
	data, err := os.ReadFile(consolePath)
	if err != nil {
		logrus.Debugf("no guest console log yet for %s at %s: %v", vmID, consolePath, err)
		return
	}
	logrus.Errorf("Guest console output for %s (from %s):\n%s", vmID, consolePath, string(data))
}

// cleanupVMArtifacts removes the on-disk files for a VM launch attempt.
// Called on failure paths so that the next start of the same VM ID does not
// see stale sockets/configs (Firecracker refuses to bind if the .sock exists).
func cleanupVMArtifacts(sockPath, configPath, logPath, consolePath, vsockUdsPath, keyPath string) {
	_ = os.Remove(sockPath)
	_ = os.Remove(configPath)
	_ = os.Remove(logPath)
	_ = os.Remove(consolePath)
	_ = os.Remove(vsockUdsPath)
	if keyPath != "" {
		_ = os.Remove(keyPath)
	}
}
