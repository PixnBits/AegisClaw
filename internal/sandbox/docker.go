//go:build darwin || windows
// +build darwin windows

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// DockerBackend implements Backend using Docker Sandbox (sbx) on macOS and Windows.
type DockerBackend struct {
	stateDir string
	mu       sync.RWMutex
	vms      map[string]*dockerSandbox
}

type dockerSandbox struct {
	config      VMConfig
	containerID string
	startTime   time.Time
}

// NewDockerBackend creates a new Docker Sandbox backend.
func NewDockerBackend(stateDir string) *DockerBackend {
	return &DockerBackend{
		stateDir: stateDir,
		vms:      make(map[string]*dockerSandbox),
	}
}

// Start creates and starts a Docker Sandbox.
func (db *DockerBackend) Start(ctx context.Context, config VMConfig) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.vms[config.ID]; exists {
		return fmt.Errorf("sandbox %s already running", config.ID)
	}

	// Build Docker run command with sandbox-like isolation
	// On macOS/Windows, we use Docker with enhanced isolation
	args := []string{"run", "-d"}

	// Add name
	args = append(args, "--name", config.ID)

	// Resource limits
	args = append(args,
		fmt.Sprintf("--memory=%dm", config.Memory),
		fmt.Sprintf("--cpus=%d", config.VCpus),
	)

	// Security settings for sandbox isolation
	args = append(args,
		"--security-opt=no-new-privileges:true",
		"--read-only",
		"--tmpfs=/tmp",
		"--tmpfs=/run",
		"--cap-drop=ALL",
		"--cap-add=NET_BIND_SERVICE",
	)

	// Network configuration
	if config.NetworkConfig != nil {
		// 7.1 Network Boundary integration (complete for dev parity):
		// For EgressViaBoundary VMs under Docker we use --network=none (no outbound
		// interfaces at all). Real egress must go through the vsock/TCP boundary
		// proxy (exactly as Firecracker guests with no NICs do). This is dev-only;
		// production uses the Firecracker backend with hypervisor-level isolation.
		if config.NetworkConfig.EgressViaBoundary {
			logrus.Infof("Docker VM %s configured with EgressViaBoundary=true (skill=%s) — using --network=none (no direct outbound; must use boundary vsock/TCP path)",
				config.ID, config.NetworkConfig.BoundarySkillID)
			args = append(args, "--network", "none")
		} else {
			// Create isolated network per VM for stronger isolation (only for VMs that may have direct egress)
			networkName := fmt.Sprintf("aegis-net-%s", config.ID)
			_ = db.createNetwork(ctx, networkName)
			args = append(args, "--network", networkName)
		}

		// Expose ports if specified
		for _, port := range config.NetworkConfig.ExposedPorts {
			args = append(args, "-p", port)
		}

		// Add environment for vsock port (emulated via port mapping)
		if config.NetworkConfig.VsockPort > 0 {
			args = append(args, "-e", fmt.Sprintf("VSOCK_PORT=%d", config.NetworkConfig.VsockPort))
		}
	}

	// 7.5.4: Pass the ephemeral private key path to the guest (daemon-side
	// secure distribution channel). For Docker dev we use an env var (weaker
	// than the Firecracker cmdline + file model; real secrets should use
	// tmpfs secret mounts in production). The guest init should consume once
	// and shred.
	// See VMConfig.PrivateKeyPath and host-daemon.md key distribution rules.
	if config.PrivateKeyPath != "" {
		args = append(args, "-e", fmt.Sprintf("AEGIS_VM_PRIVATE_KEY_PATH=%s", config.PrivateKeyPath))
	}

	// Web Portal (presentation only) must receive AEGIS_WEB_PORTAL_LISTEN_ADDR so it
	// does not default to :8080 and hit the public-bind guard in cmd/web-portal/main.go.
	// ExposedPorts (set by orchestrator for web-portal) already caused the -p mapping.
	// This makes the Docker Sandbox path (macOS/Windows) behave identically to the
	// Firecracker path for the host reverse proxy on :8080.
	if config.ID == "web-portal" {
		args = append(args, "-e", "AEGIS_WEB_PORTAL_LISTEN_ADDR=127.0.0.1:18080")
	}

	// Mount the filesystem/image
	if config.RootfsPath != "" {
		args = append(args, "-v", fmt.Sprintf("%s:/rootfs:ro", config.RootfsPath))
	}

	// Use the configured image or a default
	image := config.Image
	if image == "" {
		image = "aegis-sandbox:latest"
	}

	args = append(args, image)

	logrus.Infof("Starting Docker Sandbox %s with image %s", config.ID, image)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.Errorf("Failed to start Docker Sandbox: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to start Docker Sandbox: %w", err)
	}

	containerID := strings.TrimSpace(string(output))
	logrus.Infof("Docker Sandbox %s started with container ID %s", config.ID, containerID)

	db.vms[config.ID] = &dockerSandbox{
		config:      config,
		containerID: containerID,
		startTime:   time.Now(),
	}

	return nil
}

// Stop terminates a running Docker Sandbox.
func (db *DockerBackend) Stop(ctx context.Context, vmID string) error {
	db.mu.Lock()
	sandbox, exists := db.vms[vmID]
	if !exists {
		db.mu.Unlock()
		return fmt.Errorf("sandbox %s not found", vmID)
	}
	delete(db.vms, vmID)
	db.mu.Unlock()

	logrus.Infof("Stopping sandbox %s", vmID)

	// Stop container
	cmd := exec.CommandContext(ctx, "docker", "stop", sandbox.containerID)
	if err := cmd.Run(); err != nil {
		logrus.Warnf("Failed to stop container %s: %v", sandbox.containerID, err)
	}

	// Remove container
	cmd = exec.CommandContext(ctx, "docker", "rm", sandbox.containerID)
	if err := cmd.Run(); err != nil {
		logrus.Warnf("Failed to remove container %s: %v", sandbox.containerID, err)
	}

	// Clean up network
	networkName := fmt.Sprintf("aegis-net-%s", vmID)
	_ = db.removeNetwork(ctx, networkName)

	// 7.5.4: Best-effort cleanup of the ephemeral VM private key file
	// (defense in depth — the guest should have already shredded it).
	if sandbox.config.PrivateKeyPath != "" {
		_ = os.Remove(sandbox.config.PrivateKeyPath)
	}

	logrus.Infof("Sandbox %s stopped", vmID)
	return nil
}

// Status returns the current status of a Docker Sandbox.
func (db *DockerBackend) Status(ctx context.Context, vmID string) (Status, error) {
	db.mu.RLock()
	sandbox, exists := db.vms[vmID]
	db.mu.RUnlock()

	if !exists {
		return StatusStopped, nil
	}

	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", sandbox.containerID)
	output, err := cmd.Output()
	if err != nil {
		return StatusError, nil
	}

	if strings.TrimSpace(string(output)) == "true" {
		return StatusRunning, nil
	}

	return StatusStopped, nil
}

// List returns information about all running Sandboxes.
func (db *DockerBackend) List(ctx context.Context) ([]VMInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var sandboxes []VMInfo
	now := time.Now()

	for vmID, sandbox := range db.vms {
		uptime := int64(now.Sub(sandbox.startTime).Seconds())
		sandboxes = append(sandboxes, VMInfo{
			ID:        vmID,
			Status:    StatusRunning,
			Uptime:    uptime,
			Memory:    sandbox.config.Memory,
			CreatedAt: sandbox.startTime.Unix(),
		})
	}

	return sandboxes, nil
}

// Cleanup stops all running Sandboxes (called on daemon shutdown).
func (db *DockerBackend) Cleanup(ctx context.Context) error {
	db.mu.Lock()
	vmIDs := make([]string, 0, len(db.vms))
	for id := range db.vms {
		vmIDs = append(vmIDs, id)
	}
	db.mu.Unlock()

	for _, vmID := range vmIDs {
		_ = db.Stop(ctx, vmID)
	}

	return nil
}

// Helper functions

func (db *DockerBackend) createNetwork(ctx context.Context, networkName string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "create", networkName)
	return cmd.Run()
}

func (db *DockerBackend) removeNetwork(ctx context.Context, networkName string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "rm", networkName)
	return cmd.Run()
}

// BootPhases implements Backend (no-op for Docker sandbox; focus is Firecracker
// microVM observability per the instrumentation task). Orchestrator-level host
// phases are still captured for all backends.
func (db *DockerBackend) BootPhases(ctx context.Context, vmID string) map[string]int64 {
	return nil
}
