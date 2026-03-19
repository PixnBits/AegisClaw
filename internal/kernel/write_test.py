#!/usr/bin/env python3
"""Writes kernel integration tests."""

code = r'''package kernel

import (
	"path/filepath"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/audit"
	"go.uber.org/zap/zaptest"
)

func TestKernel_SignAndLogMerkle(t *testing.T) {
	// Reset singleton from any previous test
	ResetInstance()
	defer ResetInstance()

	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()

	k, err := GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}

	// Sign and log a kernel start action
	action := NewAction(ActionKernelStart, "kernel", nil)
	signed, err := k.SignAndLog(action)
	if err != nil {
		t.Fatalf("SignAndLog(kernel.start): %v", err)
	}

	if signed.Action.ID != action.ID {
		t.Fatalf("action ID mismatch")
	}
	if len(signed.Signature) == 0 {
		t.Fatal("signature is empty")
	}

	// Sign and log a sandbox create action
	action2 := NewAction(ActionSandboxCreate, "kernel", []byte(`{"name":"test"}`))
	_, err = k.SignAndLog(action2)
	if err != nil {
		t.Fatalf("SignAndLog(sandbox.create): %v", err)
	}

	// Verify the Merkle chain
	auditPath := filepath.Join(auditDir, "kernel.merkle.jsonl")
	verified, err := audit.VerifyChain(auditPath, k.PublicKey())
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if verified != 2 {
		t.Fatalf("expected 2 verified entries, got %d", verified)
	}

	// Check audit log state
	al := k.AuditLog()
	if al.EntryCount() != 2 {
		t.Fatalf("expected 2 entries, got %d", al.EntryCount())
	}
	if al.LastHash() == "" {
		t.Fatal("expected non-empty last hash")
	}
}

func TestKernel_SignAndVerify(t *testing.T) {
	ResetInstance()
	defer ResetInstance()

	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()

	k, err := GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}

	data := []byte("test data for signing")
	sig := k.Sign(data)

	if !k.Verify(data, sig) {
		t.Fatal("signature verification failed for correct data")
	}

	// Tampered data should fail
	if k.Verify([]byte("tampered data"), sig) {
		t.Fatal("signature verification should fail for tampered data")
	}
}

func TestKernel_InvalidActionRejected(t *testing.T) {
	ResetInstance()
	defer ResetInstance()

	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()

	k, err := GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}

	// Action with empty source should be rejected
	action := Action{
		ID:   "bad-action",
		Type: ActionKernelStart,
	}
	_, err = k.SignAndLog(action)
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}
'''

with open('kernel_test.go', 'w') as f:
    f.write(code)
print(f"Written {len(code)} bytes")
