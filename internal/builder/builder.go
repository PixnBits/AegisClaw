package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// BuilderRuntimeInterface defines the methods needed for building code.
// This interface allows for different implementations (e.g., Firecracker-based
// for the host daemon, or in-process for the builder-agent microVM).
type BuilderRuntimeInterface interface {
	LaunchBuilder(ctx context.Context, spec *BuilderSpec) (*BuilderInfo, error)
	StopBuilder(ctx context.Context, builderID string) error
	SendBuildRequest(ctx context.Context, builderID string, msg kernel.ControlMessage) (*kernel.ControlResponse, error)
}

// BuilderState represents the lifecycle state of a builder sandbox.
type BuilderState string

const (
	BuilderStateIdle     BuilderState = "idle"
	BuilderStateBuilding BuilderState = "building"
	BuilderStateStopped  BuilderState = "stopped"
	BuilderStateError    BuilderState = "error"
)

// BuilderSpec extends SandboxSpec with builder-specific fields.
type BuilderSpec struct {
	// Base sandbox spec fields
	ID   string `json:"id"`
	Name string `json:"name"`

	// Builder-specific resource limits (higher than reviewers)
	VCPUs    int64 `json:"vcpus"`
	MemoryMB int64 `json:"memory_mb"`

	// Persistent workspace overlay for iterative builds
	WorkspaceDir string `json:"workspace_dir"`
	WorkspaceMB  int    `json:"workspace_mb"`

	// Builder rootfs with full Go toolchain
	RootfsPath string `json:"rootfs_path"`

	// Network policy: only Ollama + git module proxy
	AllowedHosts []string `json:"allowed_hosts,omitempty"`
	AllowedPorts []uint16 `json:"allowed_ports,omitempty"`

	// Proposal being built
	ProposalID string `json:"proposal_id"`
}

// DefaultBuilderSpec returns a BuilderSpec with production defaults.
func DefaultBuilderSpec(proposalID string) *BuilderSpec {
	return &BuilderSpec{
		ID:           uuid.New().String(),
		Name:         fmt.Sprintf("builder-%s", proposalID[:8]),
		VCPUs:        2,
		MemoryMB:     1024,
		WorkspaceMB:  512,
		AllowedHosts: []string{"127.0.0.1"},
		AllowedPorts: []uint16{11434},
		ProposalID:   proposalID,
	}
}

// Validate checks that the BuilderSpec has valid fields.
func (bs *BuilderSpec) Validate() error {
	if bs.ID == "" {
		return fmt.Errorf("builder ID is required")
	}
	if bs.Name == "" {
		return fmt.Errorf("builder name is required")
	}
	if bs.VCPUs < 1 || bs.VCPUs > 8 {
		return fmt.Errorf("builder VCPUs must be between 1 and 8, got %d", bs.VCPUs)
	}
	if bs.MemoryMB < 256 || bs.MemoryMB > 8192 {
		return fmt.Errorf("builder memory must be between 256 and 8192 MB, got %d", bs.MemoryMB)
	}
	if bs.WorkspaceMB < 64 || bs.WorkspaceMB > 4096 {
		return fmt.Errorf("workspace must be between 64 and 4096 MB, got %d", bs.WorkspaceMB)
	}
	if bs.ProposalID == "" {
		return fmt.Errorf("proposal ID is required")
	}
	return nil
}

// toSandboxSpec converts a BuilderSpec to a base SandboxSpec for the runtime.
func (bs *BuilderSpec) toSandboxSpec() sandbox.SandboxSpec {
	np := sandbox.NetworkPolicy{
		DefaultDeny:  true,
		AllowedHosts: bs.AllowedHosts,
		AllowedPorts: bs.AllowedPorts,
	}
	if len(np.AllowedHosts) == 0 {
		np.AllowedHosts = []string{"127.0.0.1"}
	}
	if len(np.AllowedPorts) == 0 {
		np.AllowedPorts = []uint16{11434}
	}

	return sandbox.SandboxSpec{
		ID:   bs.ID,
		Name: bs.Name,
		Resources: sandbox.Resources{
			VCPUs:    bs.VCPUs,
			MemoryMB: bs.MemoryMB,
		},
		NetworkPolicy: np,
		RootfsPath:    bs.RootfsPath,
		WorkspaceMB:   bs.WorkspaceMB,
	}
}

// BuilderConfig holds configuration for the BuilderRuntime.
type BuilderConfig struct {
	// RootfsTemplate is the path to the builder-specific rootfs
	// (Alpine + Go + git + golangci-lint + staticcheck + make)
	RootfsTemplate string `yaml:"rootfs_template" mapstructure:"rootfs_template"`

	// WorkspaceBaseDir is the host directory for persistent workspace overlays
	WorkspaceBaseDir string `yaml:"workspace_base_dir" mapstructure:"workspace_base_dir"`

	// MaxConcurrentBuilds limits parallel builder VMs
	MaxConcurrentBuilds int `yaml:"max_concurrent_builds" mapstructure:"max_concurrent_builds"`

	// BuildTimeout is the maximum time for a single build operation
	BuildTimeout time.Duration `yaml:"build_timeout" mapstructure:"build_timeout"`
}

// DefaultBuilderConfig returns production defaults.
func DefaultBuilderConfig() BuilderConfig {
	return BuilderConfig{
		RootfsTemplate:      "/var/lib/aegisclaw/rootfs-templates/builder.ext4",
		WorkspaceBaseDir:    "/var/lib/aegisclaw/workspaces",
		MaxConcurrentBuilds: 2,
		BuildTimeout:        10 * time.Minute,
	}
}

// Validate checks the BuilderConfig has valid settings.
func (bc *BuilderConfig) Validate() error {
	if bc.RootfsTemplate == "" {
		return fmt.Errorf("builder rootfs template is required")
	}
	if bc.WorkspaceBaseDir == "" {
		return fmt.Errorf("workspace base directory is required")
	}
	if bc.MaxConcurrentBuilds < 1 || bc.MaxConcurrentBuilds > 8 {
		return fmt.Errorf("max concurrent builds must be between 1 and 8, got %d", bc.MaxConcurrentBuilds)
	}
	if bc.BuildTimeout < 1*time.Minute || bc.BuildTimeout > 60*time.Minute {
		return fmt.Errorf("build timeout must be between 1 and 60 minutes, got %s", bc.BuildTimeout)
	}
	return nil
}

// BuilderInfo captures the runtime state of a builder VM.
type BuilderInfo struct {
	ID         string       `json:"id"`
	ProposalID string       `json:"proposal_id"`
	State      BuilderState `json:"state"`
	SandboxID  string       `json:"sandbox_id"`
	StartedAt  *time.Time   `json:"started_at,omitempty"`
	StoppedAt  *time.Time   `json:"stopped_at,omitempty"`
	Error      string       `json:"error,omitempty"`
}

// BuilderRuntime manages dedicated builder Firecracker sandboxes.
// It delegates to the base FirecrackerRuntime for VM lifecycle
// while adding builder-specific configuration and constraints.
type BuilderRuntime struct {
	config    BuilderConfig
	runtime   *sandbox.FirecrackerRuntime
	kern      *kernel.Kernel
	logger    *zap.Logger
	mu        sync.Mutex
	builders  map[string]*BuilderInfo
	semaphore chan struct{}
}

// NewBuilderRuntime creates a BuilderRuntime backed by the base Firecracker runtime.
func NewBuilderRuntime(cfg BuilderConfig, rt *sandbox.FirecrackerRuntime, kern *kernel.Kernel, logger *zap.Logger) (*BuilderRuntime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid builder config: %w", err)
	}
	if rt == nil {
		return nil, fmt.Errorf("firecracker runtime is required")
	}
	if kern == nil {
		return nil, fmt.Errorf("kernel is required")
	}

	return &BuilderRuntime{
		config:    cfg,
		runtime:   rt,
		kern:      kern,
		logger:    logger,
		builders:  make(map[string]*BuilderInfo),
		semaphore: make(chan struct{}, cfg.MaxConcurrentBuilds),
	}, nil
}

// LaunchBuilder creates and starts a dedicated builder sandbox for a proposal.
func (br *BuilderRuntime) LaunchBuilder(ctx context.Context, spec *BuilderSpec) (*BuilderInfo, error) {
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid builder spec: %w", err)
	}

	// Acquire concurrency slot
	select {
	case br.semaphore <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled waiting for builder slot: %w", ctx.Err())
	}

	br.mu.Lock()
	defer br.mu.Unlock()

	// Set rootfs from config if not specified
	if spec.RootfsPath == "" {
		spec.RootfsPath = br.config.RootfsTemplate
	}

	// Convert to base sandbox spec
	sandboxSpec := spec.toSandboxSpec()

	// Create the sandbox
	if err := br.runtime.Create(ctx, sandboxSpec); err != nil {
		<-br.semaphore
		return nil, fmt.Errorf("failed to create builder sandbox: %w", err)
	}

	// Start the sandbox
	if err := br.runtime.Start(ctx, spec.ID); err != nil {
		br.runtime.Delete(ctx, spec.ID)
		<-br.semaphore
		return nil, fmt.Errorf("failed to start builder sandbox: %w", err)
	}

	now := time.Now().UTC()
	info := &BuilderInfo{
		ID:         spec.ID,
		ProposalID: spec.ProposalID,
		State:      BuilderStateBuilding,
		SandboxID:  spec.ID,
		StartedAt:  &now,
	}
	br.builders[spec.ID] = info

	// Log the builder launch
	payload, _ := json.Marshal(map[string]interface{}{
		"builder_id":  spec.ID,
		"proposal_id": spec.ProposalID,
		"vcpus":       spec.VCPUs,
		"memory_mb":   spec.MemoryMB,
	})
	action := kernel.NewAction(kernel.ActionBuilderStart, "builder-runtime", payload)
	if _, err := br.kern.SignAndLog(action); err != nil {
		br.logger.Error("failed to log builder launch", zap.Error(err))
	}

	br.logger.Info("builder sandbox launched",
		zap.String("builder_id", spec.ID),
		zap.String("proposal_id", spec.ProposalID),
		zap.Int64("vcpus", spec.VCPUs),
		zap.Int64("memory_mb", spec.MemoryMB),
	)

	return info, nil
}

// SendBuildRequest sends a build command to the builder sandbox via the control plane.
func (br *BuilderRuntime) SendBuildRequest(ctx context.Context, builderID string, msg kernel.ControlMessage) (*kernel.ControlResponse, error) {
	br.mu.Lock()
	info, ok := br.builders[builderID]
	br.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("builder %s not found", builderID)
	}
	if info.State != BuilderStateBuilding {
		return nil, fmt.Errorf("builder %s is not in building state (state=%s)", builderID, info.State)
	}

	return br.kern.ControlPlane().Send(builderID, msg)
}

// StopBuilder stops and cleans up a builder sandbox.
func (br *BuilderRuntime) StopBuilder(ctx context.Context, builderID string) error {
	br.mu.Lock()
	info, ok := br.builders[builderID]
	if !ok {
		br.mu.Unlock()
		return fmt.Errorf("builder %s not found", builderID)
	}
	br.mu.Unlock()

	// Stop through the base runtime
	if err := br.runtime.Stop(ctx, builderID); err != nil {
		br.logger.Error("failed to stop builder sandbox",
			zap.String("builder_id", builderID),
			zap.Error(err),
		)
	}

	// Delete sandbox resources
	if err := br.runtime.Delete(ctx, builderID); err != nil {
		br.logger.Error("failed to delete builder sandbox",
			zap.String("builder_id", builderID),
			zap.Error(err),
		)
	}

	// Release concurrency slot
	<-br.semaphore

	now := time.Now().UTC()
	br.mu.Lock()
	info.State = BuilderStateStopped
	info.StoppedAt = &now
	br.mu.Unlock()

	// Log the builder stop
	payload, _ := json.Marshal(map[string]string{
		"builder_id":  builderID,
		"proposal_id": info.ProposalID,
	})
	action := kernel.NewAction(kernel.ActionBuilderStop, "builder-runtime", payload)
	if _, err := br.kern.SignAndLog(action); err != nil {
		br.logger.Error("failed to log builder stop", zap.Error(err))
	}

	br.logger.Info("builder sandbox stopped",
		zap.String("builder_id", builderID),
		zap.String("proposal_id", info.ProposalID),
	)

	return nil
}

// Status returns the current state of a builder.
func (br *BuilderRuntime) Status(ctx context.Context, builderID string) (*BuilderInfo, error) {
	br.mu.Lock()
	defer br.mu.Unlock()

	info, ok := br.builders[builderID]
	if !ok {
		return nil, fmt.Errorf("builder %s not found", builderID)
	}

	// Sync with base runtime status
	sandboxInfo, err := br.runtime.Status(ctx, builderID)
	if err == nil {
		switch sandboxInfo.State {
		case sandbox.StateRunning:
			if info.State == BuilderStateIdle {
				info.State = BuilderStateBuilding
			}
		case sandbox.StateStopped:
			info.State = BuilderStateStopped
		case sandbox.StateError:
			info.State = BuilderStateError
			info.Error = sandboxInfo.Error
		}
	}

	return info, nil
}

// ListBuilders returns all tracked builders.
func (br *BuilderRuntime) ListBuilders() []*BuilderInfo {
	br.mu.Lock()
	defer br.mu.Unlock()

	result := make([]*BuilderInfo, 0, len(br.builders))
	for _, info := range br.builders {
		result = append(result, info)
	}
	return result
}

// ActiveBuilders returns only builders currently building.
func (br *BuilderRuntime) ActiveBuilders() []*BuilderInfo {
	br.mu.Lock()
	defer br.mu.Unlock()

	var active []*BuilderInfo
	for _, info := range br.builders {
		if info.State == BuilderStateBuilding {
			active = append(active, info)
		}
	}
	return active
}

// Cleanup stops all active builders. Called during shutdown.
func (br *BuilderRuntime) Cleanup(ctx context.Context) {
	br.mu.Lock()
	ids := make([]string, 0)
	for id, info := range br.builders {
		if info.State == BuilderStateBuilding {
			ids = append(ids, id)
		}
	}
	br.mu.Unlock()

	for _, id := range ids {
		if err := br.StopBuilder(ctx, id); err != nil {
			br.logger.Error("cleanup: failed to stop builder",
				zap.String("builder_id", id),
				zap.Error(err),
			)
		}
	}

	br.logger.Info("builder runtime cleanup complete",
		zap.Int("builders_stopped", len(ids)),
	)
}
