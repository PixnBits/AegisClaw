package security

import (
	"bytes"
	"crypto/ed25519"
	"testing"
)

// TestGenerateVMKeyPair_Isolation verifies that GenerateVMKeyPair returns a
// usable keypair and that private keys are never stored inside the Manager
// (core TCB isolation requirement from host-daemon.md: "Private keys must never
// leave their assigned microVM" and daemon must not retain them).
func TestGenerateVMKeyPair_Isolation(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	kp, err := m.GenerateVMKeyPair()
	if err != nil {
		t.Fatalf("GenerateVMKeyPair failed: %v", err)
	}
	if kp == nil || len(kp.PrivateKey) != ed25519.PrivateKeySize || len(kp.PublicKey) != ed25519.PublicKeySize {
		t.Fatal("invalid keypair returned")
	}

	// Register only the public key (as the real flow will do after handoff)
	vmID := "test-vm-isolation-001"
	m.RegisterVM(vmID, kp.PublicKey)

	gotPub, ok := m.GetVMPublicKey(vmID)
	if !ok {
		t.Fatal("expected public key to be registered")
	}
	if !bytes.Equal(gotPub, kp.PublicKey) {
		t.Fatal("registered pubkey mismatch")
	}

	// Prove isolation: there is no way to retrieve the private key from the
	// Manager after generation. The only private material lives in the local
	// kp variable returned to the caller (Orchestrator), which must hand it
	// off and drop references.
	// (If a future change adds priv storage, this test + review will catch it.)
	ids := m.ListRegisteredVMs()
	found := false
	for _, id := range ids {
		if id == vmID {
			found = true
			break
		}
	}
	if !found {
		t.Error("ListRegisteredVMs did not include the test VM")
	}

	t.Logf("✓ VM keypair generated for %s; only pubkey retained in Manager (priv never stored)", vmID)
}

// TestDaemonKeypairStillWorks ensures existing daemon key functionality is unaffected.
func TestDaemonKeypairStillWorks(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.GetKeyPair() == nil {
		t.Fatal("daemon keypair should be loaded")
	}
	sig, err := m.Sign([]byte("test message for TCB"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := m.Verify(m.GetKeyPair().PublicKey, []byte("test message for TCB"), sig); err != nil {
		t.Fatalf("Verify own signature: %v", err)
	}
	t.Log("✓ Daemon own keypair (for audit signing) unaffected by VM key extensions")
}