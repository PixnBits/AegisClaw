package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type permissionsMockClient struct{}

func (m *permissionsMockClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	switch action {
	case "permission.list":
		return &APIResponse{Success: true, Data: json.RawMessage(`[{"capability":"channel.post","subject":"coder-test"}]`)}, nil
	case "permission.requests.list":
		return &APIResponse{Success: true, Data: json.RawMessage(`[{"capability":"channel.create","status":"pending","context":"need channel"}]`)}, nil
	case "visibility.list":
		return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
	case "permission.snapshot":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"subject":"coder-test","allowed_tools":{"channel.post":true}}`)}, nil
	default:
		return &APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
	}
}

func TestHandleAPIAgentPermissions_GET(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &permissionsMockClient{})
	req := httptest.NewRequest(http.MethodGet, "/api/agents/coder-test/permissions", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["agent_id"] != "coder-test" {
		t.Errorf("expected agent_id coder-test, got %v", out["agent_id"])
	}
	grants, ok := out["grants"].([]interface{})
	if !ok || len(grants) == 0 {
		t.Errorf("expected grants in response, got %v", out["grants"])
	}
}
