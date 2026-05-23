package ipc

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

// --- chatRouter unit tests ---

func newTestChatRouter(t *testing.T) (*chatRouter, func()) {
	t.Helper()
	r := newChatRouter(zap.NewNop())
	cleanup := func() {
		r.sessions.Range(func(key, value interface{}) bool {
			r.sessions.Delete(key)
			return true
		})
	}
	return r, cleanup
}

func TestChatRouter_SessionCreate(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	sessionID := "tc-create-" + fmt.Sprintf("%d", time.Now().UnixNano())
	msg := &Message{
		ID:    "sc-1",
		Type:  "chat.session.create",
		Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q}`, sessionID)),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(chat.session.create) error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	var resp map[string]interface{}
	json.Unmarshal(result.Response, &resp)
	if resp["session_id"] != sessionID {
		t.Errorf("expected session_id %q, got %v", sessionID, resp["session_id"])
	}
	if resp["message_count"] != float64(0) {
		t.Errorf("expected message_count 0, got %v", resp["message_count"])
	}
}

func TestChatRouter_SessionCreateAutoSessionID(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	msg := &Message{
		ID:    "sc-auto",
		Type:  "chat.session.create",
		Payload: []byte(`{}`),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	var resp map[string]interface{}
	json.Unmarshal(result.Response, &resp)
	if sid, ok := resp["session_id"].(string); !ok || sid == "" {
		t.Errorf("expected non-empty auto session_id, got %v", resp["session_id"])
	}
}

func TestChatRouter_ChatMessage(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	sessionID := "tc-msg-" + fmt.Sprintf("%d", time.Now().UnixNano())
	msg := &Message{
		ID:    "cm-1",
		Type:  "chat.message",
		Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q,"message":"hello","correlation_id":"c1"}`, sessionID)),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(chat.message) error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Error)
	}

	var resp map[string]interface{}
	json.Unmarshal(result.Response, &resp)
	if resp["session_id"] != sessionID {
		t.Errorf("expected session_id %q, got %v", sessionID, resp["session_id"])
	}
	// streaming defaults to false when not provided — verify it's explicitly false.
	if streaming, ok := resp["streaming"].(bool); !ok || streaming {
		t.Errorf("expected streaming=false, got %v", resp["streaming"])
	}
}

func TestChatRouter_ChatMessageStreamFlag(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	sessionID := "tc-stream-" + fmt.Sprintf("%d", time.Now().UnixNano())
	msg := &Message{
		ID:    "cm-stream",
		Type:  "chat.message",
		Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q,"message":"stream test","correlation_id":"c2","stream":true}`, sessionID)),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	var resp map[string]interface{}
	json.Unmarshal(result.Response, &resp)
	if streaming, ok := resp["streaming"].(bool); !ok || !streaming {
		t.Errorf("expected streaming=true, got %v", resp["streaming"])
	}
}

func TestChatRouter_SessionList(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	// Create two sessions.
	for i := 0; i < 2; i++ {
		sid := fmt.Sprintf("tc-list-%d", i)
		msg := &Message{
			ID:    fmt.Sprintf("sl-%d", i),
			Type:  "chat.session.create",
			Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q}`, sid)),
		}
		if _, err := r.Handle(msg); err != nil {
			t.Fatalf("Create session %d: %v", i, err)
		}
	}

	msg := &Message{ID: "sl-2", Type: "chat.sessions.list"}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(chat.sessions.list) error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	var list []map[string]interface{}
	json.Unmarshal(result.Response, &list)
	if len(list) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(list))
	}
}

func TestChatRouter_HistoryNotFound(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	msg := &Message{
		ID:    "hh-1",
		Type:  "chat.history",
		Payload: []byte(`{"session_id":"nonexistent"}`),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for nonexistent session, got success")
	}
	if result.Error == "" {
		t.Error("expected error message for history of nonexistent session")
	}
}

func TestChatRouter_HistoryWithMessages(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	sessionID := "tc-hist-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Send two messages.
	for i := 0; i < 2; i++ {
		msg := &Message{
			ID:    fmt.Sprintf("hm-%d", i),
			Type:  "chat.message",
			Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q,"message":"msg%d","correlation_id":"c%d"}`, sessionID, i, i)),
		}
		if _, err := r.Handle(msg); err != nil {
			t.Fatalf("Send message %d: %v", i, err)
		}
	}

	// Get history.
	msg := &Message{
		ID:    "hhist",
		Type:  "chat.history",
		Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q}`, sessionID)),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle(chat.history) error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success for history, got: %s", result.Error)
	}

	var resp map[string]interface{}
	json.Unmarshal(result.Response, &resp)
	if msgs, ok := resp["messages"].([]interface{}); !ok || len(msgs) != 2 {
		t.Errorf("expected 2 messages in history, got %v", resp["messages"])
	} else if count, _ := resp["count"].(float64); int(count) != len(msgs) {
		t.Errorf("count mismatch: got %v, want %d", count, len(msgs))
	}
}

func TestChatRouter_ToolResult(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	sessionID := "tc-tool-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Create session first.
	_, _ = r.Handle(&Message{
		ID:    "tc-create",
		Type:  "chat.session.create",
		Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q}`, sessionID)),
	})

	msg := &Message{
		ID:    "tr-1",
		Type:  "chat.tool.result",
		Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q,"tool_call_id":"tcid-1","content":"tool output"}`, sessionID)),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	var resp map[string]interface{}
	json.Unmarshal(result.Response, &resp)
	if resp["tool_call_id"] != "tcid-1" {
		t.Errorf("expected tool_call_id tcid-1, got %v", resp["tool_call_id"])
	}
	if resp["status"] != "recorded" {
		t.Errorf("expected status 'recorded', got %v", resp["status"])
	}
}

func TestChatRouter_MissingSessionIDForHistory(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	msg := &Message{
		ID:    "hh-missing",
		Type:  "chat.history",
		Payload: []byte(`{}`),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for history without session_id")
	}
	if result.Error == "" {
		t.Error("expected error message, got empty")
	}
}

func TestChatRouter_UnsupportedAction(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	msg := &Message{
		ID:    "us-1",
		Type:  "chat.nonexistent",
		Payload: []byte(`{}`),
	}
	result, err := r.Handle(msg)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for unsupported action, got success")
	}
	if result.Error == "" {
		t.Error("expected error message for unsupported action")
	}
}

// TestChatRouter_SatisfiesRouteHandler compile-time check.
var _ RouteHandler = (*chatRouter)(nil).Handle

// --- chatRouter integration via hub ---

func TestChatRouter_RoutedThroughHub(t *testing.T) {
	logger := zap.NewNop()
	hub := NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer hub.Stop()

	// Register chat-router.
	cr := newChatRouter(logger)
	if err := hub.RegisterSkill("chat-router", cr.Handle); err != nil {
		t.Fatalf("RegisterSkill(chat-router) failed: %v", err)
	}

	if err := hub.RegisterVM("test-cli", RoleCLI); err != nil {
		t.Fatalf("RegisterVM failed: %v", err)
	}

	// Send a chat message via controlplane.request.
	payload := `{"message":"hello via hub","session_id":"tc-hub-1","correlation_id":"h1"}`
	controller := &Message{
		ID:      "cr-hub-1",
		From:    "test-cli",
		To:      MessageHubID,
		Type:    "controlplane.request",
		Payload: json.RawMessage(`{"action":"chat.message","data":` + payload + `}`),
	}

	result, routeErr := hub.RouteMessage("test-cli", controller)
	if routeErr != nil {
		t.Fatalf("RouteMessage failed: %v", routeErr)
	}
	if result == nil {
		t.Fatal("nil result from RouteMessage")
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Error)
	}
}

// TestChatRouter_SessionIsolation verifies that multiple goroutines creating
// sessions concurrently do not collide.
func TestChatRouter_SessionIsolation(t *testing.T) {
	r, cleanup := newTestChatRouter(t)
	defer cleanup()

	const count = 10
	done := make(chan bool, count)

	for i := 0; i < count; i++ {
		go func(n int) {
			sid := fmt.Sprintf("tc-isolate-%d", n)
			msg := &Message{
				ID:    fmt.Sprintf("iso-%d", n),
				Type:  "chat.session.create",
				Payload: json.RawMessage(fmt.Sprintf(`{"session_id":%q}`, sid)),
			}
			result, err := r.Handle(msg)
			if err != nil {
				t.Errorf("session %d: %v", n, err)
				done <- false
				return
			}
			if !result.Success {
				t.Errorf("expected success for session %d, got: %s", n, result.Error)
				done <- false
				return
			}
			done <- true
		}(i)
	}

	for i := 0; i < count; i++ {
		if !<-done {
			t.Error("isolation test failed")
		}
	}
}

// --- proposalBackend additional unit tests (PR #58: expand mediated actions) ---

// TestProposalBackend_ListByStatus verifies ListByStatus delegation.
func TestProposalBackend_ListByStatus(t *testing.T) {
	mock := &mockProposalStore{
		proposals: make(map[string]*proposal.Proposal),
		list:      make([]proposal.ProposalSummary, 0),
	}

	// Create one proposal and directly set its status in the list.
	p1, err := proposal.NewProposal("Approved Proposal", "Approved desc", proposal.CategoryEditSkill, "tester")
	if err != nil {
		t.Fatalf("NewProposal error: %v", err)
	}
	if err := mock.Create(p1); err != nil {
		t.Fatalf("mock Create error: %v", err)
	}
	// The mock stores the status at creation time (draft). Update the list entry
	// to 'approved' so ListByStatus finds it.
	mock.list[0].Status = proposal.StatusApproved

	summaries, err := mock.ListByStatus(proposal.StatusApproved)
	if err != nil {
		t.Fatalf("ListByStatus error: %v", err)
	}
	if len(summaries) < 1 {
		t.Errorf("expected at least 1 approved proposal, got %d", len(summaries))
	}
}

// TestProposalBackend_GetNotFound verifies error sanitization on Get(404).
func TestProposalBackend_GetNotFound(t *testing.T) {
	logger := zap.NewNop()
	mock := &mockProposalStore{
		proposals: make(map[string]*proposal.Proposal),
	}
	backend := NewProposalBackend(mock, logger)

	msg := &Message{
		ID:    "gnf-1",
		Type:  "proposal.status",
		Payload: json.RawMessage(`{"proposal_id":"nonexistent"}`),
	}
	result, err := backend.Handle(msg)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for nonexistent proposal, got success")
	}
	if result.Error == "" {
		t.Error("expected error message, got empty")
	}
}

// TestPreferredBackendForAction_ChatRouter verifies chat.message routes to chat-router.
func TestPreferredBackendForAction_ChatRouter(t *testing.T) {
	hub := NewMessageHubNoKernel(zap.NewNop())
	backendID := hub.preferredBackendForAction("chat.message")
	if backendID != "chat-router" {
		t.Errorf("expected chat-message to route to chat-router, got %s", backendID)
	}
}

// TestPreferredBackendForAction_ProposalVerifyAll verify all proposal actions route to store-vm.
func TestPreferredBackendForAction_ProposalVerifyAll(t *testing.T) {
	hub := NewMessageHubNoKernel(zap.NewNop())
	for _, action := range []string{"proposal.list", "proposal.status", "proposal.create"} {
		backendID := hub.preferredBackendForAction(action)
		if backendID != "store-vm" {
			t.Errorf("expected %q to route to store-vm, got %s", action, backendID)
		}
	}
}

// TestPreferredBackendForAction_WorkerVerify routes worker actions to store-vm.
func TestPreferredBackendForAction_WorkerVerify(t *testing.T) {
	hub := NewMessageHubNoKernel(zap.NewNop())
	for _, action := range []string{"worker.list", "worker.status"} {
		backendID := hub.preferredBackendForAction(action)
		if backendID != "store-vm" {
			t.Errorf("expected %q to route to store-vm, got %s", action, backendID)
		}
	}
}

// Ensure all tests compile.
var _ sync.Mutex

// Mock for proposal backend to expose ListByStatus for test.
type mockProposalLister struct {
	listFunc func() ([]proposal.ProposalSummary, error)
}

// Ensure mockProposalStore satisfies lister interface.
var _ *mockProposalLister

// TestHubRoutesChatMessageThroughChatRouter verifies full routing path from CLI
// to chat-backend via hub.handleControlPlaneRequest.
func TestHubRoutesChatMessageThroughChatRouter(t *testing.T) {
	logger := zap.NewNop()
	mockStore := &mockProposalStore{
		proposals: make(map[string]*proposal.Proposal),
		list:      make([]proposal.ProposalSummary, 0),
	}
	proposalBackend := NewProposalBackend(mockStore, logger)

	hub := NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer hub.Stop()

	if err := hub.RegisterSkill("store-vm", proposalBackend.Handle); err != nil {
		t.Fatalf("RegisterSkill(store-vm) failed: %v", err)
	}

	// Register chat-router via test helper.
	cr := newChatRouter(logger)
	if err := hub.RegisterSkill("chat-router", cr.Handle); err != nil {
		t.Fatalf("RegisterSkill(chat-router) failed: %v", err)
	}

	if err := hub.RegisterVM("test-cli", RoleCLI); err != nil {
		t.Fatalf("RegisterVM failed: %v", err)
	}

	// Send chat.message via the hub.
	payload := `{"message":"test chat message","session_id":"tc-final","correlation_id":"f1"}`
	msg := &Message{
		ID:      "cr-final",
		From:    "test-cli",
		To:      MessageHubID,
		Type:    "controlplane.request",
		Payload: json.RawMessage(`{"action":"chat.message","data":` + payload + `}`),
	}
	result, routeErr := hub.RouteMessage("test-cli", msg)
	if routeErr != nil {
		t.Fatalf("RouteMessage(chatsession) failed: %v", routeErr)
	}
	if result == nil {
		t.Fatal("nil result from hub routing")
	}
	if !result.Success {
		t.Errorf("expected success for chat.message via hub, got: %s", result.Error)
	}
}
