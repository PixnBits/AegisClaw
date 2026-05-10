package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallLLM(t *testing.T) {
	t.Setenv("AEGIS_OLLAMA_URL", "http://127.0.0.1:0")

	response := callLLM("Observe test")
	if !strings.Contains(response, "Observed") {
		t.Errorf("Expected Observed, got %s", response)
	}

	response = callLLM("Think test")
	if !strings.Contains(response, "Analyzed") {
		t.Errorf("Expected Analyzed, got %s", response)
	}
}

func TestCallLLMUsesOllamaResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("expected /api/generate path, got %s", r.URL.Path)
		}
		var req ollamaGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != defaultOllamaModel {
			t.Fatalf("expected default model %s, got %s", defaultOllamaModel, req.Model)
		}
		_ = json.NewEncoder(w).Encode(ollamaGenerateResponse{Response: "ollama-success"})
	}))
	defer server.Close()

	t.Setenv("AEGIS_OLLAMA_URL", server.URL)
	response := callLLM("Observe this via Ollama")
	if response != "ollama-success" {
		t.Fatalf("expected ollama response, got %s", response)
	}
}

func TestSignMessage(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	msg := &Message{
		Source:    "test",
		Command:   "test",
		Payload:   "data",
		Timestamp: "2026-05-10T00:00:00Z",
	}
	signMessage(msg, priv)
	if msg.Signature == "" {
		t.Error("Signature not set")
	}

	data, _ := json.Marshal(Message{Source: "test", Command: "test", Payload: "data", Timestamp: "2026-05-10T00:00:00Z"})
	sigBytes, _ := base64.StdEncoding.DecodeString(msg.Signature)
	if !ed25519.Verify(pub, data, sigBytes) {
		t.Error("Signature verification failed")
	}
}
