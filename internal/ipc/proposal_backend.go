package ipc

import (
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/store/remote"
	"github.com/PixnBits/AegisClaw/internal/storeapi"
	"go.uber.org/zap"
)

// proposalBackend is a remote-facing adapter (PR #56) that implements
// RouteHandler for proposal-related ControlPlane actions by delegating
// to a remote Store VM via storeapi.ProposalStore.
//
// It is registered under the "store-vm" key so that preferredBackendForAction
// routes "proposal.list", "proposal.status", and "proposal.create" to it.
//
// Security: All errors from the remote store are sanitized before
// returning to callers to prevent information leakage across the trust boundary.
type proposalBackend struct {
	store  storeapi.ProposalStore
	logger *zap.Logger
}

// NewProposalBackend creates a proposalBackend backed by the real
// storeapi.ProposalStore (typically the remote client wrapping the Store VM).
func NewProposalBackend(s storeapi.ProposalStore, logger *zap.Logger) *proposalBackend {
	return &proposalBackend{store: s, logger: logger}
}

// Handle implements the RouteHandler contract expected by MessageHub delegation.
func (b *proposalBackend) Handle(msg *Message) (*DeliveryResult, error) {
	switch msg.Type {
	case "proposal.list":
		summaries, err := b.store.List()
		if err != nil {
			b.logger.Error("proposal list failed", zap.Error(err))
			return &DeliveryResult{
				MessageID: msg.ID,
				Success:   false,
				Error:     remote.SanitizeError(err),
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
			b.logger.Error("proposal get failed", zap.String("proposal_id", req.ProposalID), zap.Error(err))
			return &DeliveryResult{
				MessageID: msg.ID,
				Success:   false,
				Error:     remote.SanitizeError(err),
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

	case "proposal.create":
		var p proposal.Proposal
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			return &DeliveryResult{
				MessageID: msg.ID,
				Success:   false,
				Error:     "invalid payload for proposal.create",
			}, nil
		}
		// Validate required fields before sending to Store VM.
		if p.Title == "" || p.Description == "" || p.Author == "" {
			return &DeliveryResult{
				MessageID: msg.ID,
				Success:   false,
				Error:     "missing required fields: title, description, author",
			}, nil
		}
		p.Status = proposal.StatusDraft
		if err := b.store.Create(&p); err != nil {
			b.logger.Error("proposal create failed", zap.String("title", p.Title), zap.Error(err))
			return &DeliveryResult{
				MessageID: msg.ID,
				Success:   false,
				Error:     remote.SanitizeError(err),
			}, nil
		}
		// Return the created proposal ID for confirmation.
		created := map[string]interface{}{
			"proposal_id": p.ID,
			"title":       p.Title,
			"status":      p.Status,
			"created_at":  p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		data, _ := json.Marshal(created)
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
var _ RouteHandler = (*proposalBackend)(nil).Handle