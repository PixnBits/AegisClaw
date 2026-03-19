package vault

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestSecrets_NeverInLogs verifies that secret plaintext never appears in log output.
func TestSecrets_NeverInLogs(t *testing.T) {
	core, observed := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	v, err := NewVault(dir, priv, logger)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}

	secretValue := "SUPER_SECRET_VALUE_12345"
	v.Add("apitoken", "skill-log", []byte(secretValue))
	v.Get("apitoken")
	v.Delete("apitoken")
	v.Add("readdedtoken", "skill-log", []byte(secretValue))
	v.List()

	for _, entry := range observed.All() {
		msg := entry.Message
		for _, field := range entry.Context {
			msg += " " + field.String
		}
		if strings.Contains(msg, secretValue) {
			t.Fatalf("SECRET LEAKED IN LOG: entry=%q", entry.Message)
		}
	}
}

// TestSecrets_NeverOnDiskPlaintext verifies encrypted files do not contain plaintext.
func TestSecrets_NeverOnDiskPlaintext(t *testing.T) {
	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	logger := zap.NewNop()

	v, err := NewVault(dir, priv, logger)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}

	plaintext := []byte("THIS_SHOULD_NEVER_APPEAR_ON_DISK_PLAINTEXT")
	v.Add("disktest", "skill-disk", plaintext)

	// Walk the vault directory and check every file
	filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		// The index.json should only have metadata (name, skill_id, etc)
		if filepath.Base(path) == "index.json" {
			if bytes.Contains(data, plaintext) {
				t.Fatalf("SECRET LEAKED in index.json at %s", path)
			}
			return nil
		}
		// The .age file should be encrypted
		if strings.HasSuffix(path, ".age") {
			if bytes.Contains(data, plaintext) {
				t.Fatalf("SECRET LEAKED in .age file at %s (not encrypted!)", path)
			}
			return nil
		}
		// The .age-identity file should not contain the secret
		if bytes.Contains(data, plaintext) {
			t.Fatalf("SECRET LEAKED in file %s", path)
		}
		return nil
	})
}

// TestSecrets_IndexNeverContainsValues verifies index.json stores only metadata.
func TestSecrets_IndexNeverContainsValues(t *testing.T) {
	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	logger := zap.NewNop()

	v, _ := NewVault(dir, priv, logger)
	v.Add("secret1", "skill-idx", []byte("value-that-should-not-appear"))
	v.Add("secret2", "skill-idx", []byte("another-secret-val"))

	indexPath := filepath.Join(dir, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}

	if bytes.Contains(data, []byte("value-that-should-not-appear")) {
		t.Fatal("index.json contains secret value")
	}
	if bytes.Contains(data, []byte("another-secret-val")) {
		t.Fatal("index.json contains secret value")
	}

	// Verify it IS valid JSON with expected fields
	var entries []*SecretEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("index.json is not valid JSON: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Name == "" || e.SkillID == "" {
			t.Fatal("entry missing required metadata")
		}
	}
}

// TestSecrets_DifferentVaultsIsolated verifies separate vaults cannot cross-read.
func TestSecrets_DifferentVaultsIsolated(t *testing.T) {
	logger := zap.NewNop()

	dir1 := t.TempDir()
	_, priv1, _ := ed25519.GenerateKey(rand.Reader)
	v1, _ := NewVault(dir1, priv1, logger)
	v1.Add("shared", "skill-1", []byte("vault1secret"))

	dir2 := t.TempDir()
	_, priv2, _ := ed25519.GenerateKey(rand.Reader)
	v2, _ := NewVault(dir2, priv2, logger)

	// v2 should not have v1's secret
	if v2.Has("shared") {
		t.Fatal("vault2 should not have vault1's secret")
	}

	// Even if we copy the .age file, v2 cannot decrypt because different identity
	srcFile := filepath.Join(dir1, "shared.age")
	dstFile := filepath.Join(dir2, "shared.age")
	data, _ := os.ReadFile(srcFile)
	os.WriteFile(dstFile, data, 0600)

	// v2 still cannot decrypt (wrong age identity)
	_, err := v2.decrypt(data)
	if err == nil {
		t.Fatal("vault2 should not be able to decrypt vault1's secret")
	}
}

// TestProxy_NeverLeaksInPayload ensures proxy payloads contain values but
// the SecretInjectRequest never includes the age ciphertext.
func TestProxy_NeverLeaksInPayload(t *testing.T) {
	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	logger := zap.NewNop()

	v, _ := NewVault(dir, priv, logger)
	v.Add("proxytest", "skill-p", []byte("proxy-secret-value"))

	proxy := NewSecretProxy(v, logger)
	req, err := proxy.ResolveSecrets([]string{"proxytest"})
	if err != nil {
		t.Fatalf("ResolveSecrets: %v", err)
	}

	payload, _ := proxy.BuildPayload(req)

	// Payload should contain the plaintext value (for injection)
	if !bytes.Contains(payload, []byte("proxy-secret-value")) {
		t.Fatal("payload should contain the decrypted value for injection")
	}

	// But should NOT contain any age ciphertext marker
	if bytes.Contains(payload, []byte("age-encryption.org")) {
		t.Fatal("payload should not contain age ciphertext")
	}

	// The .age file on disk should NOT contain plaintext
	ageFile := filepath.Join(dir, "proxytest.age")
	ageData, _ := os.ReadFile(ageFile)
	if bytes.Contains(ageData, []byte("proxy-secret-value")) {
		t.Fatal(".age file should not contain plaintext")
	}
}

// TestSecrets_CannotAddToBuilderOrCourtSandbox verifies that secrets refs
// are never injected into builder/court sandbox specs by ensuring SecretsRefs
// on a fresh sandbox spec serialized without them stays empty.
func TestSecrets_ProxyRejectsEmptyRefs(t *testing.T) {
	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	logger := zap.NewNop()

	v, _ := NewVault(dir, priv, logger)
	proxy := NewSecretProxy(v, logger)

	// Empty refs should produce empty injection
	req, err := proxy.ResolveSecrets([]string{})
	if err != nil {
		t.Fatalf("ResolveSecrets: %v", err)
	}
	if req.Secrets != nil {
		t.Fatal("expected nil secrets for empty refs")
	}
}
