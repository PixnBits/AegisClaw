package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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