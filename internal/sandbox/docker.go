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
	config    VMConfig
	containerID string
	startTime time.Time
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
		// Create isolated network per VM for stronger isolation
		networkName := fmt.Sprintf("aegis-net-%s", config.ID)
		_ = db.createNetwork(ctx, networkName)
		args = append(args, "--network", networkName)

		// Expose ports if specified
		for _, port := range config.NetworkConfig.ExposedPorts {
			args = append(args, "-p", port)
		}

		// Add environment for vsock port (emulated via port mapping)
		if config.NetworkConfig.VsockPort > 0 {
			args = append(args, "-e", fmt.Sprintf("VSOCK_PORT=%d", config.NetworkConfig.VsockPort))
		}
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
