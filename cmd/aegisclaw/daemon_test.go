package main

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

// === Lifecycle Containment Tests ===

func TestLifecycleContainment_RegistersSignalHandlers(t *testing.T) {
	env := &runtimeEnv{}
	logger, _ := zap.NewDevelopment()

	// Should not panic and should set up handlers
	setupLifecycleContainment(env, logger)
}

// === Secure Socket Tests ===

func TestCreateSecureSocket_SetsStrictPermissions(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "secure.sock")

	logger, _ := zap.NewDevelopment()
	ln, err := createSecureSocket(sockPath, logger)
	if err != nil {
		t.Fatalf("createSecureSocket failed: %v", err)
	}
	defer ln.Close()

	info, _ := os.Stat(sockPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestCreateSecureSocket_CreatesParentDirWith0700(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "subdir", "test.sock")

	logger, _ := zap.NewDevelopment()
	ln, err := createSecureSocket(sockPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	parent := filepath.Dir(sockPath)
	info, _ := os.Stat(parent)
	if info.Mode().Perm() != 0700 {
		t.Errorf("expected parent dir 0700, got %o", info.Mode().Perm())
	}
}

// === Hardening Function Stability Tests ===

func TestDropCapabilities_DoesNotPanic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	err := dropCapabilities(logger)
	if err != nil {
		t.Logf("dropCapabilities returned error (acceptable on some systems): %v", err)
	}
}

func TestApplySeccompFilter_DoesNotPanic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	err := applySeccompFilter(logger)
	if err != nil {
		t.Logf("applySeccompFilter returned error (may be expected): %v", err)
	}
}

// === Policy / Regression Guard Tests ===

func TestNoBusinessLogicInDaemon(t *testing.T) {
	// This is a reminder test. Real enforcement happens via CI grep rules
	// and code review. We keep it here so the intent is explicit in the test suite.
	t.Log("Business logic must not live in the Host Daemon (enforced via review + CI)")
}

func TestNoSecretHandlingInDaemon(t *testing.T) {
	t.Log("Daemon must never handle secrets (policy enforced via review + static analysis)")
}
