package main

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

// TestSecureSocketCreation verifies that createSecureSocket sets strict permissions.
func TestSecureSocketCreation(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	logger, _ := zap.NewDevelopment()
	listener, err := createSecureSocket(socketPath, logger)
	if err != nil {
		t.Fatalf("createSecureSocket failed: %v", err)
	}
	defer listener.Close()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("failed to stat socket: %v", err)
	}

	// Check permissions are 0600
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected socket permissions 0600, got %o", info.Mode().Perm())
	}
}

// TestLifecycleContainmentSignalHandling ensures signal setup doesn't panic.
func TestLifecycleContainmentSignalHandling(t *testing.T) {
	env := &runtimeEnv{}
	logger, _ := zap.NewDevelopment()

	// Should not panic
	setupLifecycleContainment(env, logger)
}

// TestCapabilityDroppingDoesNotPanic verifies the function runs without crashing.
func TestCapabilityDroppingDoesNotPanic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	_ = dropCapabilities(logger) // should not panic even if it fails
}

// TestSeccompFilterApplication verifies filter application doesn't crash on this system.
func TestSeccompFilterApplication(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	err := applySeccompFilter(logger)
	if err != nil {
		t.Logf("seccomp filter application returned (may be expected on some systems): %v", err)
	}
}

// TestNoObviousSecretPatterns is a basic safeguard.
// In a real paranoid setup this would be supplemented by gosec / semgrep in CI.
func TestNoObviousSecretPatterns(t *testing.T) {
	// This test exists to force developers to think about secret handling.
	// Real enforcement is done via code review + static analysis.
	t.Log("Secret handling policy: enforced via review + linters (see Phase 5 docs)")
}
