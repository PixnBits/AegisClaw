package exec_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/runtime/exec"
)

// ─── AgentTurnRequest / AgentTurnResponse JSON round-trip ─────────────────────

func TestAgentTurnRequest_JSONRoundTrip(t *testing.T) {
	want := exec.AgentTurnRequest{
		Messages: []exec.AgentMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Create a skill proposal."},
		},
		Model:            "llama3.2:3b",
		StreamID:         "stream-abc",
		StructuredOutput: true,
		TraceID:          "trace-001",
		Temperature:      0,
		Seed:             42,
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got exec.AgentTurnRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Messages) != len(want.Messages) {
		t.Fatalf("Messages len = %d, want %d", len(got.Messages), len(want.Messages))
	}
	for i, wm := range want.Messages {
		gm := got.Messages[i]
		if gm.Role != wm.Role || gm.Content != wm.Content {
			t.Errorf("Messages[%d] = {%q, %q}, want {%q, %q}", i, gm.Role, gm.Content, wm.Role, wm.Content)
		}
	}
	if got.Model != want.Model {
		t.Errorf("Model = %q, want %q", got.Model, want.Model)
	}
	if got.StreamID != want.StreamID {
		t.Errorf("StreamID = %q, want %q", got.StreamID, want.StreamID)
	}
	if got.StructuredOutput != want.StructuredOutput {
		t.Errorf("StructuredOutput = %v, want %v", got.StructuredOutput, want.StructuredOutput)
	}
	if got.TraceID != want.TraceID {
		t.Errorf("TraceID = %q, want %q", got.TraceID, want.TraceID)
	}
	if got.Seed != want.Seed {
		t.Errorf("Seed = %d, want %d", got.Seed, want.Seed)
	}
}

// Temperature=0 remains a valid request value even though the public request
// type omits it from direct JSON marshaling.
func TestAgentTurnRequest_ZeroTemperaturePreserved(t *testing.T) {
	req := exec.AgentTurnRequest{
		Messages:    []exec.AgentMessage{{Role: "user", Content: "hello"}},
		Temperature: 0, // explicitly zero — omitempty means it is absent from JSON,
		// so the guest-agent uses its model default.  This is the documented
		// "don't add entropy" convention; setting Seed provides an extra
		// determinism hint without requiring a specific temperature wire value.
	}
	data, _ := json.Marshal(req)
	var m map[string]interface{}
	json.Unmarshal(data, &m) //nolint:errcheck

	// Temperature=0 with omitempty is still absent from the public request JSON.
	// The Firecracker executor explicitly includes zero when a deterministic seed
	// is present so live test payloads can force temperature=0 without changing
	// production defaults.
	_, present := m["temperature"]
	if present {
		t.Error("temperature=0 should be omitted from the public AgentTurnRequest JSON")
	}
}

func TestAgentTurnResponse_JSONRoundTrip(t *testing.T) {
	cases := []exec.AgentTurnResponse{
		{Status: "final", Content: "The proposal has been created."},
		{Status: "tool_call", Tool: "proposal.create_draft", Args: `{"title":"test"}`},
		{Status: "final", Content: "Done.", Thinking: "I reasoned through this carefully."},
	}

	for _, want := range cases {
		data, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal %+v: %v", want, err)
		}
		var got exec.AgentTurnResponse
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Status != want.Status {
			t.Errorf("Status = %q, want %q", got.Status, want.Status)
		}
		if got.Content != want.Content {
			t.Errorf("Content = %q, want %q", got.Content, want.Content)
		}
		if got.Tool != want.Tool {
			t.Errorf("Tool = %q, want %q", got.Tool, want.Tool)
		}
		if got.Args != want.Args {
			t.Errorf("Args = %q, want %q", got.Args, want.Args)
		}
		if got.Thinking != want.Thinking {
			t.Errorf("Thinking = %q, want %q", got.Thinking, want.Thinking)
		}
	}
}

// ─── FirecrackerTaskExecutor ───────────────────────────────────────────────────

// stubVMRuntime implements VMRuntime for unit tests.  It returns a hard-coded
// response or an error, depending on how it is configured.
type stubVMRuntime struct {
	// response is returned as-is when err is nil.
	response json.RawMessage
	// err, if non-nil, is returned instead of response.
	err error
	// lastReq captures the last request for assertion.
	lastReq interface{}
}

func (s *stubVMRuntime) SendToVM(_ context.Context, _ string, req interface{}) (json.RawMessage, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

// buildVMResponse constructs a well-formed agentVMResponse JSON that wraps the
// given chat response data.
func buildVMResponse(t *testing.T, success bool, errMsg string, chatResp interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(chatResp)
	if err != nil {
		t.Fatalf("marshal chat resp: %v", err)
	}
	type vmResp struct {
		ID      string          `json:"id"`
		Success bool            `json:"success"`
		Error   string          `json:"error,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
	}
	out, _ := json.Marshal(vmResp{ID: "test-id", Success: success, Error: errMsg, Data: data})
	return out
}

func TestFirecrackerTaskExecutor_FinalResponse(t *testing.T) {
	stub := &stubVMRuntime{
		response: buildVMResponse(t, true, "", map[string]interface{}{
			"status":  "final",
			"content": "I have completed the task.",
		}),
	}

	ex := exec.NewFirecrackerTaskExecutor(stub, "test-vm")
	resp, err := ex.ExecuteTurn(context.Background(), exec.AgentTurnRequest{
		Messages:    []exec.AgentMessage{{Role: "user", Content: "hello"}},
		Model:       "llama3.2:3b",
		TraceID:     "test-trace-123",
		Temperature: 0,
		Seed:        42,
	})
	if err != nil {
		t.Fatalf("ExecuteTurn: %v", err)
	}
	if resp.Status != "final" {
		t.Errorf("Status = %q, want 'final'", resp.Status)
	}
	if resp.Content != "I have completed the task." {
		t.Errorf("Content = %q, want 'I have completed the task.'", resp.Content)
	}
}

func TestFirecrackerTaskExecutor_ToolCallResponse(t *testing.T) {
	stub := &stubVMRuntime{
		response: buildVMResponse(t, true, "", map[string]interface{}{
			"status": "tool_call",
			"tool":   "proposal.create_draft",
			"args":   `{"title":"My Skill"}`,
		}),
	}

	ex := exec.NewFirecrackerTaskExecutor(stub, "agent-vm")
	resp, err := ex.ExecuteTurn(context.Background(), exec.AgentTurnRequest{
		Messages: []exec.AgentMessage{{Role: "user", Content: "create a skill"}},
	})
	if err != nil {
		t.Fatalf("ExecuteTurn: %v", err)
	}
	if resp.Status != "tool_call" {
		t.Errorf("Status = %q, want 'tool_call'", resp.Status)
	}
	if resp.Tool != "proposal.create_draft" {
		t.Errorf("Tool = %q, want 'proposal.create_draft'", resp.Tool)
	}
	if resp.Args != `{"title":"My Skill"}` {
		t.Errorf("Args = %q, want '{\"title\":\"My Skill\"}'", resp.Args)
	}
}

func TestFirecrackerTaskExecutor_VMTransportError(t *testing.T) {
	stub := &stubVMRuntime{err: errors.New("vsock connection refused")}

	ex := exec.NewFirecrackerTaskExecutor(stub, "agent-vm")
	_, err := ex.ExecuteTurn(context.Background(), exec.AgentTurnRequest{
		Messages: []exec.AgentMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error on VM transport failure, got nil")
	}
	if !contains(err.Error(), "vsock connection refused") {
		t.Errorf("error should mention vsock: %v", err)
	}
}

func TestFirecrackerTaskExecutor_AgentError(t *testing.T) {
	// VM transport succeeds but the agent returns success=false.
	stub := &stubVMRuntime{
		response: buildVMResponse(t, false, "out of context window", nil),
	}

	ex := exec.NewFirecrackerTaskExecutor(stub, "agent-vm")
	_, err := ex.ExecuteTurn(context.Background(), exec.AgentTurnRequest{
		Messages: []exec.AgentMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for agent error response, got nil")
	}
	if !contains(err.Error(), "out of context window") {
		t.Errorf("error should mention agent message: %v", err)
	}
}

func TestFirecrackerTaskExecutor_MalformedVMResponse(t *testing.T) {
	stub := &stubVMRuntime{response: json.RawMessage(`not-valid-json`)}

	ex := exec.NewFirecrackerTaskExecutor(stub, "agent-vm")
	_, err := ex.ExecuteTurn(context.Background(), exec.AgentTurnRequest{
		Messages: []exec.AgentMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for malformed VM response, got nil")
	}
}

// TestFirecrackerTaskExecutor_TemperatureAndSeedPropagated verifies that
// Temperature and Seed are included in the payload sent to the VM when set.
func TestFirecrackerTaskExecutor_TemperatureAndSeedPropagated(t *testing.T) {
	stub := &stubVMRuntime{
		response: buildVMResponse(t, true, "", map[string]interface{}{
			"status":  "final",
			"content": "ok",
		}),
	}

	ex := exec.NewFirecrackerTaskExecutor(stub, "agent-vm")
	_, err := ex.ExecuteTurn(context.Background(), exec.AgentTurnRequest{
		Messages:    []exec.AgentMessage{{Role: "user", Content: "hello"}},
		Temperature: 0.7,
		Seed:        12345,
	})
	if err != nil {
		t.Fatalf("ExecuteTurn: %v", err)
	}

	// The lastReq is the agentVMRequest envelope — decode and check the inner payload.
	data, err := json.Marshal(stub.lastReq)
	if err != nil {
		t.Fatalf("marshal lastReq: %v", err)
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var chatPayload struct {
		Temperature float64 `json:"temperature"`
		Seed        int64   `json:"seed"`
	}
	if err := json.Unmarshal(envelope.Payload, &chatPayload); err != nil {
		t.Fatalf("unmarshal chat payload: %v", err)
	}
	if chatPayload.Temperature != 0.7 {
		t.Errorf("payload temperature = %v, want 0.7", chatPayload.Temperature)
	}
	if chatPayload.Seed != 12345 {
		t.Errorf("payload seed = %d, want 12345", chatPayload.Seed)
	}
}

func TestFirecrackerTaskExecutor_ZeroTemperaturePropagatedWhenSeedSet(t *testing.T) {
	stub := &stubVMRuntime{
		response: buildVMResponse(t, true, "", map[string]interface{}{
			"status":  "final",
			"content": "ok",
		}),
	}

	ex := exec.NewFirecrackerTaskExecutor(stub, "agent-vm")
	_, err := ex.ExecuteTurn(context.Background(), exec.AgentTurnRequest{
		Messages:    []exec.AgentMessage{{Role: "user", Content: "hello"}},
		Temperature: 0,
		Seed:        42,
	})
	if err != nil {
		t.Fatalf("ExecuteTurn: %v", err)
	}

	data, err := json.Marshal(stub.lastReq)
	if err != nil {
		t.Fatalf("marshal lastReq: %v", err)
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var chatPayload map[string]interface{}
	if err := json.Unmarshal(envelope.Payload, &chatPayload); err != nil {
		t.Fatalf("unmarshal chat payload: %v", err)
	}
	temp, present := chatPayload["temperature"]
	if !present {
		t.Fatal("payload temperature missing; zero temperature should be forwarded when seed is set")
	}
	if temp.(float64) != 0 {
		t.Errorf("payload temperature = %v, want 0", temp)
	}
	if chatPayload["seed"].(float64) != 42 {
		t.Errorf("payload seed = %v, want 42", chatPayload["seed"])
	}
}

// TestFirecrackerTaskExecutor_ConcurrentSafe exercises the executor under
// concurrent calls to verify there are no data races.
func TestFirecrackerTaskExecutor_ConcurrentSafe(t *testing.T) {
	stub := &stubVMRuntime{
		response: buildVMResponse(t, true, "", map[string]interface{}{
			"status":  "final",
			"content": "ok",
		}),
	}
	ex := exec.NewFirecrackerTaskExecutor(stub, "agent-vm")

	const goroutines = 10
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := ex.ExecuteTurn(context.Background(), exec.AgentTurnRequest{
				Messages: []exec.AgentMessage{{Role: "user", Content: "hello"}},
			})
			errs <- err
		}()
	}
	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent ExecuteTurn: %v", err)
		}
	}
}

// ─── VMRuntime interface contract ─────────────────────────────────────────────

// TestVMRuntimeInterface verifies that a hypothetical HTTP-based stub satisfies
// the VMRuntime interface, ensuring the interface remains stable.
func TestVMRuntimeInterface(t *testing.T) {
	// Create a test HTTP server that returns a valid VM response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"id":      "test",
			"success": true,
			"data":    json.RawMessage(`{"status":"final","content":"ok"}`),
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	// Verify the stub compiles as a VMRuntime.
	var _ exec.VMRuntime = &stubVMRuntime{}

	// A real implementation would call the HTTP server; here we just verify
	// the interface is correctly defined.
	_ = fmt.Sprintf("server at %s", srv.URL)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
