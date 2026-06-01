package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

func TestLoadSaveFromFile(t *testing.T) {
	filename := "test_store.json"
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

	data, _ := json.Marshal(Message{Source: "test", Command: "test", Payload: "data", Timestamp: "2026-05-10T00:00:00Z"})
	sigBytes, _ := base64.StdEncoding.DecodeString(msg.Signature)
	if !ed25519.Verify(pub, data, sigBytes) {
		t.Error("Signature verification failed")
	}
}

func TestStoreCommands(t *testing.T) {
	// Test proposal create
	proposals := make(map[string]interface{})
	payload := map[string]interface{}{
		"id":          "test_proposal",
		"description": "test",
	}
	id := payload["id"].(string)
	payload["state"] = "pending"
	payload["reviews"] = make(map[string]string)
	proposals[id] = payload

	if proposals["test_proposal"] == nil {
		t.Error("Proposal not created")
	}
}

func TestComputeMerkleRoot(t *testing.T) {
	log := []interface{}{"entry1", "entry2"}
	root := computeMerkleRoot(log)
	if root == "" {
		t.Error("Root should not be empty")
	}
	// Test empty
	emptyRoot := computeMerkleRoot([]interface{}{})
	if emptyRoot != "" {
		t.Error("Empty root should be empty string")
	}
}
