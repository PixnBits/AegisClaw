package ipc

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

// mockProposalStore implements storeapi.ProposalStore for testing without a remote client.
type mockProposalStore struct {
	mu        sync.RWMutex
	proposals map[string]*proposal.Proposal
	list      []proposal.ProposalSummary
	nextID    int
	errReturn error
}

func (m *mockProposalStore) Create(p *proposal.Proposal) error {
	if m.errReturn != nil {
		return m.errReturn
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	p.ID = fmt.Sprintf("test-proposal-%d", m.nextID)
	p.Status = proposal.StatusDraft
	m.proposals[p.ID] = p
	m.list = append(m.list, proposal.ProposalSummary{
		ID:        p.ID,
		Title:     p.Title,
		Status:    p.Status,
		CreatedAt: p.CreatedAt,
	})
	return nil
}

func (m *mockProposalStore) Get(id string) (*proposal.Proposal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.proposals[id]
	if !ok {
		return nil, fmt.Errorf("proposal not found: %s", id)
	}
	return p, nil
}

func (m *mockProposalStore) Update(p *proposal.Proposal) error {
	if m.errReturn != nil {
		return m.errReturn
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proposals[p.ID] = p
	return nil
}

func (m *mockProposalStore) List() ([]proposal.ProposalSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.errReturn != nil {
		return nil, m.errReturn
	}
	result := make([]proposal.ProposalSummary, len(m.list))
	copy(result, m.list)
	return result, nil
}

func (m *mockProposalStore) ListByStatus(status proposal.Status) ([]proposal.ProposalSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.errReturn != nil {
		return nil, m.errReturn
	}
	var result []proposal.ProposalSummary
	for _, ps := range m.list {
		if ps.Status == status {
			result = append(result, ps)
		}
	}
	return result, nil
}

func (m *mockProposalStore) ResolveID(prefix string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id := range m.proposals {
		if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
			return id, nil
		}
	}
	return "", fmt.Errorf("no resolution for prefix: %s", prefix)
}

func (m *mockProposalStore) Import(p *proposal.Proposal) error {
	if m.errReturn != nil {
		return m.errReturn
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proposals[p.ID] = p
	return nil
}

// TestProposalBackend_List verifies proposal.list returns summaries via the remote-backed backend.
func TestProposalBackend_List(t *testing.T) {
	logger := zap.NewNop()
	mock := &mockProposalStore{
		proposals: make(map[string]*proposal.Proposal),
		list:      make([]proposal.ProposalSummary, 0),
	}
	backend := NewProposalBackend(mock, logger)

	// Pre-populate a proposal so list returns data.
	p, err := proposal.NewProposal("Test Proposal", "A test description", proposal.CategoryNewSkill, "tester")
	if err != nil {
		t.Fatalf("NewProposal failed: %v", err)
	}
	if err := mock.Create(p); err != nil {
		t.Fatalf("mock Create failed: %v", err)
	}

	msg := &Message{ID: "pl-1", Type: "proposal.list"}
	result, err := backend.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(proposal.list) returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if len(result.Response) == 0 {
		t.Error("expected non-empty response body")
	}

	var summaries []proposal.ProposalSummary
	if err := json.Unmarshal(result.Response, &summaries); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if len(summaries) != 1 {
		t.Errorf("expected 1 proposal summary, got %d", len(summaries))
	}
}

// TestProposalBackend_Status verifies proposal.status returns proposal details.
func TestProposalBackend_Status(t *testing.T) {
	logger := zap.NewNop()
	mock := &mockProposalStore{
		proposals: make(map[string]*proposal.Proposal),
	}
	backend := NewProposalBackend(mock, logger)

	// Create a proposal via mock.
	p, err := proposal.NewProposal("Status Test", "For status check", proposal.CategoryEditSkill, "tester")
	if err != nil {
		t.Fatalf("NewProposal failed: %v", err)
	}
	if err := mock.Create(p); err != nil {
		t.Fatalf("mock Create failed: %v", err)
	}

	id := p.ID

	msg := &Message{
		ID:      "ps-1",
		Type:    "proposal.status",
		Payload: json.RawMessage(fmt.Sprintf(`{"proposal_id":%q}`, id)),
	}
	result, err := backend.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(proposal.status) returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	var status map[string]interface{}
	if err := json.Unmarshal(result.Response, &status); err != nil {
		t.Fatalf("invalid status response JSON: %v", err)
	}
	if status["proposal_id"] != id {
		t.Errorf("expected proposal_id %q, got %v", id, status["proposal_id"])
	}
	if status["title"] != "Status Test" {
		t.Errorf("expected title 'Status Test', got %v", status["title"])
	}
}

// TestProposalBackend_Create verifies proposal.create forwards to the remote client.
func TestProposalBackend_Create(t *testing.T) {
	logger := zap.NewNop()
	mock := &mockProposalStore{
		proposals: make(map[string]*proposal.Proposal),
	}
	backend := NewProposalBackend(mock, logger)

	payload := json.RawMessage(`{
		"title": "Create Test Proposal",
		"description": "Testing create flow",
		"category": "new_skill",
		"author": "tester"
	}`)

	msg := &Message{ID: "pc-1", Type: "proposal.create", Payload: payload}
	result, err := backend.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(proposal.create) returned error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	var created map[string]interface{}
	if err := json.Unmarshal(result.Response, &created); err != nil {
		t.Fatalf("invalid create response JSON: %v", err)
	}
	if created["status"] != "draft" {
		t.Errorf("expected status 'draft', got %v", created["status"])
	}
	if _, ok := created["proposal_id"]; !ok {
		t.Error("expected proposal_id in response")
	}
}

// TestProposalBackend_MissingFields verifies proposal.create rejects missing required fields.
func TestProposalBackend_MissingFields(t *testing.T) {
	logger := zap.NewNop()
	mock := &mockProposalStore{
		proposals: make(map[string]*proposal.Proposal),
	}
	backend := NewProposalBackend(mock, logger)

	// Missing title field.
	msg := &Message{
		ID:    "mf-1",
		Type:  "proposal.create",
		Payload: json.RawMessage(`{"description": "no title", "author": "tester"}`),
	}
	result, err := backend.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(proposal.create) returned error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for missing title, got success")
	}
}

// TestProposalBackend_CreateRemoteError verifies errors from the remote store are sanitized.
func TestProposalBackend_CreateErrorSanitized(t *testing.T) {
	logger := zap.NewNop()
	mock := &mockProposalStore{
		proposals: make(map[string]*proposal.Proposal),
		errReturn: fmt.Errorf("remote store connection refused: vsock timeout"),
	}
	backend := NewProposalBackend(mock, logger)

	msg := &Message{
		ID:    "ce-1",
		Type:  "proposal.create",
		Payload: json.RawMessage(`{"title":"x","description":"y","category":"new_skill","author":"tester"}`),
	}
	result, err := backend.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(proposal.create) returned error: %v", err)
	}
	if result.Success {
		t.Error("expected failure when remote store returns error, got success")
	}
	if result.Error == "" {
		t.Error("expected sanitized error message, got empty")
	}
}

// actionToSender returns the sender VMID for a given action type.
func actionToSender(action string) string {
	switch action {
	case "proposal.list":
		return "list-sender"
	case "proposal.status":
		return "status-sender"
	case "proposal.create":
		return "create-sender"
	default:
		return "default-sender"
	}
}

// TestHubRoutesProposalActionsThroughStoreVM verifies the message hub routes
// proposal actions to the store-vm backend via preferredBackendForAction.
func TestHubRoutesProposalActionsThroughStoreVM(t *testing.T) {
	logger := zap.NewNop()

	for _, action := range []string{"proposal.list", "proposal.status", "proposal.create"} {
		mockStore := &mockProposalStore{
			proposals: make(map[string]*proposal.Proposal),
			list:      make([]proposal.ProposalSummary, 0),
		}
		backend := NewProposalBackend(mockStore, logger)

		hub := NewMessageHubNoKernel(logger)
		if err := hub.Start(); err != nil {
			t.Fatalf("Start() for %q failed: %v", action, err)
		}

		// Register store-vm skill.
		if err := hub.RegisterSkill("store-vm", backend.Handle); err != nil {
			t.Fatalf("RegisterSkill(store-vm) for %q failed: %v", action, err)
		}

		// Register sender with required roles.
		if err := hub.RegisterVM("aegishub-mock", RoleHub); err != nil {
			t.Fatalf("RegisterVM(RoleHub) for %q failed: %v", action, err)
		}
		if err := hub.RegisterVM("cli-mock", RoleCLI); err != nil {
			t.Fatalf("RegisterVM(RoleCLI) for %q failed: %v", action, err)
		}

		var payload string
		switch action {
		case "proposal.status":
			// Create a proposal in the mock so status can find it.
			p, err := proposal.NewProposal("Status Test", "For routing test", proposal.CategoryEditSkill, "tester")
			if err != nil {
				t.Fatalf("NewProposal failed for %q: %v", action, err)
			}
			if err := mockStore.Create(p); err != nil {
				t.Fatalf("mock Create failed for %q: %v", action, err)
			}
			payload = fmt.Sprintf(`{"proposal_id":%q}`, p.ID)
		default:
			payload = `{"title":"routing-test","description":"test","category":"new_skill","author":"tester"}`
		}

		controller := &Message{
			ID:      fmt.Sprintf("rpc-%s", action),
			From:    actionToSender(action),
			To:      MessageHubID,
			Type:    "controlplane.request",
			Payload: json.RawMessage(`{"action":` + fmt.Sprintf("%q", action) + `,` + `"data":` + payload + `}`),
		}

		if err := hub.RegisterVM(actionToSender(action), RoleHub); err != nil {
			t.Fatalf("RegisterVM failed for %q: %v", action, err)
		}

		result, routeErr := hub.RouteMessage(actionToSender(action), controller)
		if routeErr != nil {
			t.Fatalf("RouteMessage for %q failed: %v", action, routeErr)
		}
		if result == nil {
			t.Fatalf("nil result for action %q", action)
		}

		switch action {
		case "proposal.list":
			if !result.Success {
				t.Errorf("proposal.list expected success, got error: %s", result.Error)
			}
		case "proposal.status":
			if !result.Success {
				t.Errorf("proposal.status expected success, got error=%q", result.Error)
			} else {
				var status map[string]interface{}
				if err := json.Unmarshal(result.Response, &status); err != nil {
					t.Errorf("invalid status response: %v", err)
				} else if status["proposal_id"] == nil {
					t.Error("expected proposal_id in status response")
				}
			}
		case "proposal.create":
			if !result.Success {
				t.Errorf("proposal.create expected success, got error: %s", result.Error)
			}
			if result.Response == nil {
				t.Error("expected response body for proposal.create")
			} else {
				var cr map[string]interface{}
				if err := json.Unmarshal(result.Response, &cr); err != nil {
					t.Errorf("invalid create response: %v", err)
				} else if cr["proposal_id"] == nil {
					t.Error("expected proposal_id in create response")
				}
			}
		}

		hub.UnregisterSkill("store-vm")
		hub.Stop()
	}
}
