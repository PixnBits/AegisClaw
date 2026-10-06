package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type extendedMockClient struct{}

func (m *extendedMockClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	switch action {
	case "worker.list":
		return &APIResponse{Success: true, Data: json.RawMessage(`[{"id":"w1","name":"researcher","status":"running","task":"Analyze","role":"researcher","progress":"50%"}]`)}, nil
	case "proposal.list":
		return &APIResponse{Success: true, Data: json.RawMessage(`[{"id":"p1","title":"Test","state":"pending"}]`)}, nil
	case "chat.tool_events":
		return &APIResponse{Success: true, Data: json.RawMessage(`[{"tool":"search","status":"success"}]`)}, nil
	case "chat.thought_events":
		return &APIResponse{Success: true, Data: json.RawMessage(`[{"description":"Thinking"}]`)}, nil
	case "proposal.approve":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"ok":true}`)}, nil
	case "llm.usage.summary":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"grand":{"calls":42,"tokens_prompt":1200,"tokens_completion":800,"tokens_total":2000,"by_model":{"qwen":2000}},"last_hour":{"calls":5,"tokens_prompt":100,"tokens_completion":80},"today":{"calls":20,"tokens_prompt":600,"tokens_completion":400},"mtd":{"calls":42,"tokens_prompt":1200,"tokens_completion":800},"models":{"qwen":2000},"record_count":42,"by_agent":{"coder-main":{"calls":30,"tokens_prompt":900,"tokens_completion":600,"tokens_total":1500,"by_model":{"qwen":1500}}}}`)}, nil
	default:
		return &APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
	}
}

func TestActiveWorkEndpoint(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &extendedMockClient{})
	req := httptest.NewRequest(http.MethodGet, "/api/active-work", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	items, ok := body["items"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected items: %v", body)
	}
}

func TestAgentTraceEndpoint(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &extendedMockClient{})
	req := httptest.NewRequest(http.MethodGet, "/api/agents/researcher/trace", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProposalApproveRequiresConfirmation(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &extendedMockClient{})
	req := httptestRequest(t, http.MethodPost, "/api/proposals/p1/approve", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("expected 428, got %d", rec.Code)
	}
}

func TestAPILLMUsage_ReturnsAggregatesShape(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &extendedMockClient{})
	req := httptest.NewRequest(http.MethodGet, "/api/llm-usage", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	for _, key := range []string{"grand", "last_hour", "today", "mtd", "models", "by_agent"} {
		if _, ok := out[key]; !ok {
			t.Errorf("missing aggregate key %q", key)
		}
	}
}

type llmUsageAPIClient struct {
	summary json.RawMessage
	recent  json.RawMessage
	err     error
	calls   []string
	payload map[string]json.RawMessage
}

func (c *llmUsageAPIClient) Call(_ context.Context, action string, payload json.RawMessage) (*APIResponse, error) {
	c.calls = append(c.calls, action)
	if c.payload == nil {
		c.payload = map[string]json.RawMessage{}
	}
	c.payload[action] = append(json.RawMessage(nil), payload...)
	if c.err != nil {
		return nil, c.err
	}
	data := c.summary
	if action == "llm.usage.recent" {
		data = c.recent
	}
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	return &APIResponse{Success: true, Data: data}, nil
}

func TestAPILLMUsage_RejectsBadAgentID(t *testing.T) {
	// Removing the agent_id check makes these return 200.
	bad := []string{
		"Coder",
		"1coder",
		"coder_1",
		"coder.1",
		"../etc",
		strings.Repeat("a", 65),
	}
	for _, id := range bad {
		client := &llmUsageAPIClient{summary: json.RawMessage(`{"grand":{}}`)}
		srv, _ := New("127.0.0.1:0", client)
		req := httptest.NewRequest(http.MethodGet, "/api/llm-usage?agent_id="+id, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("agent_id %q status %d, want 400", id, rec.Code)
		}
		if len(client.calls) != 0 {
			t.Errorf("agent_id %q reached the bridge", id)
		}
		req = httptest.NewRequest(http.MethodGet, "/api/llm-usage/recent?agent_id="+id, nil)
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("recent agent_id %q status %d, want 400", id, rec.Code)
		}
	}
}

func TestAPILLMUsage_AcceptsAgentIDAndClampsLimit(t *testing.T) {
	client := &llmUsageAPIClient{
		summary: json.RawMessage(`{"grand":{"calls":1},"record_count":1}`),
		recent:  json.RawMessage(`[{"agent_id":"coder-1","tokens_prompt":1}]`),
	}
	srv, _ := New("127.0.0.1:0", client)
	req := httptest.NewRequest(http.MethodGet, "/api/llm-usage?agent_id=coder-1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status %d body=%s", rec.Code, rec.Body.String())
	}
	var summary map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["agent_id"] != "coder-1" {
		t.Fatalf("summary agent_id %+v", summary["agent_id"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/llm-usage/recent?limit=9000&agent_id=coder-1", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("recent status %d body=%s", rec.Code, rec.Body.String())
	}
	var recent map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &recent); err != nil {
		t.Fatal(err)
	}
	if recent["limit"].(float64) != 500 {
		t.Fatalf("limit %+v, want 500", recent["limit"])
	}
	var sent map[string]interface{}
	if err := json.Unmarshal(client.payload["llm.usage.recent"], &sent); err != nil {
		t.Fatal(err)
	}
	if sent["limit"].(float64) != 500 || sent["agent_id"] != "coder-1" {
		t.Fatalf("bridge payload %+v", sent)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/llm-usage/recent?limit=0", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &recent); err != nil {
		t.Fatal(err)
	}
	if recent["limit"].(float64) != 100 {
		t.Fatalf("non-positive limit %+v, want default 100", recent["limit"])
	}
	req = httptest.NewRequest(http.MethodGet, "/api/llm-usage/recent", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &recent); err != nil {
		t.Fatal(err)
	}
	if recent["limit"].(float64) != 100 {
		t.Fatalf("default limit %+v", recent["limit"])
	}
}

func TestAPILLMUsage_MethodNotAllowed(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &llmUsageAPIClient{})
	for _, path := range []string{"/api/llm-usage", "/api/llm-usage/recent"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s status %d, want 405", path, rec.Code)
		}
	}
}

func TestAPILLMUsage_FetchErrorIsBadGateway(t *testing.T) {
	// handleAPISecurityPosture fails a single store read instead of returning an
	// empty shape. These endpoints do the same, with 502.
	client := &llmUsageAPIClient{err: errors.New("store down")}
	srv, _ := New("127.0.0.1:0", client)
	for _, path := range []string{"/api/llm-usage", "/api/llm-usage/recent"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Errorf("GET %s status %d body=%s, want 502", path, rec.Code, rec.Body.String())
		}
	}
}
