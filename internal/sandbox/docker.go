// Package sandbox – docker.go
//
// DockerRuntime manages workload sandboxes backed by Docker containers.
// It satisfies the same SandboxManager interface as FirecrackerRuntime and
// is wired into the system via the Orchestrator abstraction in orchestrator.go.
//
// IPC model: the host and the container communicate over a Unix domain socket.
// The host creates a per-sandbox directory ($StateDir/<id>/) and bind-mounts it
// into the container at /run/aegis/.  The guest-agent started with
// --transport=unix creates agent.sock inside that directory.  The host connects
// to $StateDir/<id>/agent.sock to send JSON requests.
//
// The JSON Request/Response protocol is identical to the vsock path so all
// callers (secrets injection, tool dispatch, etc.) work without modification.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

const (
	// dockerContainerPrefix is prepended to every sandbox ID to form the
	// Docker container name (e.g. "aegis-my-skill-abc123").
	dockerContainerPrefix = "aegis-"

	// agentSocketName is the filename of the guest-agent Unix socket inside
	// the per-sandbox state directory.
	agentSocketName = "agent.sock"

	// agentRunDir is the directory inside the container where the host-side
	// state directory is bind-mounted.  The guest-agent creates agent.sock here.
	agentRunDir = "/run/aegis"

	// dockerDefaultBin is the default docker binary name resolved via $PATH.
	dockerDefaultBin = "docker"

	// idleCheckInterval is how often the idle-pause goroutine wakes up.
	idleCheckInterval = time.Minute

	// sandboxNetworkPrefix is prepended to the sanitised sandbox ID to form the
	// per-sandbox Docker bridge network name (e.g. "aegis-net-skill_abc123").
	sandboxNetworkPrefix = "aegis-net-"

	// egressBridgeName is the name of the shared Docker bridge that connects
	// proxy-mode containers to the host-side egress proxy listener.
	egressBridgeName = "aegis-egress"
)

// DockerRuntimeConfig holds configuration for the Docker sandbox runtime.
type DockerRuntimeConfig struct {
	// DockerBin is the path to the docker binary.  Defaults to "docker" (PATH lookup).
	DockerBin string `json:"docker_bin"`

	// StateDir is the host directory where per-sandbox state (Unix socket,
	// bind-mount source) is created.  Must be an absolute path.
	StateDir string `json:"state_dir"`

	// IdleTimeout is how long a running sandbox may be idle before it is
	// automatically paused with "docker pause".  Zero disables idle pausing.
	IdleTimeout time.Duration `json:"idle_timeout"`
}

// Validate checks that the config has all required fields with valid values.
func (c *DockerRuntimeConfig) Validate() error {
	if c.StateDir == "" {
		return fmt.Errorf("state_dir is required")
	}
	if !filepath.IsAbs(c.StateDir) {
		return fmt.Errorf("state_dir must be an absolute path, got %q", c.StateDir)
	}
	return nil
}

// dockerManagedSandbox is the in-memory record for a Docker-backed sandbox.
type dockerManagedSandbox struct {
	info      SandboxInfo
	idleSince *time.Time // non-nil when the sandbox has been idle since this time
}

// DockerRuntime manages Docker container sandboxes.
// Every lifecycle operation is signed and logged through the kernel before
// side-effects occur, matching the FirecrackerRuntime contract.
type DockerRuntime struct {
	cfg       DockerRuntimeConfig
	kern      *kernel.Kernel
	logger    *zap.Logger
	sandboxes map[string]*dockerManagedSandbox
	mu        sync.RWMutex
}

// NewDockerRuntime creates a new DockerRuntime and starts the idle-pause
// background goroutine when IdleTimeout > 0.
func NewDockerRuntime(cfg DockerRuntimeConfig, kern *kernel.Kernel, logger *zap.Logger) (*DockerRuntime, error) {
	if cfg.DockerBin == "" {
		cfg.DockerBin = dockerDefaultBin
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid docker runtime config: %w", err)
	}
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create state dir %s: %w", cfg.StateDir, err)
	}
	rt := &DockerRuntime{
		cfg:       cfg,
		kern:      kern,
		logger:    logger,
		sandboxes: make(map[string]*dockerManagedSandbox),
	}
	if cfg.IdleTimeout > 0 {
		go rt.idlePauseLoop()
	}
	return rt, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (r *DockerRuntime) bin() string {
	if r.cfg.DockerBin != "" {
		return r.cfg.DockerBin
	}
	return dockerDefaultBin
}

// sandboxNetworkName returns the Docker bridge network name for a sandbox.
func sandboxNetworkName(id string) string {
	return sandboxNetworkPrefix + sanitizeID(id)
}

// containerName returns the Docker container name for a sandbox ID.
func containerName(id string) string { return dockerContainerPrefix + id }

// createSandboxNetwork creates a per-sandbox Docker bridge network.
// For NoNetwork sandboxes this is a no-op.
// For proxy-mode sandboxes the shared aegis-egress bridge is also ensured.
func (r *DockerRuntime) createSandboxNetwork(ctx context.Context, id string, policy NetworkPolicy) error {
	if policy.NoNetwork {
		return nil
	}
	netName := sandboxNetworkName(id)
	if out, err := r.runDockerCmd(ctx, "network", "create",
		"--driver", "bridge",
		"--internal",
		netName,
	); err != nil {
		return fmt.Errorf("docker network create %s: %w (output: %s)", netName, err, strings.TrimSpace(out))
	}
	if policy.EgressMode == "proxy" {
		if err := r.ensureEgressBridge(ctx); err != nil {
			// Non-fatal: egress proxy may not be needed immediately.
			r.logger.Warn("could not ensure aegis-egress bridge; proxy egress may be unavailable",
				zap.Error(err))
		}
	}
	return nil
}

// deleteSandboxNetwork removes the per-sandbox Docker bridge network.
// Called on Stop and Delete; errors are logged but do not block teardown.
func (r *DockerRuntime) deleteSandboxNetwork(ctx context.Context, id string, policy NetworkPolicy) {
	if policy.NoNetwork {
		return
	}
	netName := sandboxNetworkName(id)
	if out, err := r.runDockerCmd(ctx, "network", "rm", netName); err != nil {
		if !strings.Contains(out, "No such network") && !strings.Contains(out, "not found") {
			r.logger.Warn("docker network rm failed",
				zap.String("network", netName),
				zap.Error(err),
				zap.String("output", strings.TrimSpace(out)))
		}
	}
}

// ensureEgressBridge creates the shared aegis-egress bridge network if it does
// not already exist.  This network connects proxy-mode containers to the
// host-side egress proxy TCP listener on the bridge gateway address.
func (r *DockerRuntime) ensureEgressBridge(ctx context.Context) error {
	if out, err := r.runDockerCmd(ctx, "network", "inspect", egressBridgeName); err == nil && !strings.Contains(out, "No such network") {
		return nil // already exists
	}
	if out, err := r.runDockerCmd(ctx, "network", "create",
		"--driver", "bridge",
		"--internal",
		egressBridgeName,
	); err != nil {
		return fmt.Errorf("create egress bridge %s: %w (output: %s)", egressBridgeName, err, strings.TrimSpace(out))
	}
	r.logger.Info("created shared egress bridge network", zap.String("name", egressBridgeName))
	return nil
}



// sandboxDir returns the host-side state directory for a sandbox.
func (r *DockerRuntime) sandboxDir(id string) string {
	return filepath.Join(r.cfg.StateDir, id)
}

// socketPath returns the host-side Unix socket path for the sandbox.
func (r *DockerRuntime) socketPath(id string) string {
	return filepath.Join(r.sandboxDir(id), agentSocketName)
}

// runDockerCmd executes a docker sub-command, returning combined stdout+stderr
// and any error.  The output is always returned so callers can surface it in
// error messages even when cmd.Run fails.
func (r *DockerRuntime) runDockerCmd(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.bin(), args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// UnixSocketPath returns the host-side Unix socket for IPC with a running sandbox.
// It is the Docker equivalent of FirecrackerRuntime.VsockPath.
func (r *DockerRuntime) UnixSocketPath(id string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ms, ok := r.sandboxes[id]
	if !ok {
		return "", fmt.Errorf("sandbox %s not found", id)
	}
	if ms.info.State != StateRunning {
		return "", fmt.Errorf("sandbox %s is not running (state: %s)", id, ms.info.State)
	}
	return r.socketPath(id), nil
}

// ─── SandboxManager implementation ───────────────────────────────────────────

// validateDockerSpec checks that a SandboxSpec satisfies the requirements of
// DockerRuntime.  It does not call SandboxSpec.Validate() because that method
// enforces Firecracker-specific constraints (absolute RootfsPath, VsockCID ≥ 3).
func validateDockerSpec(spec *SandboxSpec) error {
	if spec.ID == "" {
		return fmt.Errorf("sandbox ID is required")
	}
	if spec.Name == "" {
		return fmt.Errorf("sandbox name is required")
	}
	if !nameRegex.MatchString(spec.Name) {
		return fmt.Errorf("sandbox name must match pattern %s, got %q", nameRegex.String(), spec.Name)
	}
	if spec.DockerImage == "" {
		return fmt.Errorf("docker_image is required for Docker sandboxes")
	}
	if spec.Resources.VCPUs < 1 || spec.Resources.VCPUs > 32 {
		return fmt.Errorf("VCPUs must be between 1 and 32, got %d", spec.Resources.VCPUs)
	}
	if spec.Resources.MemoryMB < 128 || spec.Resources.MemoryMB > 32768 {
		return fmt.Errorf("memory must be between 128 and 32768 MB, got %d", spec.Resources.MemoryMB)
	}
	if !spec.NetworkPolicy.DefaultDeny {
		return fmt.Errorf("network policy default_deny must be true")
	}
	if err := validateNetworkPolicy(&spec.NetworkPolicy); err != nil {
		return fmt.Errorf("invalid network policy: %w", err)
	}
	for i, ref := range spec.SecretsRefs {
		if !secretRefRegex.MatchString(ref) {
			return fmt.Errorf("secrets_refs[%d] %q must match %s", i, ref, secretRefRegex.String())
		}
	}
	return nil
}

// buildDockerCreateArgs builds the argument slice for `docker create`.
func (r *DockerRuntime) buildDockerCreateArgs(spec SandboxSpec) []string {
	workspaceMB := spec.WorkspaceMB
	if workspaceMB <= 0 {
		workspaceMB = defaultWorkspace
	}

	// The host-side state directory is bind-mounted as the run dir inside the
	// container so the guest-agent can create agent.sock there.
	hostStateDir := r.sandboxDir(spec.ID)

	args := []string{
		"create",
		"--name", containerName(spec.ID),
		"--hostname", spec.Name,
		fmt.Sprintf("--cpus=%.2f", float64(spec.Resources.VCPUs)),
		fmt.Sprintf("--memory=%dm", spec.Resources.MemoryMB),
		"--cap-drop", "ALL",
		"--read-only",
		"--tmpfs", "/run:exec,mode=755,size=64m",
		"--tmpfs", "/tmp:noexec,size=32m",
		fmt.Sprintf("--tmpfs=/workspace:exec,mode=755,size=%dm", workspaceMB),
		// Bind-mount the host state directory so the guest-agent can create
		// agent.sock inside it.  The entire directory is mounted rather than
		// a pre-created socket file so the guest process creates the socket
		// with the right ownership.
		"-v", fmt.Sprintf("%s:%s", hostStateDir, agentRunDir),
		"--label", fmt.Sprintf("aegisclaw.sandbox.id=%s", spec.ID),
		"--label", fmt.Sprintf("aegisclaw.sandbox.name=%s", spec.Name),
	}

	if spec.NetworkPolicy.NoNetwork {
		args = append(args, "--network", "none")
	} else {
		// Attach to the per-sandbox internal bridge so nftables/iptables rules
		// can reference a stable network boundary.  For proxy-mode sandboxes a
		// second docker network connect (aegis-egress) is issued in Create after
		// the container is built.
		args = append(args, "--network", sandboxNetworkName(spec.ID))
	}

	args = append(args, spec.DockerImage)
	// Invoke the guest-agent in Unix-socket mode.
	args = append(args, "/sbin/guest-agent", "--transport=unix")

	return args
}

// Create provisions a new Docker container from the spec without starting it.
// The container is created in the "created" state; call Start to boot it.
func (r *DockerRuntime) Create(ctx context.Context, spec SandboxSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := validateDockerSpec(&spec); err != nil {
		return fmt.Errorf("invalid sandbox spec: %w", err)
	}
	if _, exists := r.sandboxes[spec.ID]; exists {
		return fmt.Errorf("sandbox %s already exists", spec.ID)
	}

	// Create the host-side state directory (bind-mount source).
	stateDir := r.sandboxDir(spec.ID)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("failed to create sandbox state dir %s: %w", stateDir, err)
	}

	// Create the per-sandbox Docker bridge network before the container.
	if err := r.createSandboxNetwork(ctx, spec.ID, spec.NetworkPolicy); err != nil {
		_ = os.RemoveAll(stateDir)
		return fmt.Errorf("failed to create sandbox network: %w", err)
	}

	args := r.buildDockerCreateArgs(spec)
	if out, err := r.runDockerCmd(ctx, args...); err != nil {
		r.deleteSandboxNetwork(ctx, spec.ID, spec.NetworkPolicy)
		_ = os.RemoveAll(stateDir)
		return fmt.Errorf("docker create failed: %w (output: %s)", err, strings.TrimSpace(out))
	}

	// For proxy-mode sandboxes: also attach to the shared egress bridge.
	// Docker only supports one --network in `docker create`; additional
	// networks are attached via `docker network connect` immediately after.
	if !spec.NetworkPolicy.NoNetwork && spec.NetworkPolicy.EgressMode == "proxy" {
		if out, err := r.runDockerCmd(ctx, "network", "connect",
			egressBridgeName, containerName(spec.ID),
		); err != nil {
			// Non-fatal: log the issue and continue.  Egress will simply be
			// unavailable via the shared bridge; the sandbox can still be
			// started and used for non-egress work.
			r.logger.Warn("could not attach container to egress bridge",
				zap.String("id", spec.ID),
				zap.String("bridge", egressBridgeName),
				zap.Error(err),
				zap.String("output", strings.TrimSpace(out)))
		}
	}

	info := SandboxInfo{
		Spec:       spec,
		State:      StateCreated,
		SocketPath: r.socketPath(spec.ID),
	}
	r.sandboxes[spec.ID] = &dockerManagedSandbox{info: info}

	payload, _ := json.Marshal(spec)
	action := kernel.NewAction(kernel.ActionSandboxCreate, "kernel", payload)
	if _, err := r.kern.SignAndLog(action); err != nil {
		return fmt.Errorf("failed to log sandbox creation: %w", err)
	}

	r.logger.Info("docker sandbox created",
		zap.String("id", spec.ID),
		zap.String("name", spec.Name),
		zap.String("image", spec.DockerImage),
	)
	return nil
}

// Start boots a created or stopped Docker container sandbox.
// If the sandbox is paused it is unpaused instead of restarted.
func (r *DockerRuntime) Start(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ms, ok := r.sandboxes[id]
	if !ok {
		return fmt.Errorf("sandbox %s not found", id)
	}
	if ms.info.State == StateRunning {
		return fmt.Errorf("sandbox %s is already running", id)
	}
	if ms.info.State != StateCreated && ms.info.State != StateStopped && ms.info.State != StatePaused {
		return fmt.Errorf("sandbox %s is in state %s, cannot start", id, ms.info.State)
	}

	if ms.info.State == StatePaused {
		if out, err := r.runDockerCmd(ctx, "unpause", containerName(id)); err != nil {
			return fmt.Errorf("docker unpause failed: %w (output: %s)", err, strings.TrimSpace(out))
		}
	} else {
		if out, err := r.runDockerCmd(ctx, "start", containerName(id)); err != nil {
			ms.info.State = StateError
			ms.info.Error = fmt.Sprintf("docker start failed: %v", err)
			return fmt.Errorf("docker start failed: %w (output: %s)", err, strings.TrimSpace(out))
		}
	}

	now := time.Now().UTC()
	ms.info.State = StateRunning
	ms.info.StartedAt = &now
	ms.info.Error = ""
	ms.idleSince = nil

	payload, _ := json.Marshal(ms.info.Spec)
	action := kernel.NewAction(kernel.ActionSandboxStart, "kernel", payload)
	if _, err := r.kern.SignAndLog(action); err != nil {
		r.logger.Warn("failed to log sandbox start", zap.String("id", id), zap.Error(err))
	}

	r.logger.Info("docker sandbox started",
		zap.String("id", id),
		zap.String("name", ms.info.Spec.Name),
	)
	return nil
}

// Stop gracefully shuts down a running or paused Docker container sandbox.
// Sends SIGTERM via "docker stop --time=5"; falls back to "docker kill" on
// failure.
func (r *DockerRuntime) Stop(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ms, ok := r.sandboxes[id]
	if !ok {
		return fmt.Errorf("sandbox %s not found", id)
	}
	if ms.info.State != StateRunning && ms.info.State != StatePaused {
		return fmt.Errorf("sandbox %s is not running (state: %s)", id, ms.info.State)
	}

	if out, err := r.runDockerCmd(ctx, "stop", "--time=5", containerName(id)); err != nil {
		r.logger.Warn("docker stop failed, forcing kill",
			zap.String("id", id), zap.Error(err), zap.String("output", strings.TrimSpace(out)))
		if out2, err2 := r.runDockerCmd(ctx, "kill", containerName(id)); err2 != nil {
			return fmt.Errorf("docker kill failed: %w (output: %s)", err2, strings.TrimSpace(out2))
		}
	}

	now := time.Now().UTC()
	ms.info.State = StateStopped
	ms.info.StoppedAt = &now
	ms.idleSince = nil

	// Tear down the per-sandbox bridge network after the container has stopped.
	r.deleteSandboxNetwork(ctx, id, ms.info.Spec.NetworkPolicy)

	payload, _ := json.Marshal(ms.info.Spec)
	action := kernel.NewAction(kernel.ActionSandboxStop, "kernel", payload)
	if _, err := r.kern.SignAndLog(action); err != nil {
		r.logger.Warn("failed to log sandbox stop", zap.String("id", id), zap.Error(err))
	}

	r.logger.Info("docker sandbox stopped", zap.String("id", id))
	return nil
}

// Pause suspends a running container using Docker's cgroup freeze ("docker pause").
// The container transitions to StatePaused and can be resumed via Start.
func (r *DockerRuntime) Pause(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ms, ok := r.sandboxes[id]
	if !ok {
		return fmt.Errorf("sandbox %s not found", id)
	}
	if ms.info.State != StateRunning {
		return fmt.Errorf("sandbox %s is not running, cannot pause (state: %s)", id, ms.info.State)
	}

	if out, err := r.runDockerCmd(ctx, "pause", containerName(id)); err != nil {
		return fmt.Errorf("docker pause failed: %w (output: %s)", err, strings.TrimSpace(out))
	}

	ms.info.State = StatePaused
	ms.idleSince = nil
	r.logger.Info("docker sandbox paused", zap.String("id", id))
	return nil
}

// Delete removes a Docker container sandbox and its host-side state directory.
// Force-removes the container regardless of its current state.
func (r *DockerRuntime) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ms, ok := r.sandboxes[id]
	if !ok {
		return fmt.Errorf("sandbox %s not found", id)
	}

	out, err := r.runDockerCmd(ctx, "rm", "-f", containerName(id))
	if err != nil && !isNoSuchContainer(out) {
		return fmt.Errorf("docker rm failed: %w (output: %s)", err, strings.TrimSpace(out))
	}

	stateDir := r.sandboxDir(id)
	if rmErr := os.RemoveAll(stateDir); rmErr != nil {
		r.logger.Warn("failed to remove sandbox state dir",
			zap.String("id", id), zap.String("dir", stateDir), zap.Error(rmErr))
	}

	// Remove the per-sandbox bridge network (no-op for NoNetwork sandboxes).
	r.deleteSandboxNetwork(ctx, id, ms.info.Spec.NetworkPolicy)

	delete(r.sandboxes, id)

	payload, _ := json.Marshal(ms.info.Spec)
	action := kernel.NewAction(kernel.ActionSandboxDelete, "kernel", payload)
	if _, err := r.kern.SignAndLog(action); err != nil {
		r.logger.Warn("failed to log sandbox delete", zap.String("id", id), zap.Error(err))
	}

	r.logger.Info("docker sandbox deleted", zap.String("id", id))
	return nil
}

// List returns info for all known sandboxes.
func (r *DockerRuntime) List(_ context.Context) ([]SandboxInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]SandboxInfo, 0, len(r.sandboxes))
	for _, ms := range r.sandboxes {
		infos = append(infos, ms.info)
	}
	return infos, nil
}

// Status returns the current runtime info for the given sandbox ID.
func (r *DockerRuntime) Status(_ context.Context, id string) (*SandboxInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ms, ok := r.sandboxes[id]
	if !ok {
		return nil, fmt.Errorf("sandbox %s not found", id)
	}
	info := ms.info
	return &info, nil
}

// ─── IPC ──────────────────────────────────────────────────────────────────────

// SendToVM connects to the running container's guest-agent via Unix socket and
// sends a JSON request, returning the raw JSON response.
//
// It retries for up to ~30 s (90 × 333 ms) to give the guest-agent time to
// start and create agent.sock — the same retry window used by the vsock path in
// FirecrackerRuntime.SendToVM.  On each successful dial the sandbox's idle
// timer is reset.
func (r *DockerRuntime) SendToVM(ctx context.Context, id string, req interface{}) (json.RawMessage, error) {
	r.mu.RLock()
	ms, ok := r.sandboxes[id]
	if !ok {
		r.mu.RUnlock()
		return nil, fmt.Errorf("sandbox %s not found", id)
	}
	state := ms.info.State
	r.mu.RUnlock()

	if state != StateRunning {
		return nil, fmt.Errorf("sandbox %s is not running (state: %s)", id, state)
	}

	sockPath := r.socketPath(id)
	const (
		maxAttempts = 90
		retryDelay  = 333 * time.Millisecond
	)
	dialer := net.Dialer{Timeout: 2 * time.Second}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		conn, err := dialer.DialContext(ctx, "unix", sockPath)
		if err == nil {
			// Reset the idle timer on successful contact.
			r.mu.Lock()
			if ms2, ok2 := r.sandboxes[id]; ok2 {
				ms2.idleSince = nil
			}
			r.mu.Unlock()
			return dockerExchangeJSON(conn, id, req)
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("sandbox %s: context cancelled waiting for unix socket: %w", id, ctx.Err())
		case <-time.After(retryDelay):
		}
	}
	return nil, fmt.Errorf("sandbox %s: unix socket %s not ready after retries: %w", id, sockPath, lastErr)
}

// dockerExchangeJSON writes a JSON request and reads a JSON response on conn.
func dockerExchangeJSON(conn net.Conn, id string, req interface{}) (json.RawMessage, error) {
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request to sandbox %s: %w", id, err)
	}
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to read response from sandbox %s: %w", id, err)
	}
	return raw, nil
}

// ─── Idle-pause goroutine ─────────────────────────────────────────────────────

// idlePauseLoop runs in a goroutine and periodically pauses sandboxes that have
// been idle (no SendToVM calls) for longer than cfg.IdleTimeout.
func (r *DockerRuntime) idlePauseLoop() {
	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()
	for range ticker.C {
		r.checkIdleSandboxes()
	}
}

// checkIdleSandboxes inspects all running sandboxes and pauses those that
// have exceeded the idle timeout.
func (r *DockerRuntime) checkIdleSandboxes() {
	if r.cfg.IdleTimeout <= 0 {
		return
	}
	now := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, ms := range r.sandboxes {
		if ms.info.State != StateRunning {
			continue
		}
		if ms.idleSince == nil {
			// First observation — mark as potentially idle.
			t := now
			ms.idleSince = &t
			continue
		}
		if now.Sub(*ms.idleSince) < r.cfg.IdleTimeout {
			continue
		}
		// Sandbox has been idle long enough — freeze it.
		idleFor := now.Sub(*ms.idleSince)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if out, err := r.runDockerCmd(ctx, "pause", containerName(id)); err != nil {
			r.logger.Warn("idle-pause failed",
				zap.String("id", id), zap.Error(err), zap.String("output", strings.TrimSpace(out)))
		} else {
			ms.info.State = StatePaused
			ms.idleSince = nil
			r.logger.Info("idle sandbox paused",
				zap.String("id", id),
				zap.Duration("idle_for", idleFor),
			)
		}
		cancel()
	}
}

// ─── Cleanup ──────────────────────────────────────────────────────────────────

// Cleanup force-removes all running or paused containers and updates their
// in-memory state to stopped.  Called on daemon shutdown.
func (r *DockerRuntime) Cleanup(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var stopped int
	for id, ms := range r.sandboxes {
		if ms.info.State != StateRunning && ms.info.State != StatePaused {
			continue
		}
		r.logger.Info("cleaning up docker sandbox", zap.String("id", id))
		out, err := r.runDockerCmd(ctx, "rm", "-f", containerName(id))
		if err != nil && !isNoSuchContainer(out) {
			r.logger.Error("failed to remove docker sandbox during cleanup",
				zap.String("id", id), zap.Error(err))
		}
		now := time.Now().UTC()
		ms.info.State = StateStopped
		ms.info.StoppedAt = &now
		ms.idleSince = nil
		stopped++
	}
	if stopped > 0 {
		r.logger.Info("docker cleanup complete", zap.Int("sandboxes_stopped", stopped))
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// isNoSuchContainer returns true when Docker output indicates the container
// does not exist, which is a safe "already gone" condition for rm/kill.
func isNoSuchContainer(out string) bool {
	return strings.Contains(strings.ToLower(out), "no such container")
}
