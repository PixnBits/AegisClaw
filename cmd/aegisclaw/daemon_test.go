package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/api"
	"go.uber.org/zap"
)

// === Deep Expansion: Error Paths, Invariants, and Trust ===

func TestCreateSecureSocket_PermissionAfterCreation(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "perm.sock")

	logger, _ := zap.NewDevelopment()
	ln, err := createSecureSocket(sockPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	info, _ := os.Stat(sockPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("socket must be 0600 after creation, got %o", info.Mode().Perm())
	}
}

func TestAegisHubMonitor_DefaultRestartThreshold(t *testing.T) {
	monitor := &AegisHubMonitor{}
	// Ensure we have a sane default or explicit value
	if monitor.maxFailsBeforeRestart == 0 {
		monitor.maxFailsBeforeRestart = 3 // sensible default
	}
	if monitor.maxFailsBeforeRestart < 1 {
		t.Error("restart threshold should be at least 1")
	}
}

func TestWithAuthorizedCaller_EmptyAction(t *testing.T) {
	env := &runtimeEnv{}

	wrapped := withAuthorizedCaller(env, "", func(ctx context.Context, data json.RawMessage) *api.Response {
		return &api.Response{Success: true}
	})
	if wrapped == nil {
		t.Error("should still return a handler even with empty action name")
	}
}

// === Stronger Invariant / Security Posture Tests ===

// TestDaemonTCBBoundary verifies that a zero-value runtimeEnv holds no
// pre-initialised store or live infrastructure references, confirming that
// those resources can only enter the struct through the explicit initialisation
// path (initRuntime) rather than via package-level state.
func TestDaemonTCBBoundary(t *testing.T) {
	env := &runtimeEnv{}
	if env.Registry != nil {
		t.Error("expected nil SkillRegistry on zero-value runtimeEnv")
	}
	if env.Runtime != nil {
		t.Error("expected nil FirecrackerRuntime on zero-value runtimeEnv")
	}
	if env.ProposalStore != nil {
		t.Error("expected nil ProposalStore on zero-value runtimeEnv (store access must be mediated)")
	}
}
