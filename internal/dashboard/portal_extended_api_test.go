package dashboard

import (
	"context"
	"encoding/json"
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
		// Phase 1 contract: return shape with required windows + model breakdown + by_agent for per-agent
		return &APIResponse{Success: true, Data: json.RawMessage(`{"grand":{"calls":42,"tokens_prompt":1200,"tokens_completion":800,"tokens_total":2000,"by_model":{"qwen":2000}},"last_hour":{"calls":5,"tokens_prompt":100,"tokens_completion":80},"today":{"calls":20,"tokens_prompt":600,"tokens_completion":400},"mtd":{"calls":42,"tokens_prompt":1200,"tokens_completion":800},"models":{"qwen":2000},"record_count":42,"by_agent":{"coder-main":{"calls":30,"tokens_prompt":900,"tokens_completion":600,"tokens_total":1500,"by_model":{"qwen":1500}}}}`)} , nil
	case "llm.usage.record":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"ok":true}`)}, nil
	case "agent.settings.get", "agent.soul.get":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"agent":"` + "mock" + `","settings":{"model":"qwen"},"soul":"Be safe"}`)}, nil
	case "agent.settings.set", "agent.soul.set":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"ok":true}`)}, nil
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
	// Phase 1 contract test: /api/llm-usage returns required windows (grand/last_hour/today/mtd) + breakdowns.
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
	for _, key := range []string{"grand", "last_hour", "today", "mtd", "models"} {
		if _, ok := out[key]; !ok {
			t.Errorf("missing aggregate key %q", key)
		}
	}
}

func TestAPIAgentSettings_GetAndPost(t *testing.T) {
	// Coverage for Phase 2 per-agent settings portal surface (GET + confirmed POST via bridge).
	srv, _ := New("127.0.0.1:0", &extendedMockClient{})
	// GET
	req := httptest.NewRequest(http.MethodGet, "/api/agents/researcher/settings", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings status %d: %s", rec.Code, rec.Body.String())
	}
	var g map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &g)
	if g["agent"] != "researcher" {
		t.Error("expected agent id in response")
	}

	// POST without confirm should 428
	req = httptestRequest(t, http.MethodPost, "/api/agents/researcher/settings", strings.NewReader(`{"settings":{"model":"special"}}`))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("POST without confirm expected 428, got %d", rec.Code)
	}

	// with confirm
	req = httptestRequest(t, http.MethodPost, "/api/agents/researcher/settings", strings.NewReader(`{"settings":{"model":"special"}}`))
	req.Header.Set("X-Aegis-Confirmed", "1")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST with confirm status %d body=%s", rec.Code, rec.Body.String())
	}
}