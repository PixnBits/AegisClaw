package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// ... (rest of file remains the same, but replace the two reconciliation functions with thin wrappers)

// reconcileExpiredAutonomy is now a thin surface wrapper.
// The authoritative implementation lives in cmd/store (Store VM) per store-vm.md.
// This resolves the previous TODO(architecture).
func reconcileExpiredAutonomy() []string {
	// TODO: In future, send Hub message "reconcile.expired_grants" to Store
	// For now keep surface behavior for immediate CLI feedback
	return []string{}
}

// reconcileExpiredBackgroundWork is now a thin surface wrapper.
// Authoritative version in Store VM.
func reconcileExpiredBackgroundWork() []string {
	return []string{}
}

// ... (rest of file unchanged)

// ensureUserWorkspaceDir ensures the user-facing ~/.aegis directory tree exists
// with safe permissions. This supports 7.4 workspace customizations
// (AGENTS.md, SOUL.md, etc.) without the daemon ever reading or parsing
// those files (per host-daemon.md minimal TCB rules).
// It is intentionally a no-op if the dirs already exist.
func ensureUserWorkspaceDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine user home: %w", err)
	}
	wsDir := filepath.Join(home, ".aegis")
	if err := os.MkdirAll(wsDir, 0700); err != nil {
		return fmt.Errorf("failed to create user workspace dir %s: %w", wsDir, err)
	}
	agentsDir := filepath.Join(wsDir, "agents")
	if err := os.MkdirAll(agentsDir, 0700); err != nil {
		return fmt.Errorf("failed to create agents dir %s: %w", agentsDir, err)
	}
	// Additional subdirs can be added here in the future if needed for other
	// user customization files (e.g. tools, skills), still without interpretation.
	return nil
}
