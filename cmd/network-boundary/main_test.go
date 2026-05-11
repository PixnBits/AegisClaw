package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

func TestIsDomainAllowed(t *testing.T) {
	allowed := map[string]bool{
		"example.com":     true,
		"localhost:11434": true,
	}
	if !isDomainAllowed("http://example.com/test", allowed) {
		t.Error("Should allow example.com")
	}
	if !isDomainAllowed("http://localhost:11434/api/generate", allowed) {
		t.Error("Should allow localhost Ollama endpoint")
	}
	if isDomainAllowed("http://blocked.com/test", allowed) {
		t.Error("Should block blocked.com")
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

func TestOllamaBackendHostDefault(t *testing.T) {
	os.Unsetenv("AEGIS_OLLAMA_BACKEND_HOST")
	if got := ollamaBackendHost(); got != "localhost:11434" {
		t.Errorf("expected default localhost:11434, got %q", got)
	}
}

func TestOllamaBackendHostEnvOverride(t *testing.T) {
	t.Setenv("AEGIS_OLLAMA_BACKEND_HOST", "ollama-vm:11434")
	if got := ollamaBackendHost(); got != "ollama-vm:11434" {
		t.Errorf("expected ollama-vm:11434, got %q", got)
	}
}
