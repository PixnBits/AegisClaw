#!/usr/bin/env python3
"""Writes audit package tests — run once then delete this script."""

code = r'''package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func testKeys(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return priv, pub
}

func testLogPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.merkle.jsonl")
}

func TestMerkleLog_AppendAndVerify(t *testing.T) {
	priv, pub := testKeys(t)
	path := testLogPath(t)

	ml, err := NewMerkleLog(path, priv, pub, testLogger(t))
	if err != nil {
		t.Fatalf("NewMerkleLog: %v", err)
	}
	defer ml.Close()

	// Empty log should have zero entries
	if ml.EntryCount() != 0 {
		t.Fatalf("expected 0 entries, got %d", ml.EntryCount())
	}
	if ml.LastHash() != "" {
		t.Fatalf("expected empty last hash, got %q", ml.LastHash())
	}

	// Append entries
	for i := 0; i < 10; i++ {
		payload, _ := json.Marshal(map[string]int{"index": i})
		id, hash, err := ml.Append(payload)
		if err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
		if id == "" {
			t.Fatal("got empty entry ID")
		}
		if hash == "" {
			t.Fatal("got empty hash")
		}
	}

	if ml.EntryCount() != 10 {
		t.Fatalf("expected 10 entries, got %d", ml.EntryCount())
	}
	if ml.LastHash() == "" {
		t.Fatal("last hash should not be empty after appends")
	}

	// Close and verify chain
	ml.Close()

	verified, err := VerifyChain(path, pub)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if verified != 10 {
		t.Fatalf("expected 10 verified entries, got %d", verified)
	}
}

func TestMerkleLog_ChainContinuation(t *testing.T) {
	priv, pub := testKeys(t)
	path := testLogPath(t)

	// Write 5 entries
	ml, err := NewMerkleLog(path, priv, pub, testLogger(t))
	if err != nil {
		t.Fatalf("NewMerkleLog: %v", err)
	}
	for i := 0; i < 5; i++ {
		payload, _ := json.Marshal(map[string]int{"batch": 1, "index": i})
		if _, _, err := ml.Append(payload); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	lastHash := ml.LastHash()
	ml.Close()

	// Reopen and append 5 more
	ml2, err := NewMerkleLog(path, priv, pub, testLogger(t))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer ml2.Close()

	// Should recover the chain state
	if ml2.EntryCount() != 5 {
		t.Fatalf("expected 5 recovered entries, got %d", ml2.EntryCount())
	}
	if ml2.LastHash() != lastHash {
		t.Fatalf("last hash mismatch after reopen: expected %q, got %q", lastHash, ml2.LastHash())
	}

	for i := 0; i < 5; i++ {
		payload, _ := json.Marshal(map[string]int{"batch": 2, "index": i})
		if _, _, err := ml2.Append(payload); err != nil {
			t.Fatalf("Append after reopen: %v", err)
		}
	}

	if ml2.EntryCount() != 10 {
		t.Fatalf("expected 10 total entries, got %d", ml2.EntryCount())
	}

	ml2.Close()

	verified, err := VerifyChain(path, pub)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if verified != 10 {
		t.Fatalf("expected 10 verified, got %d", verified)
	}
}

func TestMerkleLog_TamperDetection(t *testing.T) {
	priv, pub := testKeys(t)
	path := testLogPath(t)

	ml, err := NewMerkleLog(path, priv, pub, testLogger(t))
	if err != nil {
		t.Fatalf("NewMerkleLog: %v", err)
	}
	for i := 0; i < 5; i++ {
		payload, _ := json.Marshal(map[string]string{"data": "original"})
		if _, _, err := ml.Append(payload); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	ml.Close()

	// Tamper with the file: modify a byte in the middle
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Find "original" in third entry and change it
	target := []byte("original")
	count := 0
	for i := 0; i < len(data)-len(target); i++ {
		match := true
		for j := 0; j < len(target); j++ {
			if data[i+j] != target[j] {
				match = false
				break
			}
		}
		if match {
			count++
			if count == 3 {
				data[i] = 'X' // tamper: "Xriginal"
				break
			}
		}
	}
	if count < 3 {
		t.Fatal("could not find target for tampering")
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Verification should fail
	_, err = VerifyChain(path, pub)
	if err == nil {
		t.Fatal("expected verification to fail after tampering")
	}
}

func TestMerkleLog_WrongKeyDetection(t *testing.T) {
	priv, pub := testKeys(t)
	path := testLogPath(t)

	ml, err := NewMerkleLog(path, priv, pub, testLogger(t))
	if err != nil {
		t.Fatalf("NewMerkleLog: %v", err)
	}
	payload, _ := json.Marshal(map[string]string{"data": "test"})
	if _, _, err := ml.Append(payload); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ml.Close()

	// Verify with a different key should fail
	_, wrongPub := testKeys(t)
	_, err = VerifyChain(path, wrongPub)
	if err == nil {
		t.Fatal("expected verification to fail with wrong key")
	}
}

func TestMerkleLog_EmptyLogVerifies(t *testing.T) {
	_, pub := testKeys(t)
	path := testLogPath(t)

	// Create empty file
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Close()

	verified, err := VerifyChain(path, pub)
	if err != nil {
		t.Fatalf("VerifyChain on empty log: %v", err)
	}
	if verified != 0 {
		t.Fatalf("expected 0 verified entries, got %d", verified)
	}
}
'''

with open('merkle_test.go', 'w') as f:
    f.write(code)
print(f"Written {len(code)} bytes")
