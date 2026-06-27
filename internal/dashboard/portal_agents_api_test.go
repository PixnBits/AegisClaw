package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// portalWorkerListFixture mirrors cmd/aegis portalWorkerList + channel roster merge shapes.
func portalWorkerListFixture() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"id": "project-manager-main", "name": "project-manager-main",
			"status": "standby", "role": "project-manager", "task": "project-manager",
			"progress": "—", "channel": "main", "channel_id": "main",
		},
		map[string]interface{}{
			"id": "court-persona-ciso", "name": "court-persona-ciso",
			"status": "running", "role": "court", "task": "court",
			"progress": "—", "channel": "main", "channel_id": "main",
		},
		map[string]interface{}{
			"id": "coder-main", "name": "coder-main",
			"status": "running", "role": "coder", "task": "coder",
			"progress": "42%", "channel": "main", "channel_id": "main",
		},
	}
}

func TestHandleAPIAgents_ReturnsWrappedAgentsObject(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &fakeAPIClient{
		data: map[string]interface{}{
			"worker.list": portalWorkerListFixture(),
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	if body == "[]" || body == "null" {
		t.Fatalf("GET /api/agents must return {\"agents\":[...]}, got bare %s", body)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v body=%s", err, body)
	}
	agents, ok := out["agents"].([]interface{})
	if !ok {
		t.Fatalf("expected agents array in response, got %T (%v)", out["agents"], out)
	}
	if len(agents) == 0 {
		t.Fatal("expected non-empty agents when worker.list returns portal roster")
	}
}

func TestHandleAPIAgents_IncludesProjectManagerAndCourtPersonas(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &fakeAPIClient{
		data: map[string]interface{}{
			"worker.list":        portalWorkerListFixture(),
			"channel.turn_state": map[string]interface{}{"members": []interface{}{}},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var out struct {
		Agents []map[string]interface{} `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	names := make(map[string]struct{}, len(out.Agents))
	for _, a := range out.Agents {
		if n, ok := a["name"].(string); ok {
			names[n] = struct{}{}
		}
	}
	for _, want := range []string{"project-manager-main", "court-persona-ciso", "coder-main"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing agent card %q; got names=%v", want, names)
		}
	}
}

func TestHandleAPIAgents_EmptyWorkerListRegression(t *testing.T) {
	// Documents the failure mode users see at :8080/api/agents when worker.list is empty.
	srv, _ := New("127.0.0.1:0", &fakeAPIClient{
		data: map[string]interface{}{
			"worker.list": []interface{}{},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	agents, _ := out["agents"].([]interface{})
	if agents == nil {
		t.Fatalf("expected agents key even when empty, got %v", out)
	}
	if len(agents) != 0 {
		t.Fatalf("expected empty agents slice for empty worker.list, got %d", len(agents))
	}
}

func TestHandleAPIAgents_SurvivesMalformedWorkerListMap(t *testing.T) {
	// Bridge/stub bug class: worker.list returns {} instead of [] — must not panic; agents stay empty.
	srv, _ := New("127.0.0.1:0", &fakeAPIClient{
		data: map[string]interface{}{
			"worker.list": map[string]interface{}{"unexpected": "shape"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	agents, ok := out["agents"].([]interface{})
	if !ok {
		t.Fatalf("expected agents array, got %T", out["agents"])
	}
	if len(agents) != 0 {
		t.Errorf("malformed worker.list should yield empty agents, got %d", len(agents))
	}
}

func TestHandleAPIAgents_AgentCardFields(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &fakeAPIClient{
		data: map[string]interface{}{"worker.list": portalWorkerListFixture()},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var out struct {
		Agents []map[string]interface{} `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Agents) == 0 {
		t.Fatal("expected agent cards")
	}
	for i, a := range out.Agents {
		for _, key := range []string{"name", "status", "task", "progress"} {
			if _, ok := a[key]; !ok {
				t.Errorf("agent[%d] missing %q: %v", i, key, a)
			}
		}
	}
}