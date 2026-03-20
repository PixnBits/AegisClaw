package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

func testRegistry(t *testing.T) *ModelRegistry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	r, err := NewModelRegistry(path)
	if err != nil {
		t.Fatalf("NewModelRegistry: %v", err)
	}
	return r
}

func ollamaMockServer(t *testing.T, models []ModelInfo) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(ListResponse{Models: models})
		case "/api/show":
			json.NewEncoder(w).Encode(ShowResponse{
				Details: ModelDetails{
					Family:        "test",
					ParameterSize: "3B",
				},
			})
		case "/api/pull":
			var req PullRequest
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(PullResponse{
				Status: "success",
				Digest: "sha256:abc123def456",
				Total:  1000,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestManagerListStatus(t *testing.T) {
	models := []ModelInfo{
		{Name: "mistral-nemo:latest", Size: 4000000000, Digest: "sha256:aabbcc"},
		{Name: "llama3.2:3b", Size: 2000000000, Digest: "sha256:ddeeff"},
	}
	srv := ollamaMockServer(t, models)
	defer srv.Close()

	reg := testRegistry(t)
	reg.Register(ModelEntry{Name: "mistral-nemo", SHA256: "aabbcc", Tags: []string{"security"}})

	client := NewClient(ClientConfig{Endpoint: srv.URL})
	mgr := NewManager(client, reg, ManagerConfig{}, testLogger())

	statuses, err := mgr.ListStatus(context.Background())
	if err != nil {
		t.Fatalf("ListStatus failed: %v", err)
	}

	if len(statuses) < 2 {
		t.Fatalf("expected at least 2 statuses, got %d", len(statuses))
	}

	// Find mistral-nemo
	var found bool
	for _, s := range statuses {
		if s.Name == "mistral-nemo" {
			found = true
			if !s.Registered {
				t.Error("expected registered")
			}
			if !s.Available {
				t.Error("expected available")
			}
			if !s.Verified {
				t.Error("expected verified")
			}
		}
	}
	if !found {
		t.Error("mistral-nemo not found in statuses")
	}
}

func TestManagerListStatusUnregistered(t *testing.T) {
	models := []ModelInfo{
		{Name: "unknown-model", Size: 1000, Digest: "sha256:xyz"},
	}
	srv := ollamaMockServer(t, models)
	defer srv.Close()

	reg := testRegistry(t)
	client := NewClient(ClientConfig{Endpoint: srv.URL})
	mgr := NewManager(client, reg, ManagerConfig{}, testLogger())

	statuses, err := mgr.ListStatus(context.Background())
	if err != nil {
		t.Fatalf("ListStatus failed: %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Registered {
		t.Error("expected not registered")
	}
	if !statuses[0].Available {
		t.Error("expected available")
	}
}

func TestManagerVerify(t *testing.T) {
	models := []ModelInfo{
		{Name: "test-model", Size: 500, Digest: "sha256:correcthash"},
	}
	srv := ollamaMockServer(t, models)
	defer srv.Close()

	reg := testRegistry(t)
	reg.Register(ModelEntry{Name: "test-model", SHA256: "correcthash", Tags: []string{"test"}})

	client := NewClient(ClientConfig{Endpoint: srv.URL})
	mgr := NewManager(client, reg, ManagerConfig{}, testLogger())

	status, err := mgr.Verify(context.Background(), "test-model")
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !status.Verified {
		t.Error("expected verified")
	}
	if !status.Registered {
		t.Error("expected registered")
	}
	if !status.Available {
		t.Error("expected available")
	}
}

func TestManagerVerifyMismatch(t *testing.T) {
	models := []ModelInfo{
		{Name: "test-model", Size: 500, Digest: "sha256:wronghash"},
	}
	srv := ollamaMockServer(t, models)
	defer srv.Close()

	reg := testRegistry(t)
	reg.Register(ModelEntry{Name: "test-model", SHA256: "correcthash", Tags: []string{"test"}})

	client := NewClient(ClientConfig{Endpoint: srv.URL})
	mgr := NewManager(client, reg, ManagerConfig{}, testLogger())

	status, err := mgr.Verify(context.Background(), "test-model")
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if status.Verified {
		t.Error("expected not verified (hash mismatch)")
	}
}

func TestManagerVerifyNotAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`model not found`))
	}))
	defer srv.Close()

	reg := testRegistry(t)
	reg.Register(ModelEntry{Name: "missing", SHA256: "abc", Tags: nil})

	client := NewClient(ClientConfig{Endpoint: srv.URL})
	mgr := NewManager(client, reg, ManagerConfig{}, testLogger())

	status, err := mgr.Verify(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for unavailable model")
	}
	if status.Available {
		t.Error("expected not available")
	}
	if status.Registered != true {
		t.Error("expected registered")
	}
}

func TestManagerUpdate(t *testing.T) {
	models := []ModelInfo{
		{Name: "new-model", Size: 3000, Digest: "sha256:abc123def456"},
	}
	srv := ollamaMockServer(t, models)
	defer srv.Close()

	reg := testRegistry(t)
	client := NewClient(ClientConfig{Endpoint: srv.URL})
	mgr := NewManager(client, reg, ManagerConfig{}, testLogger())

	status, err := mgr.Update(context.Background(), "new-model")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if !status.Registered {
		t.Error("expected registered after update")
	}
	if !status.Available {
		t.Error("expected available after update")
	}
	if !status.Verified {
		t.Error("expected verified after update")
	}

	// Check registry was persisted
	entry, ok := reg.Get("new-model")
	if !ok {
		t.Fatal("expected model in registry after update")
	}
	if entry.SHA256 != "abc123def456" {
		t.Errorf("unexpected sha256: %s", entry.SHA256)
	}
}

func TestManagerUpdatePullError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`pull failed`))
	}))
	defer srv.Close()

	reg := testRegistry(t)
	client := NewClient(ClientConfig{Endpoint: srv.URL})
	mgr := NewManager(client, reg, ManagerConfig{}, testLogger())

	_, err := mgr.Update(context.Background(), "broken")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestManagerSyncKnownGood(t *testing.T) {
	reg := testRegistry(t)
	client := NewClient(ClientConfig{Endpoint: "http://127.0.0.1:1"}) // won't connect
	mgr := NewManager(client, reg, ManagerConfig{}, testLogger())

	mgr.SyncKnownGood()

	if reg.Count() != len(KnownGoodModels) {
		t.Errorf("expected %d models, got %d", len(KnownGoodModels), reg.Count())
	}

	// Should not overwrite existing entries
	reg.Register(ModelEntry{Name: KnownGoodModels[0].Name, SHA256: "custom", Tags: []string{"custom"}})
	mgr.SyncKnownGood()

	entry, _ := reg.Get(KnownGoodModels[0].Name)
	if entry.SHA256 != "custom" {
		t.Error("SyncKnownGood should not overwrite existing entries")
	}
}

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"mistral-nemo", "mistral-nemo"},
		{"mistral-nemo:latest", "mistral-nemo"},
		{"llama3.2:3b", "llama3.2:3b"},
		{"model:v2", "model:v2"},
	}
	for _, tc := range tests {
		got := normalizeModelName(tc.input)
		if got != tc.want {
			t.Errorf("normalizeModelName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTagsForModel(t *testing.T) {
	tags := tagsForModel("mistral-nemo")
	if len(tags) == 0 {
		t.Error("expected tags for known model")
	}

	tags = tagsForModel("unknown-model-xyz")
	if tags != nil {
		t.Errorf("expected nil tags for unknown model, got %v", tags)
	}
}
