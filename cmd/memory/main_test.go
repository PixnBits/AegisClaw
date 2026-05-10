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
