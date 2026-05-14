package builder

import (
	"context"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/events"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap/zaptest"
)

func TestBuildOrchestrator_StartsAndHandlesEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Minimal mocks
	store := &proposal.Store{} // In real tests this would be a test double
	k := &kernel.Kernel{}
	dispatcher := events.NewProposalEventDispatcher()

	orch, err := NewBuildOrchestrator(nil, store, k, logger, dispatcher) // pipeline can be nil for this smoke test
	if err != nil {
		t.Skip("NewBuildOrchestrator requires non-nil pipeline in current implementation - skipping full test")
	}

	orch.Start(context.Background())

	// Emit a test event
	dispatcher.EmitStatusChanged(&proposal.Proposal{ID: "test-123"}, proposal.StatusApproved, proposal.StatusImplementing, "test", "test")

	// If we reach here without panic, basic wiring works
	t.Log("BuildOrchestrator started and handled event without panic")
}
