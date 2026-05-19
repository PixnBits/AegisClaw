package ipc

import (
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/store"
	"go.uber.org/zap"
)

// proposalBackend is a real backend adapter (Phase 9) that implements
// RouteHandler for proposal-related ControlPlane actions by delegating
// to an injected ProposalStore (typically a git-backed proposal.Store).
//
// It is registered under the "store-vm" key so that preferredBackendForAction
// routes "proposal.list" and "proposal.status" to it.
//
// The adapter is intentionally small and stateless so it can later be
// replaced by a remote Store VM client without changing the delegation
// contract in MessageHub.
type proposalBackend struct {
	store  store.ProposalStore
	logger *zap.Logger
}

// NewProposalBackend creates a proposalBackend for the given ProposalStore.
func NewProposalBackend(s store.ProposalStore, logger *zap.Logger) *proposalBackend {
	return &proposalBackend{store: s, logger: logger}
}

// handle implements the RouteHandler contract expected by MessageHub delegation.
func (b *proposalBackend) handle(msg *Message) (*DeliveryResult, error) {
	switch msg.Type {
	case "proposal.list":
		summaries, err := b.store.List()
		if err != nil {
			return &DeliveryResult{
				MessageID: msg.ID,
				Success:   false,
				Error:     "proposal list failed: " + err.Error(),
			}, nil
		}
		data, _ := json.Marshal(summaries)
		return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil

	case "proposal.status":
		var req struct {
			ProposalID string `json:"proposal_id"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		if req.ProposalID == "" {
			return &DeliveryResult{
				MessageID: msg.ID,
				Success:   false,
				Error:     "proposal_id required for status",
			}, nil
		}
		p, err := b.store.Get(req.ProposalID)
		if err != nil {
			return &DeliveryResult{
				MessageID: msg.ID,
				Success:   false,
				Error:     "proposal not found: " + err.Error(),
			}, nil
		}
		// Return a compact status payload (title, status, created_at, etc.)
		status := map[string]interface{}{
			"proposal_id": p.ID,
			"title":       p.Title,
			"status":      p.Status,
			"created_at":  p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		data, _ := json.Marshal(status)
		return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil

	default:
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     "unsupported proposal action: " + msg.Type,
		}, nil
	}
}

// Ensure it satisfies the expected handler signature used by RegisterSkill.
var _ RouteHandler = (*proposalBackend)(nil).handle