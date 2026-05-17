package court

import "context"

// Client is the interface the Host Daemon (and other components) use
// to interact with the Governance Court.
//
// This is intentionally minimal at first. The goal is to remove Court
// logic from the privileged Host Daemon and move it toward dedicated
// Court components (Court Scribe + persona VMs) as described in the
// lessons-learned architecture.
//
// The daemon should ideally only use this client to request reviews
// or submit votes, without owning or executing Court logic itself.
type Client interface {
	// Review requests a full Court review for a proposal.
	// This should eventually be handled by Court components via AegisHub.
	Review(ctx context.Context, proposalID string) error

	// Vote submits a vote on behalf of a persona (or system).
	Vote(ctx context.Context, proposalID string, persona string, vote string, reason string) error

	// GetStatus returns high-level status of a Court session (optional for now).
	GetStatus(ctx context.Context, proposalID string) (string, error)
}

// StubClient is a no-op implementation used during aggressive extraction.
// It allows us to remove the real Court engine from the daemon without
// immediately breaking everything.
type StubClient struct{}

func (s *StubClient) Review(ctx context.Context, proposalID string) error {
	return nil // TODO: route to AegisHub -> Court components
}

func (s *StubClient) Vote(ctx context.Context, proposalID string, persona string, vote string, reason string) error {
	return nil // TODO: route to AegisHub -> Court components
}

func (s *StubClient) GetStatus(ctx context.Context, proposalID string) (string, error) {
	return "stub", nil
}

var _ Client = (*StubClient)(nil)
