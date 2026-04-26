package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

// syncProposalsToWorkspace copies proposals from the host's proposal store
// into a builder workspace volume. This ensures the builder VM can see proposals
// created via the web portal or CLI.
func syncProposalsToWorkspace(workspacePath string, hostProposalStore string, logger *zap.Logger) error {
	// Check if host proposal store exists
	hostIndexPath := filepath.Join(hostProposalStore, "index.json")
	if _, err := os.Stat(hostIndexPath); os.IsNotExist(err) {
		logger.Debug("no host proposal store to sync",
			zap.String("path", hostProposalStore),
		)
		return nil // Not an error - just no proposals yet
	}

	// Create temporary mount point
	tmpMount := filepath.Join(os.TempDir(), fmt.Sprintf("aegis-workspace-%d", os.Getpid()))
	if err := os.MkdirAll(tmpMount, 0700); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}
	defer os.RemoveAll(tmpMount)

	// Mount workspace volume
	mountCmd := exec.Command("mount", "-o", "loop", workspacePath, tmpMount)
	if out, err := mountCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount workspace: %w: %s", err, out)
	}
	defer func() {
		exec.Command("umount", tmpMount).Run()
	}()

	// Ensure proposals directory exists in workspace and is a git repo
	wsProposalPath := filepath.Join(tmpMount, "proposals")
	if err := os.MkdirAll(wsProposalPath, 0700); err != nil {
		return fmt.Errorf("failed to create workspace proposals directory: %w", err)
	}

	// Copy the entire git repository from host to workspace
	// This preserves all branches, refs, and commit history
	hostGitDir := filepath.Join(hostProposalStore, ".git")
	wsGitDir := filepath.Join(wsProposalPath, ".git")

	// Remove existing .git if present to ensure clean copy
	os.RemoveAll(wsGitDir)

	// Copy .git directory recursively
	cpCmd := exec.Command("cp", "-r", hostGitDir, wsGitDir)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy git directory: %w: %s", err, out)
	}

	// Set git config for workspace repo
	gitCmd := exec.Command("git", "-C", wsProposalPath, "config", "user.email", "daemon@aegisclaw")
	gitCmd.Run()
	gitCmd = exec.Command("git", "-C", wsProposalPath, "config", "user.name", "AegisClaw Daemon")
	gitCmd.Run()

	// Checkout main branch to populate working directory
	checkoutCmd := exec.Command("git", "-C", wsProposalPath, "checkout", "main")
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout main: %w: %s", err, out)
	}

	// Read host proposal index
	hostData, err := os.ReadFile(hostIndexPath)
	if err != nil {
		return fmt.Errorf("failed to read host proposal index: %w", err)
	}

	// Parse to check for implementing proposals
	var hostIndex struct {
		Proposals []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal(hostData, &hostIndex); err != nil {
		return fmt.Errorf("failed to parse host proposal index: %w", err)
	}

	// Count implementing proposals
	implementingCount := 0
	for _, p := range hostIndex.Proposals {
		if p.Status == "implementing" {
			implementingCount++
		}
	}

	if implementingCount == 0 {
		logger.Debug("no implementing proposals to sync")
		return nil
	}

	logger.Info("synced proposals to builder workspace",
		zap.Int("implementing_count", implementingCount),
		zap.String("workspace", workspacePath),
	)

	return nil
}
