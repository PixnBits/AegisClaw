package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestVaultRejectsLooseStorePermissionsOnAccess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	logger := zaptest.NewLogger(t)
	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := v.Add("apitoken", "skill-a", []byte("secret")); err == nil {
		t.Fatal("expected Add to refuse insecure vault directory permissions")
	}
	if v.Has("apitoken") {
		t.Fatal("Has should fail closed when vault directory permissions are insecure")
	}
}

func TestVaultRejectsSymlinkSecretRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	logger := zaptest.NewLogger(t)
	v, err := NewVault(dir, testKey(t), logger)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	if err := v.Add("apitoken", "skill-a", []byte("secret")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	target := filepath.Join(t.TempDir(), "target.age")
	if err := os.WriteFile(target, []byte("not age"), 0600); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(dir, "apitoken.age")
	if err := os.Remove(secretPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, secretPath); err != nil {
		t.Fatal(err)
	}

	_, err = v.Get("apitoken")
	if err == nil {
		t.Fatal("expected symlink secret file to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "too many levels") &&
		!strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("expected symlink/no-follow error, got %v", err)
	}
}

func TestVaultRejectsSymlinkIndexOnStartup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(target, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}

	_, err := NewVault(dir, testKey(t), zaptest.NewLogger(t))
	if err == nil {
		t.Fatal("expected NewVault to reject symlink index.json")
	}
}
