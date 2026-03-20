package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient(ClientConfig{})
	if c.endpoint != OllamaEndpoint {
		t.Errorf("expected default endpoint %q, got %q", OllamaEndpoint, c.endpoint)
	}
}

func TestNewClientCustomEndpoint(t *testing.T) {
	c := NewClient(ClientConfig{Endpoint: "http://10.0.0.1:11434"})
	if c.endpoint != "http://10.0.0.1:11434" {
		t.Errorf("unexpected endpoint %q", c.endpoint)
	}
}

func TestNewClientCustomTimeout(t *testing.T) {
	c := NewClient(ClientConfig{Timeout: 30 * time.Second})
	if c.http.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", c.http.Timeout)
	}
}

func TestGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected JSON content type")
		}

		var req GenerateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "mistral-nemo" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if req.Stream {
			t.Error("expected stream=false")
		}

		json.NewEncoder(w).Encode(GenerateResponse{
			Model:    "mistral-nemo",
			Response: "Hello, world!",
			Done:     true,
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	resp, err := c.Generate(context.Background(), GenerateRequest{
		Model:  "mistral-nemo",
		Prompt: "Say hello",
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp.Response != "Hello, world!" {
		t.Errorf("unexpected response: %q", resp.Response)
	}
	if resp.Model != "mistral-nemo" {
		t.Errorf("unexpected model: %q", resp.Model)
	}
}

func TestChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(req.Messages))
		}
		if req.Stream {
			t.Error("expected stream=false")
		}

		json.NewEncoder(w).Encode(ChatResponse{
			Model:   req.Model,
			Message: ChatMessage{Role: "assistant", Content: "I am a test response."},
			Done:    true,
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	resp, err := c.Chat(context.Background(), ChatRequest{
		Model: "llama-3.2-3b",
		Messages: []ChatMessage{
			{Role: "user", Content: "Who are you?"},
		},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Message.Content != "I am a test response." {
		t.Errorf("unexpected content: %q", resp.Message.Content)
	}
}

func TestList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(ListResponse{
			Models: []ModelInfo{
				{Name: "mistral-nemo", Size: 4000000000, Digest: "sha256:abc123"},
				{Name: "llama-3.2-3b", Size: 2000000000, Digest: "sha256:def456"},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	models, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Name != "mistral-nemo" {
		t.Errorf("unexpected model name: %s", models[0].Name)
	}
}

func TestShow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ShowResponse{
			Details: ModelDetails{
				Family:            "llama",
				ParameterSize:     "3B",
				QuantizationLevel: "Q4_K_M",
			},
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	resp, err := c.Show(context.Background(), "llama-3.2-3b")
	if err != nil {
		t.Fatalf("Show failed: %v", err)
	}
	if resp.Details.Family != "llama" {
		t.Errorf("unexpected family: %s", resp.Details.Family)
	}
}

func TestPull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(PullResponse{
			Status: "success",
			Digest: "sha256:abc123",
			Total:  4000000000,
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	resp, err := c.Pull(context.Background(), "mistral-nemo")
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("unexpected status: %s", resp.Status)
	}
	if resp.Digest != "sha256:abc123" {
		t.Errorf("unexpected digest: %s", resp.Digest)
	}
}

func TestHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	if !c.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestHealthyFail(t *testing.T) {
	c := NewClient(ClientConfig{Endpoint: "http://127.0.0.1:1"})
	if c.Healthy(context.Background()) {
		t.Error("expected unhealthy")
	}
}

func TestGenerateServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	_, err := c.Generate(context.Background(), GenerateRequest{Model: "nonexistent"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChatServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	_, err := c.Chat(context.Background(), ChatRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateWithOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GenerateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Temperature != 0.7 {
			t.Errorf("expected temperature 0.7, got %f", req.Temperature)
		}
		if req.Format != "json" {
			t.Errorf("expected format json, got %q", req.Format)
		}
		json.NewEncoder(w).Encode(GenerateResponse{
			Model:    req.Model,
			Response: `{"result": true}`,
			Done:     true,
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Endpoint: srv.URL})
	resp, err := c.Generate(context.Background(), GenerateRequest{
		Model:       "mistral-nemo",
		Prompt:      "Return JSON",
		Temperature: 0.7,
		Format:      "json",
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp.Response != `{"result": true}` {
		t.Errorf("unexpected response: %q", resp.Response)
	}
}
