package pullrequest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Store manages pull request persistence using filesystem storage.
// PRs are stored as JSON files in <storePath>/<pr-id>.json
type Store struct {
	storePath string
	logger    *zap.Logger
	mu        sync.RWMutex
	// In-memory cache for quick lookups
	cache map[string]*PullRequest
}

// NewStore creates a new PR store at the given path.
func NewStore(storePath string, logger *zap.Logger) (*Store, error) {
	if storePath == "" {
		return nil, fmt.Errorf("PR store path is required")
	}
	
	if err := os.MkdirAll(storePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create PR store directory: %w", err)
	}
	
	s := &Store{
		storePath: storePath,
		logger:    logger,
		cache:     make(map[string]*PullRequest),
	}
	
	// Load existing PRs into cache
	if err := s.loadCache(); err != nil {
		logger.Warn("failed to load PR cache", zap.Error(err))
	}
	
	logger.Info("PR store initialized", zap.String("path", storePath), zap.Int("cached_prs", len(s.cache)))
	return s, nil
}

// loadCache loads all PR files into memory cache.
func (s *Store) loadCache() error {
	entries, err := os.ReadDir(s.storePath)
	if err != nil {
		return fmt.Errorf("failed to read PR store: %w", err)
	}
	
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		
		prID := entry.Name()[:len(entry.Name())-5] // Remove .json
		pr, err := s.loadPR(prID)
		if err != nil {
			s.logger.Warn("failed to load PR", zap.String("id", prID), zap.Error(err))
			continue
		}
		s.cache[prID] = pr
	}
	
	return nil
}

// loadPR loads a single PR from disk.
func (s *Store) loadPR(id string) (*PullRequest, error) {
	path := filepath.Join(s.storePath, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("PR not found: %s", id)
		}
		return nil, fmt.Errorf("failed to read PR file: %w", err)
	}
	
	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse PR JSON: %w", err)
	}
	
	return &pr, nil
}

// savePR saves a PR to disk.
func (s *Store) savePR(pr *PullRequest) error {
	if err := pr.Validate(); err != nil {
		return fmt.Errorf("PR validation failed: %w", err)
	}
	
	path := filepath.Join(s.storePath, pr.ID+".json")
	data, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal PR: %w", err)
	}
	
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write PR file: %w", err)
	}
	
	return nil
}

// Create creates a new pull request.
func (s *Store) Create(pr *PullRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if _, exists := s.cache[pr.ID]; exists {
		return fmt.Errorf("PR already exists: %s", pr.ID)
	}
	
	if err := s.savePR(pr); err != nil {
		return err
	}
	
	s.cache[pr.ID] = pr
	s.logger.Info("PR created", zap.String("id", pr.ID), zap.String("proposal_id", pr.ProposalID))
	return nil
}

// Update updates an existing pull request.
func (s *Store) Update(pr *PullRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if _, exists := s.cache[pr.ID]; !exists {
		return fmt.Errorf("PR not found: %s", pr.ID)
	}
	
	pr.UpdatedAt = time.Now().UTC()
	
	if err := s.savePR(pr); err != nil {
		return err
	}
	
	s.cache[pr.ID] = pr
	s.logger.Info("PR updated", zap.String("id", pr.ID))
	return nil
}

// Get retrieves a pull request by ID.
func (s *Store) Get(id string) (*PullRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	pr, exists := s.cache[id]
	if !exists {
		return nil, fmt.Errorf("PR not found: %s", id)
	}
	
	// Return a copy to prevent external modifications
	prCopy := *pr
	return &prCopy, nil
}

// GetByProposalID retrieves a pull request by proposal ID.
func (s *Store) GetByProposalID(proposalID string) (*PullRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	for _, pr := range s.cache {
		if pr.ProposalID == proposalID {
			prCopy := *pr
			return &prCopy, nil
		}
	}
	
	return nil, fmt.Errorf("PR not found for proposal: %s", proposalID)
}

// List returns all pull requests, optionally filtered by status.
func (s *Store) List(status *Status) ([]*PullRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	var prs []*PullRequest
	for _, pr := range s.cache {
		if status == nil || pr.Status == *status {
			prCopy := *pr
			prs = append(prs, &prCopy)
		}
	}
	
	// Sort by created time, newest first
	sort.Slice(prs, func(i, j int) bool {
		return prs[i].CreatedAt.After(prs[j].CreatedAt)
	})
	
	return prs, nil
}

// Delete removes a pull request from the store.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if _, exists := s.cache[id]; !exists {
		return fmt.Errorf("PR not found: %s", id)
	}
	
	path := filepath.Join(s.storePath, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete PR file: %w", err)
	}
	
	delete(s.cache, id)
	s.logger.Info("PR deleted", zap.String("id", id))
	return nil
}

// AddCourtReview adds a court persona review to a PR.
func (s *Store) AddCourtReview(prID string, review CourtReview) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	pr, exists := s.cache[prID]
	if !exists {
		return fmt.Errorf("PR not found: %s", prID)
	}
	
	pr.CourtReviews = append(pr.CourtReviews, review)
	pr.UpdatedAt = time.Now().UTC()
	
	// Update court review status based on reviews
	s.updateCourtReviewStatus(pr)
	
	if err := s.savePR(pr); err != nil {
		return err
	}
	
	s.logger.Info("Court review added to PR",
		zap.String("pr_id", prID),
		zap.String("persona", review.Persona),
		zap.String("verdict", review.Verdict),
	)
	return nil
}

// updateCourtReviewStatus updates the overall court review status based on individual reviews.
func (s *Store) updateCourtReviewStatus(pr *PullRequest) {
	if !pr.CourtReviewRequired {
		pr.CourtReviewStatus = CourtReviewSkipped
		return
	}
	
	if len(pr.CourtReviews) == 0 {
		pr.CourtReviewStatus = CourtReviewPending
		return
	}
	
	pr.CourtReviewStatus = CourtReviewInProgress
	
	// Check if all required personas have reviewed
	// For now, we consider it approved if majority approve and none reject
	approvals := 0
	rejections := 0
	
	for _, review := range pr.CourtReviews {
		if review.Verdict == "approve" {
			approvals++
		} else if review.Verdict == "reject" {
			rejections++
		}
	}
	
	if rejections > 0 {
		pr.CourtReviewStatus = CourtReviewRejected
	} else if approvals >= 3 { // Require at least 3 approvals
		pr.CourtReviewStatus = CourtReviewApproved
	}
}

// Approve marks a PR as approved by the user.
func (s *Store) Approve(prID, approvedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	pr, exists := s.cache[prID]
	if !exists {
		return fmt.Errorf("PR not found: %s", prID)
	}
	
	if pr.Status != StatusOpen {
		return fmt.Errorf("cannot approve PR with status: %s", pr.Status)
	}
	
	pr.Approved = true
	pr.ApprovedBy = approvedBy
	pr.ApprovedAt = time.Now().UTC()
	pr.UpdatedAt = pr.ApprovedAt
	
	if err := s.savePR(pr); err != nil {
		return err
	}
	
	s.logger.Info("PR approved", zap.String("pr_id", prID), zap.String("approved_by", approvedBy))
	return nil
}

// Close marks a PR as closed without merging.
func (s *Store) Close(prID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	pr, exists := s.cache[prID]
	if !exists {
		return fmt.Errorf("PR not found: %s", prID)
	}
	
	pr.Status = StatusClosed
	pr.ClosedAt = time.Now().UTC()
	pr.UpdatedAt = pr.ClosedAt
	
	if err := s.savePR(pr); err != nil {
		return err
	}
	
	s.logger.Info("PR closed", zap.String("pr_id", prID))
	return nil
}

// MarkMerged marks a PR as merged.
func (s *Store) MarkMerged(prID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	pr, exists := s.cache[prID]
	if !exists {
		return fmt.Errorf("PR not found: %s", prID)
	}
	
	pr.Status = StatusMerged
	pr.MergedAt = time.Now().UTC()
	pr.UpdatedAt = pr.MergedAt
	
	if err := s.savePR(pr); err != nil {
		return err
	}
	
	s.logger.Info("PR marked as merged", zap.String("pr_id", prID))
	return nil
}
