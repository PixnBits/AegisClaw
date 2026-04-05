package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/dashboard"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// stubClient is a minimal APIClient for tests.
type stubClient struct{}

func (s *stubClient) Call(_ context.Context, action string, _ json.RawMessage) (*dashboard.APIResponse, error) {
	switch action {
	case "worker.list":
		data, _ := json.Marshal([]map[string]interface{}{
			{"worker_id": "aaaa-bbbb-cccc-dddd", "role": "researcher", "status": "done", "step_count": 5, "task_description": "research Go generics", "spawned_at": time.Now().UTC()},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil
	case "dashboard.skills":
		data, _ := json.Marshal(map[string]interface{}{
			"runtime_skills": []map[string]interface{}{
				{
					"name":        "calendar-sync",
					"description": "Synchronize calendar events with a remote service.",
					"version":     2,
					"state":       "active",
					"sandbox_id":  "sandbox-calendar-sync",
					"tools": []map[string]interface{}{{
						"name":        "sync",
						"description": "Sync upcoming events.",
					}},
				},
			},
			"built_in_skills": []map[string]interface{}{
				{
					"name":        "default-script-runner",
					"description": "Default scripting runner that executes short scripts safely.",
					"state":       "submitted",
					"source":      "built-in baseline (system)",
					"tools": []map[string]interface{}{{
						"name":        "execute_script",
						"description": "Execute short scripts using approved runtimes.",
					}},
				},
			},
			"built_in_templates": []map[string]interface{}{
				{
					"name":        "skill_script_runner",
					"kind":        "builder_template",
					"description": "Generate a hardened Go wrapper that executes approved scripts",
				},
			},
			"proposals": []map[string]interface{}{
				{
					"id":           "prop-1234",
					"title":        "Bootstrap default script runner skill",
					"status":       "submitted",
					"category":     "new_skill",
					"target_skill": "default-script-runner",
				},
			},
		})
		return &dashboard.APIResponse{Success: true, Data: data}, nil
	case "event.timers.list":
		return &dashboard.APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
	case "event.signals.list":
		return &dashboard.APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
	case "event.approvals.list":
		return &dashboard.APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
	case "memory.list":
		return &dashboard.APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
	default:
		return &dashboard.APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
	}
}

func newTestServer(t *testing.T) *dashboard.Server {
	t.Helper()
	s, err := dashboard.New("127.0.0.1:0", &stubClient{})
	if err != nil {
		t.Fatalf("dashboard.New: %v", err)
	}
	return s
}

func TestDashboard_HealthEndpoint(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestDashboard_IndexPage(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Overview") {
		t.Errorf("expected Overview page, got: %s", body[:minInt(200, len(body))])
	}
}

func TestDashboard_AgentsPage(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/agents", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if len(body) < 100 {
		t.Error("expected non-empty HTML response")
	}
}

func TestDashboard_AsyncPage(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/async", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestDashboard_MemoryPage(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/memory", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestDashboard_ApprovalsPage(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/approvals", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestDashboard_NotFound(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestDashboard_SkillsPage(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/skills", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Runtime Skills") {
		t.Fatalf("expected runtime skills section, got body of length %d", len(body))
	}
	if !strings.Contains(body, "default-script-runner") {
		t.Error("expected built-in baseline to be shown")
	}
	if !strings.Contains(body, "skill_script_runner") {
		t.Error("expected built-in template to be shown")
	}
	if !strings.Contains(body, "execute_script") || !strings.Contains(body, "sync") {
		t.Error("expected skill tools to be rendered in the page")
	}
}
