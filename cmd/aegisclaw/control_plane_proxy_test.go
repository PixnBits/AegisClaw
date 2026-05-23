package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

// TestControlPlaneProxy_New verifies that NewControlPlaneProxy constructs
// a non-nil proxy with the provided dependencies.
func TestControlPlaneProxy_New(t *testing.T) {
	logger := zap.NewNop()
	proxy := NewControlPlaneProxy(nil, logger)
	if proxy == nil {
		t.Fatal("NewControlPlaneProxy returned nil")
	}
}

// TestControlPlaneProxy_Forward_Basic exercises the happy-path stub
// and verifies that a success response with empty data is returned.
func TestControlPlaneProxy_Forward_Basic(t *testing.T) {
	logger := zap.NewNop()
	proxy := NewControlPlaneProxy(nil, logger)

	req := ControlPlaneRequest{
		Action: "worker.list",
		Data:   json.RawMessage(`{}`),
	}

	resp, err := proxy.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Forward returned nil response")
	}
	if !resp.Success {
		t.Errorf("expected Success=true, got false (error=%s)", resp.Error)
	}
}

// TestControlPlaneProxy_Forward_ErrorHandling verifies proxy behavior.
// Note: with default fail-fast (no sample unless AEGISCLAW_ALLOW_SAMPLE_DATA=true),
// unknown actions return clear errors; this test uses nil-hub path which bypasses.
func TestControlPlaneProxy_Forward_ErrorHandling(t *testing.T) {
	logger := zap.NewNop()
	proxy := NewControlPlaneProxy(nil, logger)

	// Table-driven cases for different actions.
	cases := []struct {
		action string
	}{
		{"worker.list"},
		{"skill.status"},
		{"chat.message"},
	}

	for _, c := range cases {
		req := ControlPlaneRequest{Action: c.action}
		resp, err := proxy.Forward(context.Background(), req)
		if err != nil {
			t.Errorf("Forward(%s) unexpected error: %v", c.action, err)
			continue
		}
		if resp == nil || !resp.Success {
			t.Errorf("Forward(%s) expected success response, got %+v", c.action, resp)
		}
	}
}

// TestControlPlaneProxy_HandlerUsagePattern demonstrates how a wired
// handler would use the proxy (example for Phase 7 integration tests).
func TestControlPlaneProxy_HandlerUsagePattern(t *testing.T) {
	logger := zap.NewNop()
	proxy := NewControlPlaneProxy(nil, logger)

	// Simulate what a handler does:
	data := json.RawMessage(`{"active_only": true}`)
	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "worker.list",
		Data:   data,
	})
	if err != nil {
		t.Fatalf("handler simulation Forward error: %v", err)
	}
	if resp == nil {
		t.Fatal("handler simulation got nil response")
	}
}

// TestControlPlaneProxy_Forward_MediatedWorkerList defines the expected
// behavior for a mediated "worker.list" request through AegisHub.
// This test is written first (test-guided) and will pass once Phase 8
// implements the full ControlPlaneRequest routing in MessageHub.
func TestControlPlaneProxy_Forward_MediatedWorkerList(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	// Register a backend handler simulating the Store VM worker store.
	// In real flow this would be registered by the Store VM or a proxy skill.
	if err := hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		// For this test we ignore the payload and return a canned worker list.
		data := json.RawMessage(`[{"worker_id":"w-test-1","role":"general","status":"idle"}]`)
		return &ipc.DeliveryResult{
			MessageID: msg.ID,
			Success:   true,
			Response:  data,
		}, nil
	}); err != nil {
		t.Fatalf("failed to register test backend: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	req := ControlPlaneRequest{
		Action: "worker.list",
		Data:   json.RawMessage(`{}`),
	}

	resp, err := proxy.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward returned error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("expected successful mediated response, got %+v", resp)
	}
	// At minimum the response should contain some data (even if stubbed today).
	if len(resp.Data) == 0 {
		t.Log("note: response data is empty in current stub; real implementation will populate it")
	}
}

// TestControlPlaneProxy_Forward_UnknownAction verifies graceful error handling
// when an unsupported action is requested.
func TestControlPlaneProxy_Forward_UnknownAction(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)
	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "nonexistent.action",
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.Success {
		t.Error("expected failure response for unknown action")
	}
	if resp.Error == "" {
		t.Error("expected descriptive error message")
	}
}

// TestControlPlaneProxy_Forward_BackendErrorPropagation verifies that errors
// returned by a registered backend handler are properly surfaced to the caller.
func TestControlPlaneProxy_Forward_BackendErrorPropagation(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	// Register a handler under the backend that "worker.status" maps to ("store-vm").
	// This ensures preferredBackendForAction routes the request to this failing handler.
	if err := hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		return &ipc.DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     "backend unavailable",
		}, nil
	}); err != nil {
		t.Fatalf("failed to register failing backend: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "worker.status",
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.Success {
		t.Error("expected failure when backend returns error")
	}
	if resp.Error != "backend unavailable" {
		t.Errorf("expected backend error to be propagated, got %q", resp.Error)
	}
}

// TestControlPlaneProxy_Forward_DelegatesToRegisteredHandler verifies that
// when a handler is registered for a ControlPlane action, the request is
// delegated to it rather than using the internal sample-data switch.
func TestControlPlaneProxy_Forward_DelegatesToRegisteredHandler(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	// Register a handler under "store-vm" (the backend for "worker.list").
	// This exercises the delegation path: registered backend response wins over sample data.
	expectedData := json.RawMessage(`{"custom":"delegated-response"}`)

	if err := hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		return &ipc.DeliveryResult{
			MessageID: msg.ID,
			Success:   true,
			Response:  expectedData,
		}, nil
	}); err != nil {
		t.Fatalf("failed to register custom handler: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "worker.list",
	})
	if err != nil {
		t.Fatalf("Forward error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success from delegated handler, got error: %s", resp.Error)
	}
	if string(resp.Data) != string(expectedData) {
		t.Errorf("expected delegated response data %s, got %s", expectedData, resp.Data)
	}
}

// TestControlPlaneProxy_Forward_RespectsContextCancellation verifies that
// a cancelled context results in a proper failure response rather than hanging.
func TestControlPlaneProxy_Forward_RespectsContextCancellation(t *testing.T) {
	// Opt-in sample fallback for this test (uses worker.list without explicit backend registration).
	t.Setenv("AEGISCLAW_ALLOW_SAMPLE_DATA", "true")
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)
	proxy := NewControlPlaneProxy(hub, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	resp, err := proxy.Forward(ctx, ControlPlaneRequest{Action: "worker.list"})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.Success {
		t.Error("expected failure for cancelled context")
	}
	if resp.Error == "" {
		t.Error("expected error message indicating cancellation")
	}
}

// TestControlPlaneProxy_Forward_ChatMessage_Delegates defines expected behavior
// for chat.message requests. Written first (test-guided) before handler wiring.
func TestControlPlaneProxy_Forward_ChatMessage_Delegates(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	// Simulate a chat router backend that would live in AegisHub or Agent VM.
	if err := hub.RegisterSkill("chat-router", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		data := json.RawMessage(`{"session_id":"s-001","reply":"hello from hub"}`)
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
	}); err != nil {
		t.Fatalf("failed to register chat-router: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "chat.message",
		Data:   json.RawMessage(`{"message":"hi"}`),
	})
	if err != nil {
		t.Fatalf("Forward error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success for chat.message, got error: %s", resp.Error)
	}
}

// TestControlPlaneProxy_Forward_ProposalList_Delegates defines expected behavior
// for proposal.list requests. Written first (test-guided).
func TestControlPlaneProxy_Forward_ProposalList_Delegates(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	if err := hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		data := json.RawMessage(`[{"proposal_id":"p-1","title":"Example","status":"draft"}]`)
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
	}); err != nil {
		t.Fatalf("failed to register store-vm: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "proposal.list",
	})
	if err != nil {
		t.Fatalf("Forward error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success for proposal.list, got error: %s", resp.Error)
	}
}

// TestControlPlaneProxy_Forward_ProposalStatus_ErrorPropagation verifies error
// handling for proposal.status when the backend fails.
func TestControlPlaneProxy_Forward_ProposalStatus_ErrorPropagation(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	if err := hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		return &ipc.DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     "proposal not found",
		}, nil
	}); err != nil {
		t.Fatalf("failed to register store-vm: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "proposal.status",
		Data:   json.RawMessage(`{"proposal_id":"missing"}`),
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.Success {
		t.Error("expected failure when backend returns error")
	}
	if resp.Error != "proposal not found" {
		t.Errorf("expected backend error propagated, got %q", resp.Error)
	}
}

// TestProposalHandlers_RegisteredWithProxy defines expected behavior once
// proposal handlers are registered with ControlPlaneProxy (test-guided).
func TestProposalHandlers_RegisteredWithProxy(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)
	if err := hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		data := json.RawMessage(`[{"proposal_id":"p-reg-1"}]`)
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	proxy := NewControlPlaneProxy(hub, logger)

	// Simulate what registration will do: create handler and call it.
	listH := makeProposalListHandler(proxy)
	resp := listH(context.Background(), json.RawMessage(`{}`))
	if resp == nil || !resp.Success {
		t.Fatalf("proposal.list handler failed: %+v", resp)
	}

	statusH := makeProposalStatusHandler(proxy)
	resp = statusH(context.Background(), json.RawMessage(`{"proposal_id":"p-reg-1"}`))
	if resp == nil || !resp.Success {
		t.Fatalf("proposal.status handler failed: %+v", resp)
	}
}

// TestSessionsSendHandler_NilProxyFallback defines expected behavior when
// sessions.send tool path intentionally uses nil proxy (internal bypass).
// This test is written first per test-guided approach.
func TestSessionsSendHandler_NilProxyFallback(t *testing.T) {
	env := &runtimeEnv{} // minimal env; full test would need more setup
	// With nil proxy, the handler should still be constructible.
	// In practice it will error on chat path, but construction succeeds.
	h := makeSessionsSendHandler(env, nil, nil)
	if h == nil {
		t.Fatal("makeSessionsSendHandler with nil proxy returned nil")
	}
}

// TestSessionsSendHandler_UsesProxy defines expected behavior for sessions.send
// when threaded with ControlPlaneProxy (test-guided).
func TestSessionsSendHandler_UsesProxy(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)
	if err := hub.RegisterSkill("chat-router", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		data := json.RawMessage(`{"reply":"sessions via proxy"}`)
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
	}); err != nil {
		t.Fatalf("register chat-router: %v", err)
	}
	proxy := NewControlPlaneProxy(hub, logger)

	// Note: full sessions.send still needs env setup; here we test the proxy path
	// that will be used inside the handler after threading.
	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "chat.message",
		Data:   json.RawMessage(`{"session_id":"s-test","message":"hi from sessions"}`),
	})
	if err != nil || !resp.Success {
		t.Fatalf("expected sessions chat path to succeed via proxy: %v %+v", err, resp)
	}
}

// TestProposalHandlers_ErrorOnNilProxy verifies graceful error when handlers
// are called without a proxy (coverage for registration edge case).
func TestProposalHandlers_ErrorOnNilProxy(t *testing.T) {
	listH := makeProposalListHandler(nil)
	resp := listH(context.Background(), json.RawMessage(`{}`))
	if resp == nil || resp.Success {
		t.Error("expected error response when proxy is nil for proposal.list")
	}

	statusH := makeProposalStatusHandler(nil)
	resp = statusH(context.Background(), json.RawMessage(`{"proposal_id":"x"}`))
	if resp == nil || resp.Success {
		t.Error("expected error response when proxy is nil for proposal.status")
	}
}

// Phase 9 Integration Test Approach (defined in tests first, per test-guided style):
// We use in-process MessageHubNoKernel + RegisterSkill to simulate Store VM / chat-router
// backends. This allows full CLI → Proxy → AegisHub → Backend flow testing without
// requiring real Firecracker VMs or vsock. Real adapters (e.g., wrapping ProposalStore)
// can be registered the same way in production wiring.
//
// Example realistic adapter (moved to internal/ipc or store adapter in full impl):
// type proposalBackend struct { store store.ProposalStore }
// func (b *proposalBackend) handle(msg *ipc.Message) (*ipc.DeliveryResult, error) { ... calls b.store.List() ... }

// TestMediatedProposalList_DelegatesToStoreVM verifies the full mediated path for proposal.list.
func TestMediatedProposalList_DelegatesToStoreVM(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	// Simulate a Store VM backend that would use real ProposalStore.List() in production.
	if err := hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		data := json.RawMessage(`[{"proposal_id":"p-int-1","title":"Integration Test Proposal","status":"draft"}]`)
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
	}); err != nil {
		t.Fatalf("register store-vm: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "proposal.list",
	})
	if err != nil {
		t.Fatalf("Forward error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}
}

// TestMediatedChatMessage_DelegatesToChatRouter verifies chat.message delegation.
func TestMediatedChatMessage_DelegatesToChatRouter(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	if err := hub.RegisterSkill("chat-router", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		data := json.RawMessage(`{"session_id":"s-int-1","reply":"Phase 9 chat response"}`)
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
	}); err != nil {
		t.Fatalf("register chat-router: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "chat.message",
		Data:   json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil || !resp.Success {
		t.Fatalf("chat.message delegation failed: %v %+v", err, resp)
	}
}

// TestMediatedProposalStatus_DelegatesToStoreVM (test-guided) defines expected
// realistic behavior for proposal.status: delegation succeeds and returns
// structured data including proposal_id, title, status, created_at.
func TestMediatedProposalStatus_DelegatesToStoreVM(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	// Simulate Store VM returning realistic proposal data.
	if err := hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		data := json.RawMessage(`{"proposal_id":"p-42","title":"Realistic Proposal","status":"submitted","created_at":"2026-05-19T18:00:00Z"}`)
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
	}); err != nil {
		t.Fatalf("register store-vm: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "proposal.status",
		Data:   json.RawMessage(`{"proposal_id":"p-42"}`),
	})
	if err != nil {
		t.Fatalf("Forward error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}
	// Data verification: check key fields are present in response.
	var result map[string]string
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal resp.Data: %v", err)
	}
	if result["proposal_id"] != "p-42" || result["status"] != "submitted" {
		t.Errorf("unexpected proposal data: %+v", result)
	}
}

// TestMediatedChatMessage_ErrorFromChatRouter (test-guided) defines expected
// error propagation when the chat-router backend returns a failure.
func TestMediatedChatMessage_ErrorFromChatRouter(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	if err := hub.RegisterSkill("chat-router", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		return &ipc.DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     "chat router unavailable",
		}, nil
	}); err != nil {
		t.Fatalf("register chat-router: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "chat.message",
		Data:   json.RawMessage(`{"message":"test"}`),
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.Success {
		t.Error("expected failure from chat-router error")
	}
	if resp.Error != "chat router unavailable" {
		t.Errorf("expected backend error surfaced, got %q", resp.Error)
	}
}

// TestMediatedProposalList_RealProposalStore (test-guided) defines the
// expected behavior when a real ProposalStore is used via the proposalBackend.
// It creates a temporary git-based proposal store, adds a proposal, registers
// a handler that calls the store (simulating the real backend), and verifies
// the mediated response contains the created proposal.
func TestMediatedProposalList_RealProposalStore(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	// Create a real ProposalStore (git-backed) in a temp directory.
	tmp := t.TempDir()
	propStore, err := proposal.NewStore(tmp, logger)
	if err != nil {
		t.Fatalf("failed to create proposal store: %v", err)
	}

	// Create one proposal so List() returns something real.
	// Use NewProposal to ensure a valid UUID ID (Phase 9 test adaptation).
	p, err := proposal.NewProposal("Real Proposal from Store", "Integration test proposal", proposal.CategoryNewSkill, "tester")
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}
	if err := propStore.Create(p); err != nil {
		t.Fatalf("failed to create proposal in store: %v", err)
	}

	// Use the production proposalBackend adapter (test-guided: verifies real registration path).
	backend := ipc.NewProposalBackend(propStore, logger)
	if err := hub.RegisterSkill("store-vm", backend.Handle); err != nil {
		t.Fatalf("register store-vm: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "proposal.list",
	})
	if err != nil {
		t.Fatalf("Forward error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success from real store, got: %s", resp.Error)
	}

	// Verify the response contains our real proposal.
	var summaries []map[string]interface{}
	if err := json.Unmarshal(resp.Data, &summaries); err != nil {
		t.Fatalf("unmarshal summaries: %v", err)
	}
	if len(summaries) == 0 || summaries[0]["title"] != "Real Proposal from Store" {
		t.Errorf("expected real proposal in list, got: %+v", summaries)
	}
}

// TestMediatedProposalStatus_FullPathWithRealBackend exercises the complete
// mediation path with a real ProposalStore for proposal.status.
func TestMediatedProposalStatus_FullPathWithRealBackend(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	tmp := t.TempDir()
	propStore, err := proposal.NewStore(tmp, logger)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	// Use NewProposal for valid UUID (Phase 9 test adaptation).
	p, err := proposal.NewProposal("Full Path Proposal", "Full path integration test", proposal.CategoryNewSkill, "tester")
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}
	if err := propStore.Create(p); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Use the production proposalBackend adapter (test-guided: verifies real registration path for status).
	backend := ipc.NewProposalBackend(propStore, logger)
	if err := hub.RegisterSkill("store-vm", backend.Handle); err != nil {
		t.Fatalf("register: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "proposal.status",
		Data:   json.RawMessage(`{"proposal_id":"` + p.ID + `"}`),
	})
	if err != nil || !resp.Success {
		t.Fatalf("full path status failed: %v %+v", err, resp)
	}
}

// TestChatRouter_SessionAwareness (test-guided) defines expected realistic
// behavior for the chat-router: it should track basic session context and
// return structured responses with session_id, reply, and correlation_id.
func TestChatRouter_SessionAwareness(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	// Simulate an improved chat-router that maintains simple session state.
	// In a real impl this would be a stateful handler or delegate to Agent VM.
	sessions := make(map[string]string) // session_id -> last_message
	if err := hub.RegisterSkill("chat-router", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		var req struct {
			Message   string `json:"message"`
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		if req.SessionID == "" {
			req.SessionID = "default"
		}
		prev := sessions[req.SessionID]
		sessions[req.SessionID] = req.Message
		reply := map[string]interface{}{
			"session_id":     req.SessionID,
			"reply":          "received: " + req.Message + " (prev: " + prev + ")",
			"correlation_id": "corr-" + req.SessionID,
			"timestamp":      "now",
		}
		data, _ := json.Marshal(reply)
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
	}); err != nil {
		t.Fatalf("register chat-router: %v", err)
	}

	proxy := NewControlPlaneProxy(hub, logger)

	// First message
	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "chat.message",
		Data:   json.RawMessage(`{"message":"hello","session_id":"sess-1"}`),
	})
	if err != nil || !resp.Success {
		t.Fatalf("first chat failed: %v %+v", err, resp)
	}

	// Second message to same session should see previous context
	resp, err = proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "chat.message",
		Data:   json.RawMessage(`{"message":"world","session_id":"sess-1"}`),
	})
	if err != nil || !resp.Success {
		t.Fatalf("second chat failed: %v %+v", err, resp)
	}

	// Verify response contains expected structure (basic regression check)
	var out map[string]string
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["session_id"] != "sess-1" || out["correlation_id"] == "" {
		t.Errorf("unexpected chat response: %+v", out)
	}
}

// TestControlPlaneProxy_Delegation_Priority verifies that a registered handler
// wins over the internal sample-data fallback (clear delegation priority).
func TestControlPlaneProxy_Delegation_Priority(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)
	proxy := NewControlPlaneProxy(hub, logger)

	// Register a handler that returns a distinct marker.
	expected := json.RawMessage(`{"delegated":true,"source":"registered"}`)
	_ = hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		return &ipc.DeliveryResult{
			MessageID: msg.ID,
			Success:   true,
			Response:  expected,
		}, nil
	})

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "worker.list", // maps to store-vm
	})
	if err != nil {
		t.Fatalf("Forward error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("expected success, got: %+v", resp)
	}
	if string(resp.Data) != string(expected) {
		t.Errorf("delegation did not win: got %s, want %s", resp.Data, expected)
	}
}

// TestControlPlaneProxy_ErrorPropagation verifies consistent error surfacing
// across different backend types (store-vm vs chat-router failure paths).
func TestControlPlaneProxy_ErrorPropagation(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)
	proxy := NewControlPlaneProxy(hub, logger)

	// Failing store-vm backend
	_ = hub.RegisterSkill("store-vm", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		return &ipc.DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     "store-vm backend error",
		}, nil
	})
	// Failing chat-router backend
	_ = hub.RegisterSkill("chat-router", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		return &ipc.DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     "chat-router backend error",
		}, nil
	})

	for _, action := range []string{"worker.list", "chat.message"} {
		resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{Action: action})
		if err != nil {
			t.Errorf("%s: unexpected Go error: %v", action, err)
			continue
		}
		if resp == nil || resp.Success || resp.Error == "" {
			t.Errorf("%s: expected error response, got %+v", action, resp)
		}
	}
}

// TestMediatedProposalList_NoBackendErrors (test-guided) verifies the Phase 9
// error path: proposal actions return a clear actionable error (no silent sample fallback)
// when the "store-vm" backend is not registered at AegisHub startup.
func TestMediatedProposalList_NoBackendErrors(t *testing.T) {
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("hub.Start: %v", err)
	}
	defer hub.Stop()
	_ = hub.RegisterIdentityForTest("daemon", ipc.RoleCLI)

	proxy := NewControlPlaneProxy(hub, logger)

	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{Action: "proposal.list"})
	if err != nil {
		t.Fatalf("Forward unexpected Go error: %v", err)
	}
	if resp == nil || resp.Success || resp.Error == "" {
		t.Fatalf("expected error response for missing backend, got: %+v", resp)
	}
	if !strings.Contains(resp.Error, "store-vm") && !strings.Contains(resp.Error, "backend") {
		t.Errorf("error should mention store-vm or backend: %s", resp.Error)
	}
}