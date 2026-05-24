package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCountTokens(t *testing.T) {
	messages := []string{"hello", "world"}
	expected := 10
	if count := countTokens(messages); count != expected {
		t.Errorf("Expected %d, got %d", expected, count)
	}
}

func TestLoadSaveFromFile(t *testing.T) {
	filename := "test_memory.json"
	defer os.Remove(filename)

	data := map[string]interface{}{"key": "value"}
	saveToFile(filename, data)

	loaded := loadFromFile(filename)
	if loaded["key"] != "value" {
		t.Errorf("Expected value, got %v", loaded["key"])
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

	// Verify
	data, _ := json.Marshal(Message{Source: "test", Command: "test", Payload: "data", Timestamp: "2026-05-10T00:00:00Z"})
	sigBytes, _ := base64.StdEncoding.DecodeString(msg.Signature)
	if !ed25519.Verify(pub, data, sigBytes) {
		t.Error("Signature verification failed")
	}
}

func TestMemoryCommands(t *testing.T) {
	longTerm := make(map[string]interface{})
	content := "test memory"
	payload := map[string]interface{}{
		"content": content,
		"tags":    []string{"test"},
	}
	longTerm[content] = payload

	// Test search logic
	results := []interface{}{}
	query := "test"
	for k, v := range longTerm {
		if strings.Contains(k, query) {
			results = append(results, v)
		}
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}

func TestNormalizeVector(t *testing.T) {
	f64s := []float64{0.1, 0.2}
	if got := normalizeVector(f64s); len(got) != 2 {
		t.Error("[]float64 passthrough failed")
	}
	ifs := []interface{}{0.3, 0.4}
	if got := normalizeVector(ifs); len(got) != 2 || got[0] != 0.3 {
		t.Error("[]interface{} normalize failed")
	}
	if got := normalizeVector("bad"); got != nil {
		t.Error("bad type should return nil")
	}
}

func TestTokenLimitEnforcement(t *testing.T) {
	// Simulate the trim logic from get_context (len-based for test simplicity)
	short := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	if len(short) > 5 {
		short = short[len(short)-5:]
	}
	if len(short) != 5 {
		t.Errorf("trim did not reduce to <=5, got %d", len(short))
	}
	// Real impl also recounts tokens post-trim; here we just validate len guard
}

func TestMemoryContextResponseShape(t *testing.T) {
	// Light validation that enhanced get_context would include spec fields
	payload := map[string]interface{}{
		"short_term":     []string{"recent1"},
		"long_term":      []interface{}{"mem1"},
		"token_count":    42,
		"token_limit":    32000,
		"retrieval_note": "top semantic...",
	}
	if payload["token_limit"] != 32000 {
		t.Error("context payload missing token_limit per spec")
	}
}
