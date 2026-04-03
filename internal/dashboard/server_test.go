package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/dashboard"
)

// stubClient is a minimal APIClient for tests.
type stubClient struct{}

func (s *stubClient) Call(_ context.Context, action string, _ json.RawMessage) (*dashboard.APIResponse, error) {
	switch action {
	case "worker.list":
		data, _ := json.Marshal([]map[string]interface{}{
			{"worker_id": "aaaa-bbbb-cccc-dddd", "role": "researcher", "status": "done", "step_count": 5, "task_description": "research Go generics", "spawned_at": time.Now().UTC()},
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

func TestDashboard_IndexRedirect(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/agents" {
		t.Errorf("expected redirect to /agents, got %q", loc)
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
