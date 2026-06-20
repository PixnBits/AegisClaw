package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHarnessEndpointReturnsDefaultPlan(t *testing.T) {
	client := &mockHarnessClient{}
	srv, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/channels/main/harness", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var state map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	plan, ok := state["plan"].(map[string]interface{})
	if !ok {
		t.Fatal("missing plan")
	}
	if plan["plan_id"] != "plan_main" {
		t.Fatalf("plan_id: %v", plan["plan_id"])
	}
}

type mockHarnessClient struct{}

func (m *mockHarnessClient) Call(_ context.Context, _ string, _ json.RawMessage) (*APIResponse, error) {
	return &APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
}