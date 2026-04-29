package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// BuildRequest is sent to the builder agent to trigger a build.
type BuildRequest struct {
	ProposalID  string          `json:"proposal_id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Spec        json.RawMessage `json:"spec,omitempty"`
	Round       int             `json:"round"`
}

// BuildResponse is returned by the builder agent after a build completes or fails.
type BuildResponse struct {
	ProposalID  string            `json:"proposal_id"`
	State       PipelineState     `json:"state"`
	CommitHash  string            `json:"commit_hash,omitempty"`
	Branch      string            `json:"branch,omitempty"`
	Files       map[string]string `json:"files,omitempty"`
	FileHashes  map[string]string `json:"file_hashes,omitempty"`
	Reasoning   string            `json:"reasoning,omitempty"`
	Error       string            `json:"error,omitempty"`
	Round       int               `json:"round"`
}

// BuilderLauncher abstracts builder microVM creation/destruction.
type BuilderLauncher interface {
	// LaunchBuilder creates and starts a builder microVM.
	LaunchBuilder(ctx context.Context) (string, error)
	
	// SendBuildRequest sends a build request to the builder VM via vsock.
	SendBuildRequest(ctx context.Context, sandboxID string, req *BuildRequest) (*BuildResponse, error)
	
	// StopBuilder stops and destroys the builder microVM.
	StopBuilder(ctx context.Context, sandboxID string) error
	
	// GetStatus checks if builder VM is still running.
	GetStatus(ctx context.Context, sandboxID string) (string, error)
}

// FirecrackerBuilderLauncher manages builder microVMs using Firecracker.
type FirecrackerBuilderLauncher struct {
	runtime *sandbox.FirecrackerRuntime
	config  sandbox.RuntimeConfig
	proxy   *llm.OllamaProxy
	logger  *zap.Logger
}

// NewFirecrackerBuilderLauncher creates a launcher for builder microVMs.
func NewFirecrackerBuilderLauncher(
	runtime *sandbox.FirecrackerRuntime,
	cfg sandbox.RuntimeConfig,
	proxy *llm.OllamaProxy,
	logger *zap.Logger,
) *FirecrackerBuilderLauncher {
	return &FirecrackerBuilderLauncher{
		runtime: runtime,
		config:  cfg,
		proxy:   proxy,
		logger:  logger,
	}
}

// LaunchBuilder creates and starts a builder microVM.
// The VM has restricted network access (only Ollama via proxy).
func (fbl *FirecrackerBuilderLauncher) LaunchBuilder(ctx context.Context) (string, error) {
	sandboxID := "builder-" + uuid.New().String()[:8]
	
	spec := sandbox.SandboxSpec{
		ID:   sandboxID,
		Name: "builder-agent",
		Resources: sandbox.Resources{
			VCPUs:    2,  // Builders need more CPU for compilation
			MemoryMB: 2048, // Builders need more memory for builds
		},
		// Builder needs limited network for git operations and package downloads
		// but should be restricted to approved hosts only
		NetworkPolicy: sandbox.NetworkPolicy{
			DefaultDeny:  true,
			AllowedHosts: []string{"127.0.0.1"}, // Ollama only by default
			AllowedPorts: []uint16{11434},       // Ollama port
		},
		RootfsPath: fbl.config.RootfsTemplate,
		InitPath:   "/sbin/builder-agent",
	}

	if err := fbl.runtime.Create(ctx, spec); err != nil {
		return "", fmt.Errorf("failed to create builder sandbox: %w", err)
	}
	
	if err := fbl.runtime.Start(ctx, sandboxID); err != nil {
		fbl.runtime.Delete(ctx, sandboxID)
		return "", fmt.Errorf("failed to start builder sandbox: %w", err)
	}

	// Start the LLM proxy for this VM
	vsockPath, err := fbl.runtime.VsockPath(sandboxID)
	if err != nil {
		fbl.runtime.Stop(ctx, sandboxID)
		fbl.runtime.Delete(ctx, sandboxID)
		return "", fmt.Errorf("failed to get vsock path for builder sandbox: %w", err)
	}
	
	if err := fbl.proxy.StartForVM(sandboxID, vsockPath); err != nil {
		fbl.runtime.Stop(ctx, sandboxID)
		fbl.runtime.Delete(ctx, sandboxID)
		return "", fmt.Errorf("failed to start llm proxy for builder sandbox: %w", err)
	}

	fbl.logger.Info("builder sandbox launched",
		zap.String("sandbox_id", sandboxID),
	)
	
	return sandboxID, nil
}

// SendBuildRequest sends a build request to the builder VM and waits for response.
func (fbl *FirecrackerBuilderLauncher) SendBuildRequest(
	ctx context.Context,
	sandboxID string,
	req *BuildRequest,
) (*BuildResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal build request: %w", err)
	}

	ctlMsg := kernel.ControlMessage{
		Type:    "builder.execute",
		Payload: payload,
	}

	// Send request via vsock
	rawResp, err := fbl.runtime.SendToVM(ctx, sandboxID, ctlMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to send build request to VM: %w", err)
	}

	// Parse response
	var resp BuildResponse
	if err := json.Unmarshal(rawResp, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal build response: %w", err)
	}

	fbl.logger.Info("received build response",
		zap.String("sandbox_id", sandboxID),
		zap.String("proposal_id", resp.ProposalID),
		zap.String("state", string(resp.State)),
	)

	return &resp, nil
}

// StopBuilder stops and destroys the builder microVM.
func (fbl *FirecrackerBuilderLauncher) StopBuilder(ctx context.Context, sandboxID string) error {
	// Stop LLM proxy first (no error return)
	fbl.proxy.StopForVM(sandboxID)

	// Stop the VM
	if err := fbl.runtime.Stop(ctx, sandboxID); err != nil {
		fbl.logger.Warn("failed to stop builder sandbox",
			zap.String("sandbox_id", sandboxID),
			zap.Error(err),
		)
	}

	// Delete the VM
	if err := fbl.runtime.Delete(ctx, sandboxID); err != nil {
		return fmt.Errorf("failed to delete builder sandbox: %w", err)
	}

	fbl.logger.Info("builder sandbox stopped",
		zap.String("sandbox_id", sandboxID),
	)

	return nil
}

// GetStatus checks if the builder VM is still running.
func (fbl *FirecrackerBuilderLauncher) GetStatus(ctx context.Context, sandboxID string) (string, error) {
	info, err := fbl.runtime.Status(ctx, sandboxID)
	if err != nil {
		return "", fmt.Errorf("failed to get builder status: %w", err)
	}
	return strings.ToLower(string(info.State)), nil
}
