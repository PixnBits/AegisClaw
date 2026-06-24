package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	case "ciso.delegation.get":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"enabled":false}`)}, nil
	case "ciso.delegation.set":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"ok":true}`)}, nil
	case "permission.grant", "permission.revoke", "visibility.set":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"ok":true}`)}, nil
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

func TestHandleAPICisoDelegation_POST_GET(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &permissionsMockClient{})
	// POST set (high impact requires confirmation header)
	req := httptest.NewRequest(http.MethodPost, "/api/settings/ciso-delegation", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Aegis-Confirmed", "1")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set status %d: %s", rec.Code, rec.Body.String())
	}

	// GET
	req = httptest.NewRequest(http.MethodGet, "/api/settings/ciso-delegation", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status %d", rec.Code)
	}
}

func TestHandleAPIAgentPermissions_POST_GrantRevoke(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &permissionsMockClient{})

	// before
	req := httptest.NewRequest(http.MethodGet, "/api/agents/coder-test/permissions", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var before map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &before)
	beforeGrants := 0
	if g, ok := before["grants"].([]interface{}); ok { beforeGrants = len(g) }

	for _, act := range []string{"grant", "revoke"} {
		body := `{"action":"` + act + `","capability":"channel.post"}`
		req = httptest.NewRequest(http.MethodPost, "/api/agents/coder-test/permissions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Aegis-Confirmed", "1")
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status %d: %s", act, rec.Code, rec.Body.String())
		}
	}

	// after
	req = httptest.NewRequest(http.MethodGet, "/api/agents/coder-test/permissions", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var after map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &after)
	afterGrants := 0
	if g, ok := after["grants"].([]interface{}); ok { afterGrants = len(g) }
	t.Logf("PERM_FLOW before grants=%d after=%d shape=%+v", beforeGrants, afterGrants, after)
}
