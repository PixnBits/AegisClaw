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
	config         VMConfig
	cmd            *exec.Cmd
	startTime      time.Time
	sockPath       string
	consoleLogPath string   // Phase 0: path to captured guest serial console
	consoleFile    *os.File // open handle that Firecracker writes guest console to (closed on Stop)
}

// FirecrackerVsockUDSPath returns the host-side Unix domain socket path that
// Firecracker creates for a VM's vsock device (the `uds_path` in the machine
// config). This is the single source of truth for the naming convention so the
// Host Daemon's reverse proxy and the backend's cleanup logic never drift.
//
// IMPORTANT: Firecracker does NOT expose the guest over the host kernel's
// AF_VSOCK. Host -> guest connections must go through this UDS using the
// Firecracker "hybrid vsock" handshake (connect to the UDS, then write
// "CONNECT <guest_port>\n"). A raw vsock.Dial(cid, port) from the host fails
// with ENODEV ("no such device"). See cmd/aegis/main.go dialFirecrackerVsock.
func FirecrackerVsockUDSPath(stateDir, vmID string) string {
	return filepath.Join(stateDir, "fc-"+vmID+"-vsock.sock")
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
	vsockUdsPath := FirecrackerVsockUDSPath(fb.stateDir, config.ID)

	// Clean up any stale artifacts from previous crashed/killed attempts.
	// This is required for reliable restarts: Firecracker refuses to bind if the
	// .sock already exists ("FailedToBindSocket ... Check that it is not already used").
	_ = os.Remove(sockPath)
	_ = os.Remove(configPath)
	_ = os.Remove(logPath)
	_ = os.Remove(consoleLogPath)
	_ = os.Remove(vsockUdsPath)
	// Do NOT remove config.PrivateKeyPath / *.vmkey here — orchestrator just wrote it and
	// we need it for cmdline hex + rootfs inject below. Stop() cleans up the key file.

	rootfsPath := config.RootfsPath
	if config.PrivateKeyPath != "" {
		rootfsPath = prepareVMRootfs(fb.stateDir, config.ID, config.RootfsPath, config.PrivateKeyPath)
	}

	// Build Firecracker configuration
	fcConfig := map[string]interface{}{
		"boot-source": map[string]interface{}{
			"kernel_image_path": config.KernelPath,
			"boot_args":         buildBootArgs(config),
		},
		"drives": []map[string]interface{}{
			{
				"drive_id":       "rootfs",
				"path_on_host":   rootfsPath,
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
		// NOTE: Firecracker has no "console" object in its machine config schema.
		// The guest serial console (ttyS0, see buildBootArgs "console=ttyS0") is
		// emitted on the Firecracker *process* stdout/stderr, which we redirect to
		// consoleLogPath below. A "console" config key here is silently ignored at
		// best and rejected at worst, so we deliberately do not set one.
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
		config.ID, config.KernelPath, rootfsPath)
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

	// Capture the guest serial console (ttyS0) + Firecracker's own process output.
	// Firecracker streams the guest console to its stdout/stderr for the entire VM
	// lifetime, so we redirect both to a real file (consoleLogPath). This is the
	// only reliable window into guest boot + application startup (e.g. the
	// web-portal binary's vsock listener messages) and is invaluable when a VM
	// boots but never becomes ready. The file is closed when the VM is stopped.
	consoleFile, consoleErr := os.OpenFile(consoleLogPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if consoleErr != nil {
		logrus.Warnf("could not open console log %s for VM %s (continuing without persisted guest console): %v", consoleLogPath, config.ID, consoleErr)
		// Fall back to an in-memory buffer purely so the cmd.Start error path below
		// still has something to report; this should be rare.
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
	} else {
		cmd.Stdout = consoleFile
		cmd.Stderr = consoleFile
	}

	if err := cmd.Start(); err != nil {
		dumpConsoleLog(consoleLogPath, config.ID)
		dumpFirecrackerLog(logPath, config.ID)
		if consoleFile != nil {
			_ = consoleFile.Close()
		}
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
		dumpFirecrackerLog(logPath, config.ID)
		dumpConsoleLog(consoleLogPath, config.ID)
		if consoleFile != nil {
			_ = consoleFile.Close()
		}
		cleanupVMArtifacts(sockPath, configPath, logPath, consoleLogPath, vsockUdsPath, config.PrivateKeyPath)
		return fmt.Errorf("failed to wait for Firecracker socket: %w", err)
	}

	// NOTE: We deliberately do NOT call configureVM / send InstanceStart here.
	// When using --config-file with a full boot-source + drives + machine-config,
	// Firecracker starts the microVM automatically (see successful "build microvm
	// from one single json" + "Successfully started microvm" in the VMM log).
	// Sending InstanceStart afterwards is rejected with HTTP 400.

	fb.vms[config.ID] = &firecrackerVM{
		config:         config,
		cmd:            cmd,
		startTime:      time.Now(),
		sockPath:       sockPath,
		consoleLogPath: consoleLogPath, // Phase 0 observability
		consoleFile:    consoleFile,    // kept open for the VM lifetime; closed on Stop
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

	// Close the guest console capture file (Firecracker has now exited / been killed).
	if vm.consoleFile != nil {
		_ = vm.consoleFile.Close()
	}

	// Clean up all per-VM artifacts (including the vsock UDS that Firecracker creates)
	stateDir := filepath.Dir(vm.sockPath)
	_ = os.Remove(vm.sockPath)
	_ = os.Remove(filepath.Join(stateDir, "fc-"+vmID+".json"))
	_ = os.Remove(filepath.Join(stateDir, "fc-"+vmID+".log"))
	_ = os.Remove(filepath.Join(stateDir, "fc-"+vmID+"-console.log"))
	_ = os.Remove(FirecrackerVsockUDSPath(stateDir, vmID))
	_ = os.Remove(filepath.Join(stateDir, vmID+".rootfs.img"))

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
			ID:             vmID,
			Status:         StatusRunning,
			Uptime:         uptime,
			Memory:         vm.config.Memory,
			CreatedAt:      vm.startTime.Unix(),
			ConsoleLogPath: vm.consoleLogPath, // Phase 0: make console logs discoverable
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

	// Tell the kernel which init to run. Our component rootfs images are produced
	// via `docker export`, which keeps the filesystem but DROPS the image
	// ENTRYPOINT, so the kernel would otherwise fall back to the base image's
	// /sbin/init (Alpine -> openrc, which isn't installed) and never launch the
	// component binary. Images that ship a custom /init wrapper set InitProcess.
	if config.InitProcess != "" {
		base += " init=" + config.InitProcess
	}

	if config.NetworkConfig != nil && config.NetworkConfig.EgressViaBoundary {
		// Pass boundary details to the guest via cmdline.
		// The guest (or future init system) is expected to use this for its
		// outbound proxy instead of a direct default route.
		egress := fmt.Sprintf(" aegis.egress_boundary=%s aegis.skill_id=%s",
			config.NetworkConfig.BoundaryEgressAddr,
			config.NetworkConfig.BoundarySkillID)
		base += egress
	}

	// 7.5.4: Point guests at the injected /etc/aegis/vmkey (see guest_key_inject.go).
	// Do not pass the raw key on the kernel cmdline — length limits and parsing can
	// desync register (pub) vs sign (priv) and break hub signatures after reconnect.
	if config.PrivateKeyPath != "" {
		base += " aegis.vm_private_key_path=/etc/aegis/vmkey"
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
