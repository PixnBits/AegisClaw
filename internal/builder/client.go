package builder

import "context"

// Client is the interface used to interact with builder/orchestrator functionality.
//
// During the aggressive extraction, the Host Daemon should no longer own
// BuildOrchestrator, Pipeline, or CodeGenerator logic.
// This client provides a clean seam so the daemon can request builds
// without running the logic itself.
//
// Long term, this should route through AegisHub to dedicated builder coordination
// or Builder VMs.
type Client interface {
	// TriggerBuild requests that a proposal be built/implemented.
	// This is currently a no-op stub during extraction.
	TriggerBuild(ctx context.Context, proposalID string) error

	// GetStatus returns the status of a build for a given proposal (stub for now).
	GetStatus(ctx context.Context, proposalID string) (string, error)
}

// StubClient is a temporary no-op implementation used while we remove
// BuildOrchestrator ownership from the Host Daemon.
type StubClient struct{}

func (s *StubClient) TriggerBuild(ctx context.Context, proposalID string) error {
	return nil // TODO: route via AegisHub to builder coordination
}

func (s *StubClient) GetStatus(ctx context.Context, proposalID string) (string, error) {
	return "stub", nil
}

var _ Client = (*StubClient)(nil)
