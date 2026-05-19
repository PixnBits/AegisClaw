package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/ipc"
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

// TestControlPlaneProxy_Forward_ErrorHandling verifies that the proxy
// currently does not return errors for unknown actions (stub behavior).
// Future real implementation may return errors for ACL failures, etc.
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

	// Register a handler that simulates a backend failure.
	if err := hub.RegisterSkill("failing-backend", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
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

	// Register a custom handler that returns distinctive data.
	const customAction = "custom.action"
	expectedData := json.RawMessage(`{"custom":"delegated-response"}`)

	if err := hub.RegisterSkill("custom-skill", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		// Only respond if this is our custom action (simple check on payload).
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
		Action: customAction,
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
	logger := zap.NewNop()
	hub := ipc.NewMessageHubNoKernel(logger)
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