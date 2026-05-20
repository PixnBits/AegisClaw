package kernel

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/audit"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
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

// TestKernel_MerkleAuditChainMultiEntries exercises sequential SignAndLog
// appends for the daemon/kernel audit path (DB-02: Merkle signing on action).
func TestKernel_MerkleAuditChainMultiEntries(t *testing.T) {
	ResetInstance()
	defer ResetInstance()

	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()

	k, err := GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}

	const n = 5
	for i := 0; i < n; i++ {
		payload, _ := json.Marshal(map[string]int{"seq": i})
		action := NewAction(ActionSandboxCreate, "kernel", payload)
		if _, err := k.SignAndLog(action); err != nil {
			t.Fatalf("SignAndLog seq %d: %v", i, err)
		}
	}

	auditPath := filepath.Join(auditDir, "kernel.merkle.jsonl")
	verified, err := audit.VerifyChain(auditPath, k.PublicKey())
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if verified != n {
		t.Fatalf("expected %d verified entries, got %d", n, verified)
	}
	if k.AuditLog().EntryCount() != uint64(n) {
		t.Fatalf("entry count: got %d want %d", k.AuditLog().EntryCount(), n)
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

// TestKernel_RunPeriodicAuditSync_NoOpWhenIntervalNonPositive covers the early-return
// branch for disabled / invalid intervals (DB-02 remainder wiring).
func TestKernel_RunPeriodicAuditSync_NoOpWhenIntervalNonPositive(t *testing.T) {
	ResetInstance()
	defer ResetInstance()

	t.Setenv("HOME", t.TempDir())

	k, err := GetInstance(zaptest.NewLogger(t), t.TempDir())
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	ctx := context.Background()
	k.RunPeriodicAuditSync(ctx, 0)
	k.RunPeriodicAuditSync(ctx, -1*time.Second)
}

// TestKernel_RunPeriodicAuditSync_ExitsOnCancel ensures the ticker loop stops when
// the context is cancelled (DB-02: productized interval sync must not leak goroutines).
func TestKernel_RunPeriodicAuditSync_ExitsOnCancel(t *testing.T) {
	ResetInstance()
	defer ResetInstance()

	t.Setenv("HOME", t.TempDir())

	k, err := GetInstance(zaptest.NewLogger(t), t.TempDir())
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		k.RunPeriodicAuditSync(ctx, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPeriodicAuditSync did not exit after cancel")
	}
}

// TestKernelInit_LogsDoNotContainRawPrivateKeyMaterial is DB-04: kernel init must not
// emit the raw Ed25519 private key (full key or seed half) into structured logs.
func TestKernelInit_LogsDoNotContainRawPrivateKeyMaterial(t *testing.T) {
	ResetInstance()
	defer ResetInstance()

	core, recorded := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	home := t.TempDir()
	t.Setenv("HOME", home)

	k, err := GetInstance(logger, t.TempDir())
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	_ = k.PublicKey()

	keyPath := filepath.Join(home, ".config", "aegisclaw", "kernel.key")
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read kernel key: %v", err)
	}
	forbidFull := strings.ToLower(hex.EncodeToString(raw))
	forbidSeed := strings.ToLower(hex.EncodeToString(raw[:32]))

	for _, ent := range recorded.All() {
		msg := ent.Message
		for _, f := range ent.Context {
			msg += " " + fmt.Sprintf("%+v", f)
		}
		lower := strings.ToLower(msg)
		if strings.Contains(lower, forbidFull) {
			t.Fatalf("log line contains full private key material: %q", ent.Message)
		}
		if strings.Contains(lower, forbidSeed) {
			t.Fatalf("log line contains private key seed material: %q", ent.Message)
		}
	}
}
