package proposal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
)

const (
	proposalFile  = "proposal.json"
	defaultBranch = "main"
)

// Store manages proposal persistence in a git repository.
// Each proposal is stored as a JSON file on its own branch (proposal-<id>).
// The main branch holds an index of all proposals.
type Store struct {
	repoPath string
	repo     *git.Repository
	logger   *zap.Logger
	mu       sync.RWMutex
}

// ProposalIndex is the summary stored on the main branch.
type ProposalIndex struct {
	Proposals []ProposalSummary `json:"proposals"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// ProposalSummary stores essential info for listing without loading full proposals.
type ProposalSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Category    Category  `json:"category"`
	Status      Status    `json:"status"`
	Risk        RiskLevel `json:"risk"`
	Author      string    `json:"author"`
	Round       int       `json:"round"`
	TargetSkill string    `json:"target_skill,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewStore opens or creates a proposal git repository at the given path.
func NewStore(repoPath string, logger *zap.Logger) (*Store, error) {
	if repoPath == "" {
		return nil, fmt.Errorf("proposal store path is required")
	}

	if err := os.MkdirAll(repoPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create proposal store directory: %w", err)
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		repo, err = initRepo(repoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to init proposal repo: %w", err)
		}
		logger.Info("proposal git repository initialized", zap.String("path", repoPath))
	} else {
		logger.Info("proposal git repository opened", zap.String("path", repoPath))
	}

	return &Store{
		repoPath: repoPath,
		repo:     repo,
		logger:   logger,
	}, nil
}

// RepoPath returns the filesystem path to the proposal store repository.
func (s *Store) RepoPath() string {
	return s.repoPath
}

func initRepo(path string) (*git.Repository, error) {
	repo, err := git.PlainInit(path, false)
	if err != nil {
		return nil, err
	}

	// Set HEAD to refs/heads/main (go-git defaults to master)
	mainRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(defaultBranch))
	if err := repo.Storer.SetReference(mainRef); err != nil {
		return nil, fmt.Errorf("failed to set HEAD to main: %w", err)
	}

	// Create initial commit on main branch with empty index
	w, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	index := ProposalIndex{
		Proposals: []ProposalSummary{},
		UpdatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal initial index: %w", err)
	}

	indexPath := filepath.Join(path, "index.json")
	if err := os.WriteFile(indexPath, data, 0600); err != nil {
		return nil, fmt.Errorf("failed to write initial index: %w", err)
	}

	if _, err := w.Add("index.json"); err != nil {
		return nil, fmt.Errorf("failed to add index.json: %w", err)
	}

	_, err = w.Commit("chore: initialize proposal repository", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "AegisClaw Kernel",
			Email: "kernel@aegisclaw.local",
			When:  time.Now().UTC(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create initial commit: %w", err)
	}

	// Ensure main branch reference exists
	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD after commit: %w", err)
	}
	mainBranchRef := plumbing.NewBranchReferenceName(defaultBranch)
	ref := plumbing.NewHashReference(mainBranchRef, head.Hash())
	if err := repo.Storer.SetReference(ref); err != nil {
		return nil, fmt.Errorf("failed to create main branch reference: %w", err)
	}

	return repo, nil
}

// Create persists a new proposal on its own git branch.
func (s *Store) Create(p *Proposal) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid proposal: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	w, err := s.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Get main branch HEAD
	headRef, err := s.repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}

	// Create proposal branch from main
	branchRef := plumbing.NewBranchReferenceName(p.BranchName())
	ref := plumbing.NewHashReference(branchRef, headRef.Hash())
	if err := s.repo.Storer.SetReference(ref); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", p.BranchName(), err)
	}

	// Checkout proposal branch
	if err := w.Checkout(&git.CheckoutOptions{
		Branch: branchRef,
	}); err != nil {
		return fmt.Errorf("failed to checkout %s: %w", p.BranchName(), err)
	}

	// Write proposal file
	if err := s.writeProposalFile(p); err != nil {
		s.checkoutMain(w)
		return err
	}

	// Add and commit
	if _, err := w.Add(proposalFile); err != nil {
		s.checkoutMain(w)
		return fmt.Errorf("failed to add proposal file: %w", err)
	}

	_, err = w.Commit(fmt.Sprintf("feat: create proposal %s - %s", p.ID[:8], p.Title), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "AegisClaw Kernel",
			Email: "kernel@aegisclaw.local",
			When:  time.Now().UTC(),
		},
	})
	if err != nil {
		s.checkoutMain(w)
		return fmt.Errorf("failed to commit proposal: %w", err)
	}

	// Update index on main
	if err := s.updateIndex(w, p); err != nil {
		return err
	}

	s.logger.Info("proposal created",
		zap.String("proposal_id", p.ID),
		zap.String("branch", p.BranchName()),
		zap.String("title", p.Title),
	)
	return nil
}

// Update persists changes to an existing proposal on its branch.
func (s *Store) Update(p *Proposal) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid proposal: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	w, err := s.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	branchRef := plumbing.NewBranchReferenceName(p.BranchName())

	// Assume branch exists (created in Create), checkout with reset if needed
	if err := w.Checkout(&git.CheckoutOptions{Branch: branchRef}); err != nil {
		// If worktree has unstaged changes, try to reset
		if resetErr := w.Reset(&git.ResetOptions{Mode: git.HardReset}); resetErr != nil {
			return fmt.Errorf("failed to reset worktree: %w", resetErr)
		}
		// Try checkout again
		if checkoutErr := w.Checkout(&git.CheckoutOptions{Branch: branchRef}); checkoutErr != nil {
			return fmt.Errorf("failed to checkout %s after reset: %w", p.BranchName(), checkoutErr)
		}
	}

	if err := s.writeProposalFile(p); err != nil {
		s.checkoutMain(w)
		return err
	}

	if _, err := w.Add(proposalFile); err != nil {
		s.checkoutMain(w)
		return fmt.Errorf("failed to add proposal file: %w", err)
	}

	_, err = w.Commit(fmt.Sprintf("update: proposal %s status=%s v%d", p.ID[:8], p.Status, p.Version), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "AegisClaw Kernel",
			Email: "kernel@aegisclaw.local",
			When:  time.Now().UTC(),
		},
	})
	if err != nil {
		// A "clean" working tree means the data is identical to what's already
		// committed — treat this as a successful no-op rather than an error.
		if strings.Contains(err.Error(), "clean") {
			s.checkoutMain(w)
			s.logger.Debug("proposal update skipped (no changes)",
				zap.String("proposal_id", p.ID),
				zap.Int("version", p.Version),
			)
			return nil
		}
		s.checkoutMain(w)
		return fmt.Errorf("failed to commit proposal update: %w", err)
	}

	if err := s.updateIndex(w, p); err != nil {
		return err
	}

	s.logger.Info("proposal updated",
		zap.String("proposal_id", p.ID),
		zap.String("status", string(p.Status)),
		zap.Int("version", p.Version),
	)
	return nil
}

// Get loads a proposal from its git branch.
func (s *Store) Get(id string) (*Proposal, error) {
	if id == "" {
		return nil, fmt.Errorf("proposal ID is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	branchName := fmt.Sprintf("proposal-%s", id)
	branchRef := plumbing.NewBranchReferenceName(branchName)

	ref, err := s.repo.Reference(branchRef, true)
	if err != nil {
		return nil, fmt.Errorf("proposal %s not found: %w", id, err)
	}

	commit, err := s.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit for proposal %s: %w", id, err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get tree for proposal %s: %w", id, err)
	}

	file, err := tree.File(proposalFile)
	if err != nil {
		return nil, fmt.Errorf("proposal.json not found on branch %s: %w", branchName, err)
	}

	content, err := file.Contents()
	if err != nil {
		return nil, fmt.Errorf("failed to read proposal.json: %w", err)
	}

	return UnmarshalProposal([]byte(content))
}

// List returns summaries of all proposals from the index.
func (s *Store) List() ([]ProposalSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	index, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	return index.Proposals, nil
}

// ListByStatus returns summaries filtered by status.
func (s *Store) ListByStatus(status Status) ([]ProposalSummary, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var result []ProposalSummary
	for _, p := range all {
		if p.Status == status {
			result = append(result, p)
		}
	}
	return result, nil
}

// Import upserts a proposal into the store. If the proposal already exists
// on a branch, it is updated; otherwise a new branch is created.
// This is used by the daemon to receive proposals from unprivileged CLI
// clients that maintain their own local store.
func (s *Store) Import(p *Proposal) error {
	if _, err := s.Get(p.ID); err != nil {
		return s.Create(p)
	}
	return s.Update(p)
}

func (s *Store) writeProposalFile(p *Proposal) error {
	data, err := p.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal proposal: %w", err)
	}
	path := filepath.Join(s.repoPath, proposalFile)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write proposal file: %w", err)
	}
	return nil
}

func (s *Store) updateIndex(w *git.Worktree, p *Proposal) error {
	mainRef := plumbing.NewBranchReferenceName(defaultBranch)
	if err := w.Checkout(&git.CheckoutOptions{Branch: mainRef}); err != nil {
		return fmt.Errorf("failed to checkout main: %w", err)
	}

	index, err := s.readIndex()
	if err != nil {
		index = &ProposalIndex{Proposals: []ProposalSummary{}}
	}

	summary := ProposalSummary{
		ID:          p.ID,
		Title:       p.Title,
		Category:    p.Category,
		Status:      p.Status,
		Risk:        p.Risk,
		Author:      p.Author,
		Round:       p.Round,
		TargetSkill: p.TargetSkill,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}

	found := false
	for i, existing := range index.Proposals {
		if existing.ID == p.ID {
			index.Proposals[i] = summary
			found = true
			break
		}
	}
	if !found {
		index.Proposals = append(index.Proposals, summary)
	}

	sort.Slice(index.Proposals, func(i, j int) bool {
		return index.Proposals[i].UpdatedAt.After(index.Proposals[j].UpdatedAt)
	})

	index.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	indexPath := filepath.Join(s.repoPath, "index.json")
	if err := os.WriteFile(indexPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	if _, err := w.Add("index.json"); err != nil {
		return fmt.Errorf("failed to add index.json: %w", err)
	}

	_, err = w.Commit(fmt.Sprintf("chore: update index for proposal %s", p.ID[:8]), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "AegisClaw Kernel",
			Email: "kernel@aegisclaw.local",
			When:  time.Now().UTC(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to commit index update: %w", err)
	}

	return nil
}

func (s *Store) readIndex() (*ProposalIndex, error) {
	mainRef := plumbing.NewBranchReferenceName(defaultBranch)
	ref, err := s.repo.Reference(mainRef, true)
	if err != nil {
		return nil, fmt.Errorf("main branch not found: %w", err)
	}

	commit, err := s.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get main commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get main tree: %w", err)
	}

	file, err := tree.File("index.json")
	if err != nil {
		return nil, fmt.Errorf("index.json not found: %w", err)
	}

	content, err := file.Contents()
	if err != nil {
		return nil, fmt.Errorf("failed to read index.json: %w", err)
	}

	var index ProposalIndex
	if err := json.Unmarshal([]byte(content), &index); err != nil {
		return nil, fmt.Errorf("failed to unmarshal index: %w", err)
	}

	return &index, nil
}

func (s *Store) checkoutMain(w *git.Worktree) {
	mainRef := plumbing.NewBranchReferenceName(defaultBranch)
	if err := w.Checkout(&git.CheckoutOptions{Branch: mainRef}); err != nil {
		s.logger.Error("failed to checkout main after error", zap.Error(err))
	}
}

// ResolveID expands a prefix (or full ID) to the full proposal ID.
// Returns the full ID or an error if zero or multiple proposals match.
func (s *Store) ResolveID(prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("proposal ID is required")
	}

	summaries, err := s.List()
	if err != nil {
		return "", err
	}

	// 1. Exact UUID match.
	for _, p := range summaries {
		if p.ID == prefix {
			return prefix, nil
		}
	}

	// 2. UUID prefix match (minimum 4 chars).
	var prefixMatches []string
	if len(prefix) >= 4 {
		for _, p := range summaries {
			if len(p.ID) > len(prefix) && p.ID[:len(prefix)] == prefix {
				prefixMatches = append(prefixMatches, p.ID)
			}
		}
	}
	if len(prefixMatches) == 1 {
		return prefixMatches[0], nil
	}
	if len(prefixMatches) > 1 {
		return "", fmt.Errorf("ambiguous prefix %q matches %d proposals", prefix, len(prefixMatches))
	}

	// 3. Title or skill-name match (case-insensitive, for LLM-friendly lookups).
	norm := strings.ToLower(strings.TrimSpace(prefix))
	var titleMatches []string
	for _, p := range summaries {
		if strings.EqualFold(p.Title, norm) ||
			strings.EqualFold(p.TargetSkill, norm) ||
			slugMatch(p.Title, norm) {
			titleMatches = append(titleMatches, p.ID)
		}
	}
	if len(titleMatches) == 1 {
		return titleMatches[0], nil
	}
	if len(titleMatches) > 1 {
		return "", fmt.Errorf("ambiguous name %q matches %d proposals", prefix, len(titleMatches))
	}

	return "", fmt.Errorf("no proposal found matching %q", prefix)
}

// slugMatch returns true if the slugified title equals the query.
// e.g. "Time Telling Skill" matches "time-telling-skill" or "time telling skill".
func slugMatch(title, query string) bool {
	slug := strings.ToLower(title)
	slug = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, slug)
	// Collapse runs of dashes and trim.
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")

	// Also normalise the query the same way.
	normQ := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, query)
	for strings.Contains(normQ, "--") {
		normQ = strings.ReplaceAll(normQ, "--", "-")
	}
	normQ = strings.Trim(normQ, "-")

	return slug == normQ
}
