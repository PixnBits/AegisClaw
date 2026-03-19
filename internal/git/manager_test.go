package gitmanager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func setupTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")
	os.MkdirAll(auditDir, 0700)

	logger := zaptest.NewLogger(t)

	kernel.ResetInstance()
	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("failed to create kernel: %v", err)
	}

	basePath := filepath.Join(tmpDir, "repos")
	mgr, err := NewManager(basePath, kern, logger)
	if err != nil {
		t.Fatalf("failed to create git manager: %v", err)
	}

	return mgr, func() {
		kernel.ResetInstance()
	}
}

func TestNewManager(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	if mgr.SkillsPath() == "" {
		t.Error("expected non-empty skills path")
	}
	if mgr.SelfPath() == "" {
		t.Error("expected non-empty self path")
	}

	// Verify both repos exist
	if _, err := os.Stat(mgr.SkillsPath()); err != nil {
		t.Errorf("skills path does not exist: %v", err)
	}
	if _, err := os.Stat(mgr.SelfPath()); err != nil {
		t.Errorf("self path does not exist: %v", err)
	}
}

func TestNewManagerErrors(t *testing.T) {
	logger := zap.NewNop()

	_, err := NewManager("", nil, logger)
	if err == nil {
		t.Error("expected error for empty path")
	}

	_, err = NewManager("/tmp/test-git", nil, logger)
	if err == nil {
		t.Error("expected error for nil kernel")
	}
}

func TestCreateProposalBranch(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	err := mgr.CreateProposalBranch(RepoSkills, "test-123")
	if err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	branches, err := mgr.ListBranches(RepoSkills)
	if err != nil {
		t.Fatalf("failed to list branches: %v", err)
	}

	found := false
	for _, b := range branches {
		if b == "proposal-test-123" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected branch proposal-test-123 in %v", branches)
	}
}

func TestCommitFiles(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create a proposal branch
	err := mgr.CreateProposalBranch(RepoSkills, "commit-test")
	if err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	// Commit files
	files := map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {}\n",
	}

	hash, err := mgr.CommitFiles(RepoSkills, files, "add main.go and test")
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	if hash == "" {
		t.Error("expected non-empty commit hash")
	}
	if len(hash) != 40 {
		t.Errorf("expected 40-char hash, got %d: %s", len(hash), hash)
	}

	// Verify files exist on disk
	mainPath := filepath.Join(mgr.SkillsPath(), "main.go")
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("main.go not found after commit: %v", err)
	}
}

func TestCommitFilesValidation(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	_, err := mgr.CommitFiles(RepoSkills, nil, "test")
	if err == nil {
		t.Error("expected error for nil files")
	}

	_, err = mgr.CommitFiles(RepoSkills, map[string]string{"a": "b"}, "")
	if err == nil {
		t.Error("expected error for empty message")
	}

	_, err = mgr.CommitFiles(RepoSkills, map[string]string{"../evil": "data"}, "hack")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestGenerateDiff(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create branch and commit files
	err := mgr.CreateProposalBranch(RepoSkills, "diff-test")
	if err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	files := map[string]string{
		"hello.go": "package hello\n\nfunc Hello() string { return \"world\" }\n",
	}

	_, err = mgr.CommitFiles(RepoSkills, files, "add hello.go")
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Generate diff
	diff, err := mgr.GenerateDiff(RepoSkills, "diff-test")
	if err != nil {
		t.Fatalf("failed to generate diff: %v", err)
	}

	if diff == "" {
		t.Error("expected non-empty diff")
	}

	// The diff should mention hello.go
	if !containsSubstring(diff, "hello.go") {
		t.Errorf("expected diff to mention hello.go, got:\n%s", diff)
	}
}

func TestGetBranchCommits(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	err := mgr.CreateProposalBranch(RepoSkills, "log-test")
	if err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	// Two commits
	_, err = mgr.CommitFiles(RepoSkills, map[string]string{"a.txt": "first"}, "first commit")
	if err != nil {
		t.Fatalf("first commit failed: %v", err)
	}

	_, err = mgr.CommitFiles(RepoSkills, map[string]string{"b.txt": "second"}, "second commit")
	if err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	commits, err := mgr.GetBranchCommits(RepoSkills, "log-test")
	if err != nil {
		t.Fatalf("failed to get commits: %v", err)
	}

	// Should have at least 3 commits: init + 2 we added
	if len(commits) < 3 {
		t.Errorf("expected at least 3 commits, got %d", len(commits))
	}

	// Most recent first
	if !containsSubstring(commits[0].Message, "second commit") {
		t.Errorf("expected most recent commit to be 'second commit', got %q", commits[0].Message)
	}
}

func TestListBranches(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	branches, err := mgr.ListBranches(RepoSkills)
	if err != nil {
		t.Fatalf("failed to list branches: %v", err)
	}

	// Should have at least main
	found := false
	for _, b := range branches {
		if b == "main" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected main branch in %v", branches)
	}
}

func TestGetCurrentBranch(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// After init, should be on main
	// Actually after CreateProposalBranch, we are on the proposal branch
	err := mgr.CreateProposalBranch(RepoSkills, "branch-test")
	if err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	current, err := mgr.GetCurrentBranch(RepoSkills)
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}

	if current != "proposal-branch-test" {
		t.Errorf("expected proposal-branch-test, got %s", current)
	}
}

func TestCheckoutBranch(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	err := mgr.CreateProposalBranch(RepoSkills, "checkout-test")
	if err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	// Should be on proposal branch now
	err = mgr.CheckoutBranch(RepoSkills, "main")
	if err != nil {
		t.Fatalf("failed to checkout main: %v", err)
	}

	current, err := mgr.GetCurrentBranch(RepoSkills)
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}

	if current != "main" {
		t.Errorf("expected main, got %s", current)
	}
}

func TestHasConflicts(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	err := mgr.CreateProposalBranch(RepoSkills, "conflict-test")
	if err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	// With no divergence, no conflicts expected
	hasConflicts, err := mgr.HasConflicts(RepoSkills, "conflict-test")
	if err != nil {
		t.Fatalf("failed to check conflicts: %v", err)
	}

	if hasConflicts {
		t.Error("expected no conflicts for fresh branch")
	}
}

func TestSelfRepo(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create branch on self repo
	err := mgr.CreateProposalBranch(RepoSelf, "self-test")
	if err != nil {
		t.Fatalf("failed to create branch on self repo: %v", err)
	}

	files := map[string]string{
		"patch.go": "package kernel\n// self-improvement patch\n",
	}

	hash, err := mgr.CommitFiles(RepoSelf, files, "self-improvement test")
	if err != nil {
		t.Fatalf("failed to commit to self repo: %v", err)
	}

	if hash == "" {
		t.Error("expected non-empty commit hash for self repo")
	}
}

func TestInvalidRepoKind(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	err := mgr.CreateProposalBranch(RepoKind("invalid"), "test")
	if err == nil {
		t.Error("expected error for invalid repo kind")
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
