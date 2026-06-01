package bootargs

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDistributedVMKeyFromFileDoesNotZeroReturnedKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "vmkey")
	pub, privSeed, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(privSeed)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_VM_PRIVATE_KEY_PATH", keyPath)
	t.Setenv("AEGIS_VM_PRIVATE_KEY_HEX", "")

	priv, gotPub, err := LoadDistributedVMKey("test")
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPub) != string(pub) {
		t.Fatal("public key mismatch")
	}
	allZero := true
	for _, b := range priv {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("returned private key was zeroed (pub/sign mismatch)")
	}
	if !roundtripSignVerify(t, priv, gotPub) {
		t.Fatal("signature does not verify with returned keypair")
	}
}

func TestLoadDistributedVMKeyPrefersFileOverCmdlineHex(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "vmkey")
	_, privA, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubB, privB, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(privB)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_VM_PRIVATE_KEY_PATH", keyPath)
	t.Setenv("AEGIS_VM_PRIVATE_KEY_HEX", hex.EncodeToString(privA))

	_, gotPub, err := LoadDistributedVMKey("test")
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPub) != string(pubB) {
		t.Fatal("expected key from injected file, not cmdline hex")
	}
}

func roundtripSignVerify(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey) bool {
	t.Helper()
	type msg struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		Command     string `json:"command"`
		Timestamp   string `json:"timestamp"`
		Signature   string `json:"signature"`
	}
	m := msg{Source: "test", Destination: "hub", Command: "ping", Timestamp: "2026-01-01T00:00:00Z"}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, data)
	return ed25519.Verify(pub, data, sig)
}
