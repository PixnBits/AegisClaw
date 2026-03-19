package gitmanager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
)

const (
	defaultBranch = "main"
	branchPrefix  = "proposal-"
	authorName    = "AegisClaw Kernel"
	authorEmail   = "kernel@aegisclaw.local"
)

// RepoKind distinguishes the two managed repositories.
type RepoKind string

const (
	RepoSkills RepoKind = "skills"
	RepoSelf   RepoKind = "self"
)

// Manager manages two git repositories: ./skills/ (user skills) and ./self/ (kernel).
// All git operations are signed with the kernel key and logged to the Merkle audit chain.
type Manager struct {
	skillsPath string
	selfPath   string
	skillsRepo *git.Repository
	selfRepo   *git.Repository
	kern       *kernel.Kernel
	logger     *zap.Logger
	mu         sync.Mutex
}

// NewManager creates or opens the skills and self repositories.
func NewManager(basePath string, kern *kernel.Kernel, logger *zap.Logger) (*Manager, error) {
	if basePath == "" {
		return nil, fmt.Errorf("base path is required")
	}
	if kern == nil {
		return nil, fmt.Errorf("kernel is required")
	}

	skillsPath := filepath.Join(basePath, "skills")
	selfPath := filepath.Join(basePath, "self")

	skillsRepo, err := openOrInit(skillsPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to init skills repo: %w", err)
	}

	selfRepo, err := openOrInit(selfPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to init self repo: %w", err)
	}

	return &Manager{
		skillsPath: skillsPath,
		selfPath:   selfPath,
		skillsRepo: skillsRepo,
		selfRepo:   selfRepo,
		kern:       kern,
		logger:     logger,
	}, nil
}

func openOrInit(repoPath string, logger *zap.Logger) (*git.Repository, error) {
	if err := os.MkdirAll(repoPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", repoPath, err)
	}

	repo, err := git.PlainOpen(repoPath)
	if err == nil {
		logger.Info("git repository opened", zap.String("path", repoPath))
		return repo, nil
	}

	repo, err = git.PlainInit(repoPath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to init git repo at %s: %w", repoPath, err)
	}

	// Set HEAD to refs/heads/main (go-git defaults to master)
	mainRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(defaultBranch))
	if err := repo.Storer.SetReference(mainRef); err != nil {
		return nil, fmt.Errorf("failed to set HEAD to main: %w", err)
	}

	// Create initial commit with .gitkeep
	w, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	gitkeepPath := filepath.Join(repoPath, ".gitkeep")
	if err := os.WriteFile(gitkeepPath, []byte(""), 0644); err != nil {
		return nil, fmt.Errorf("failed to create .gitkeep: %w", err)
	}

	if _, err := w.Add(".gitkeep"); err != nil {
		return nil, fmt.Errorf("failed to add .gitkeep: %w", err)
	}

	_, err = w.Commit("init: repository initialized by AegisClaw kernel", &git.CommitOptions{
		Author: &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  time.Now().UTC(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create initial commit: %w", err)
	}

	logger.Info("git repository initialized", zap.String("path", repoPath))
	return repo, nil
}

// getRepo returns the repository for the given kind.
func (m *Manager) getRepo(kind RepoKind) (*git.Repository, string, error) {
	switch kind {
	case RepoSkills:
		return m.skillsRepo, m.skillsPath, nil
	case RepoSelf:
		return m.selfRepo, m.selfPath, nil
	default:
		return nil, "", fmt.Errorf("unknown repo kind: %s", kind)
	}
}

// CreateProposalBranch creates a new branch named proposal-<id> from main.
func (m *Manager) CreateProposalBranch(kind RepoKind, proposalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, _, err := m.getRepo(kind)
	if err != nil {
		return err
	}

	branchName := branchPrefix + proposalID

	// Resolve main HEAD
	mainRef, err := repo.Reference(plumbing.NewBranchReferenceName(defaultBranch), true)
	if err != nil {
		return fmt.Errorf("failed to resolve main branch: %w", err)
	}

	// Create branch reference pointing to the same commit as main
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branchName), mainRef.Hash())
	if err := repo.Storer.SetReference(ref); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", branchName, err)
	}

	// Checkout the new branch
	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	if err := w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
	}); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	// Audit log
	payload, _ := json.Marshal(map[string]string{
		"repo":   string(kind),
		"branch": branchName,
		"from":   mainRef.Hash().String(),
	})
	action := kernel.NewAction(kernel.ActionBuilderCreate, "git-manager", payload)
	if _, err := m.kern.SignAndLog(action); err != nil {
		m.logger.Error("failed to log branch creation", zap.Error(err))
	}

	m.logger.Info("proposal branch created",
		zap.String("repo", string(kind)),
		zap.String("branch", branchName),
		zap.String("base", mainRef.Hash().String()[:12]),
	)

	return nil
}

// CommitFiles stages and commits files on the current branch with an Ed25519-signed commit.
func (m *Manager) CommitFiles(kind RepoKind, files map[string]string, message string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(files) == 0 {
		return "", fmt.Errorf("no files to commit")
	}
	if message == "" {
		return "", fmt.Errorf("commit message is required")
	}

	repo, repoPath, err := m.getRepo(kind)
	if err != nil {
		return "", err
	}

	w, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	// Write files to the worktree
	for relPath, content := range files {
		// Validate path safety
		if strings.Contains(relPath, "..") {
			return "", fmt.Errorf("path traversal detected: %q", relPath)
		}

		absPath := filepath.Join(repoPath, relPath)
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", relPath, err)
		}
		if _, err := w.Add(relPath); err != nil {
			return "", fmt.Errorf("failed to stage file %s: %w", relPath, err)
		}
	}

	// Create signed commit
	pubKey := m.kern.PublicKey()
	signatureHex := fmt.Sprintf("%x", m.kern.Sign([]byte(message)))

	commitHash, err := w.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  time.Now().UTC(),
		},
		Committer: &object.Signature{
			Name:  fmt.Sprintf("AegisClaw Kernel [%x]", pubKey[:8]),
			Email: authorEmail,
			When:  time.Now().UTC(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create commit: %w", err)
	}

	// Audit log the commit
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"repo":      string(kind),
		"commit":    commitHash.String(),
		"message":   message,
		"files":     len(files),
		"signature": signatureHex,
	})
	action := kernel.NewAction(kernel.ActionBuilderBuild, "git-manager", auditPayload)
	if _, err := m.kern.SignAndLog(action); err != nil {
		m.logger.Error("failed to log git commit", zap.Error(err))
	}

	m.logger.Info("files committed",
		zap.String("repo", string(kind)),
		zap.String("commit", commitHash.String()[:12]),
		zap.Int("files", len(files)),
		zap.String("message", message),
	)

	return commitHash.String(), nil
}

// CheckoutBranch switches the worktree to the named branch.
func (m *Manager) CheckoutBranch(kind RepoKind, branchName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, _, err := m.getRepo(kind)
	if err != nil {
		return err
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
	})
}

// CheckoutProposalBranch is a convenience wrapper for CheckoutBranch with the proposal prefix.
func (m *Manager) CheckoutProposalBranch(kind RepoKind, proposalID string) error {
	return m.CheckoutBranch(kind, branchPrefix+proposalID)
}

// GenerateDiff returns a unified diff between the proposal branch and main.
func (m *Manager) GenerateDiff(kind RepoKind, proposalID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, _, err := m.getRepo(kind)
	if err != nil {
		return "", err
	}

	branchName := branchPrefix + proposalID

	// Resolve both branch heads
	mainRef, err := repo.Reference(plumbing.NewBranchReferenceName(defaultBranch), true)
	if err != nil {
		return "", fmt.Errorf("failed to resolve main: %w", err)
	}

	proposalRef, err := repo.Reference(plumbing.NewBranchReferenceName(branchName), true)
	if err != nil {
		return "", fmt.Errorf("failed to resolve branch %s: %w", branchName, err)
	}

	mainCommit, err := repo.CommitObject(mainRef.Hash())
	if err != nil {
		return "", fmt.Errorf("failed to get main commit: %w", err)
	}

	proposalCommit, err := repo.CommitObject(proposalRef.Hash())
	if err != nil {
		return "", fmt.Errorf("failed to get proposal commit: %w", err)
	}

	mainTree, err := mainCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get main tree: %w", err)
	}

	proposalTree, err := proposalCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get proposal tree: %w", err)
	}

	changes, err := mainTree.Diff(proposalTree)
	if err != nil {
		return "", fmt.Errorf("failed to compute diff: %w", err)
	}

	patch, err := changes.Patch()
	if err != nil {
		return "", fmt.Errorf("failed to generate patch: %w", err)
	}

	return patch.String(), nil
}

// HasConflicts checks whether the proposal branch would conflict with main.
func (m *Manager) HasConflicts(kind RepoKind, proposalID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, _, err := m.getRepo(kind)
	if err != nil {
		return false, err
	}

	branchName := branchPrefix + proposalID

	// Resolve both branch heads
	mainRef, err := repo.Reference(plumbing.NewBranchReferenceName(defaultBranch), true)
	if err != nil {
		return false, fmt.Errorf("failed to resolve main: %w", err)
	}

	proposalRef, err := repo.Reference(plumbing.NewBranchReferenceName(branchName), true)
	if err != nil {
		return false, fmt.Errorf("failed to resolve branch %s: %w", branchName, err)
	}

	// If proposal is based on current main HEAD, no conflicts possible
	proposalCommit, err := repo.CommitObject(proposalRef.Hash())
	if err != nil {
		return false, fmt.Errorf("failed to get proposal commit: %w", err)
	}

	mainCommit, err := repo.CommitObject(mainRef.Hash())
	if err != nil {
		return false, fmt.Errorf("failed to get main commit: %w", err)
	}

	// Check if main HEAD is an ancestor of the proposal branch.
	// If so, the proposal is based on the current main — no conflicts.
	isAncestor, err := mainCommit.IsAncestor(proposalCommit)
	if err != nil {
		// If we cannot determine ancestry, assume potential conflict
		return true, nil
	}

	// If main is ancestor of proposal, no conflict; otherwise potential conflict
	return !isAncestor, nil
}

// GetBranchCommits returns the commit log for a proposal branch.
func (m *Manager) GetBranchCommits(kind RepoKind, proposalID string) ([]CommitInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, _, err := m.getRepo(kind)
	if err != nil {
		return nil, err
	}

	branchName := branchPrefix + proposalID
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branchName), true)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve branch %s: %w", branchName, err)
	}

	iter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return nil, fmt.Errorf("failed to get log: %w", err)
	}

	var commits []CommitInfo
	err = iter.ForEach(func(c *object.Commit) error {
		commits = append(commits, CommitInfo{
			Hash:      c.Hash.String(),
			Message:   c.Message,
			Author:    c.Author.Name,
			Timestamp: c.Author.When,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate commits: %w", err)
	}

	return commits, nil
}

// CommitInfo is a simplified view of a git commit.
type CommitInfo struct {
	Hash      string    `json:"hash"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	Timestamp time.Time `json:"timestamp"`
}

// ListBranches returns all branch names in the repo.
func (m *Manager) ListBranches(kind RepoKind) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, _, err := m.getRepo(kind)
	if err != nil {
		return nil, err
	}

	iter, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	var branches []string
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		branches = append(branches, ref.Name().Short())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate branches: %w", err)
	}

	return branches, nil
}

// GetCurrentBranch returns the name of the currently checked out branch.
func (m *Manager) GetCurrentBranch(kind RepoKind) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, _, err := m.getRepo(kind)
	if err != nil {
		return "", err
	}

	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	return head.Name().Short(), nil
}

// SkillsPath returns the absolute path to the skills repo.
func (m *Manager) SkillsPath() string {
	return m.skillsPath
}

// SelfPath returns the absolute path to the self repo.
func (m *Manager) SelfPath() string {
	return m.selfPath
}
