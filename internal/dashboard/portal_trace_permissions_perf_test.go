package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type countingBridgeClient struct {
	calls atomic.Int32
}

func (c *countingBridgeClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	c.calls.Add(1)
	switch action {
	case "permission.panel":
		return &APIResponse{Success: true, Data: json.RawMessage(`{
			"grants":[],"requests":[],"visibility":[],"snapshot":{"subject":"court-persona-user-advocate"}
		}`)}, nil
	default:
		return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
	}
}

func TestCourtPersonaTraceSkipsChatBridgeCalls(t *testing.T) {
	cc := &countingBridgeClient{}
	srv, _ := New("127.0.0.1:0", cc)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/court-persona-user-advocate/trace", nil)
	rec := httptest.NewRecorder()
	start := time.Now()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("trace took %v; expected fast path", elapsed)
	}
	if cc.calls.Load() != 0 {
		t.Fatalf("expected 0 bridge calls, got %d", cc.calls.Load())
	}
}

func TestAgentPermissionsUsesSinglePanelCall(t *testing.T) {
	cc := &countingBridgeClient{}
	srv, _ := New("127.0.0.1:0", cc)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/court-persona-user-advocate/permissions", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	if cc.calls.Load() != 1 {
		t.Fatalf("expected 1 permission.panel call, got %d", cc.calls.Load())
	}
}