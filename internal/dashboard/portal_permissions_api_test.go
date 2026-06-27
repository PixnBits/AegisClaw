package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AegisClaw/internal/permissions"
)

type permissionsMockClient struct{}

func (m *permissionsMockClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	switch action {
	case "permission.panel":
		return &APIResponse{Success: true, Data: json.RawMessage(`{
			"agent_id":"coder-test",
			"grants":[{"capability":"channel.post","subject":"coder-test"}],
			"requests":[{"capability":"channel.create","status":"pending","context":"need channel"}],
			"visibility":[],
			"snapshot":{"subject":"coder-test","allowed_tools":{"channel.post":true}}
		}`)}, nil
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

func TestHandleAPIAgentPermissions_GET_ProjectManagerBootstrap(t *testing.T) {
	st := permissions.DefaultBootstrap()
	mc := &mutPermClient{st: st}
	srv, _ := New("127.0.0.1:0", mc)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/project-manager-main/permissions", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	grants, ok := out["grants"].([]interface{})
	if !ok || len(grants) == 0 {
		t.Fatalf("expected PM bootstrap grants, got %v", out["grants"])
	}
	body := string(rec.Body.Bytes())
	if !strings.Contains(body, "channel.create") {
		t.Errorf("expected channel.create in PM grants, got %s", body)
	}
	snap, ok := out["snapshot"].(map[string]interface{})
	if !ok || snap["subject"] != "project-manager-main" {
		t.Errorf("expected permission snapshot for project-manager-main, got %v", out["snapshot"])
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

type mutPermClient struct {
	st *permissions.State
}

func (m *mutPermClient) Call(_ context.Context, action string, pl json.RawMessage) (*APIResponse, error) {
	var p map[string]interface{}
	if len(pl) > 0 {
		json.Unmarshal(pl, &p)
	}
	if p == nil {
		p = map[string]interface{}{}
	}
	src := "web-portal"
	if fc, ok := p["from_ciso"].(bool); ok && fc {
		src = "court-persona-ciso-1"
	}
	var aud []interface{}
	respCmd, resp, e := permissions.DispatchCommand(m.st, src, action, p, &aud, permissions.NowRFC3339())
	if e != nil {
		return &APIResponse{Success: false, Error: e.Error()}, nil
	}
	b, _ := json.Marshal(resp)
	if respCmd != "" {
		return &APIResponse{Success: true, Data: b}, nil
	}
	if action == "permission.panel" {
		subject, _ := p["subject"].(string)
		_, panel, e := permissions.DispatchCommand(m.st, src, action, map[string]interface{}{"subject": subject}, &aud, permissions.NowRFC3339())
		if e != nil {
			return &APIResponse{Success: false, Error: e.Error()}, nil
		}
		b, _ := json.Marshal(panel)
		return &APIResponse{Success: true, Data: b}, nil
	}
	if action == "permission.list" {
		b, _ := json.Marshal(map[string]interface{}{"grants": m.st.Grants})
		return &APIResponse{Success: true, Data: b}, nil
	}
	return &APIResponse{Success: true, Data: []byte(`{}`)}, nil
}

func TestHandleAPIAgentPermissions_POST_RejectsMissingFields(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &permissionsMockClient{})

	for _, body := range []string{
		`{"action":"grant"}`,
		`{"capability":"channel.post"}`,
		`{}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/agents/coder-test/permissions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, rec.Code)
		}
	}
}

func TestHandleAPIAgentPermissions_POST_GrantRevoke(t *testing.T) {
	st := permissions.NewState()
	_ = permissions.GrantCapability(st, "coder-test", "seed.cap", "test", "")
	mc := &mutPermClient{st: st}
	srv, _ := New("127.0.0.1:0", mc)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/coder-test/permissions", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var before map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &before)
	beforeGrants := 0
	if g, ok := before["grants"].([]interface{}); ok {
		beforeGrants = len(g)
	}

	body := `{"action":"grant","capability":"mut.cap"}`
	req = httptest.NewRequest(http.MethodPost, "/api/agents/coder-test/permissions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Aegis-Confirmed", "1")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("grant %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agents/coder-test/permissions", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var after map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &after)
	afterGrants := 0
	if g, ok := after["grants"].([]interface{}); ok {
		afterGrants = len(g)
	}
	if beforeGrants == afterGrants {
		t.Errorf("expected mutation before=%d after=%d", beforeGrants, afterGrants)
	}
	t.Logf("DASHBOARD_MUTATE before=%d after=%d", beforeGrants, afterGrants)
}

// Test ciso delegation + ciso-source grant path through the real dashboard HTTP handler (mut client now honors from_ciso like fixture, exercises src selection + guard on shipped Dispatch).
func TestDashboard_CisoDelegationAndCisoSourceGrant(t *testing.T) {
	st := permissions.NewState()
	mc := &mutPermClient{st: st}
	srv, _ := New("127.0.0.1:0", mc)

	// Enable delegation via real POST
	body := `{"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings/ciso-delegation", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Aegis-Confirmed", "1")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delegation set %d", rec.Code)
	}

	// Ciso source grant via real POST to permissions handler, with from_ciso in body (client will select src=court-persona-ciso-1).
	grantBody := `{"action":"grant","capability":"ciso.dash.handler","subject":"coder-test","from_ciso":true}`
	req = httptest.NewRequest(http.MethodPost, "/api/agents/coder-test/permissions", strings.NewReader(grantBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Aegis-Confirmed", "1")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ciso grant via handler %d", rec.Code)
	}
	if !permissions.HasGrant(st, "coder-test", "ciso.dash.handler") {
		t.Error("ciso grant from handler should be present")
	}
	t.Log("DASHBOARD_CISO_HANDLER delegation + ciso-source grant via real POST/handler/mutClient (from_ciso src) succeeded")
}

