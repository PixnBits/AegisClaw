package pullrequest

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Status represents the lifecycle state of a pull request.
type Status string

const (
	StatusOpen   Status = "open"   // PR is open and awaiting review
	StatusMerged Status = "merged" // PR has been merged
	StatusClosed Status = "closed" // PR was closed without merging
)

// PullRequest represents a code review request for generated skill code.
// It is automatically created after the builder pipeline completes successfully.
type PullRequest struct {
	ID          string    `json:"id"`           // Unique PR identifier
	ProposalID  string    `json:"proposal_id"`  // Associated proposal
	Title       string    `json:"title"`        // PR title (from proposal)
	Description string    `json:"description"`  // PR description
	Status      Status    `json:"status"`       // Current status
	Branch      string    `json:"branch"`       // Source branch (proposal-<id>)
	BaseBranch  string    `json:"base_branch"`  // Target branch (usually "main")
	CommitHash  string    `json:"commit_hash"`  // Latest commit on the PR branch
	Author      string    `json:"author"`       // Proposal author
	CreatedAt   time.Time `json:"created_at"`   // When PR was created
	UpdatedAt   time.Time `json:"updated_at"`   // Last update time
	MergedAt    time.Time `json:"merged_at,omitempty"` // When PR was merged
	ClosedAt    time.Time `json:"closed_at,omitempty"` // When PR was closed
	
	// Build and security results
	BuildPassed         bool              `json:"build_passed"`
	AnalysisPassed      bool              `json:"analysis_passed"`
	SecurityGatesPassed bool              `json:"security_gates_passed"`
	FilesChanged        int               `json:"files_changed"`
	Additions           int               `json:"additions"`
	Deletions           int               `json:"deletions"`
	
	// Court review integration
	CourtReviewRequired bool              `json:"court_review_required"`
	CourtReviewStatus   CourtReviewStatus `json:"court_review_status"`
	CourtReviews        []CourtReview     `json:"court_reviews,omitempty"`
	
	// Approval tracking
	Approved   bool      `json:"approved"`
	ApprovedBy string    `json:"approved_by,omitempty"`
	ApprovedAt time.Time `json:"approved_at,omitempty"`
	
	// Metadata
	SBOMPath string `json:"sbom_path,omitempty"` // Path to SBOM file
	DiffSize int    `json:"diff_size"`           // Size of diff in bytes
}

// CourtReviewStatus tracks the state of Court review for a PR.
type CourtReviewStatus string

const (
	CourtReviewPending  CourtReviewStatus = "pending"   // Waiting for Court review
	CourtReviewInProgress CourtReviewStatus = "in_progress" // Court is reviewing
	CourtReviewApproved CourtReviewStatus = "approved" // Court approved the code
	CourtReviewRejected CourtReviewStatus = "rejected" // Court rejected the code
	CourtReviewSkipped  CourtReviewStatus = "skipped"  // Court review not required (low risk)
)

// CourtReview represents feedback from a Court persona on the generated code.
type CourtReview struct {
	ID        string    `json:"id"`
	Persona   string    `json:"persona"`    // Court persona name
	Verdict   string    `json:"verdict"`    // approve, reject, request_changes
	RiskScore float64   `json:"risk_score"` // 0-10 risk assessment
	Comments  string    `json:"comments"`   // Overall feedback
	Timestamp time.Time `json:"timestamp"`
	
	// Code-level comments (for future inline comment support)
	InlineComments []InlineComment `json:"inline_comments,omitempty"`
}

// InlineComment represents a comment on a specific line of code.
// This is for future Phase 4 enhancement - initially comments will be read-only.
type InlineComment struct {
	FilePath   string    `json:"file_path"`
	LineNumber int       `json:"line_number"`
	Comment    string    `json:"comment"`
	Severity   string    `json:"severity"` // info, warning, error
	Timestamp  time.Time `json:"timestamp"`
}

// NewPullRequest creates a new PR from builder pipeline results.
func NewPullRequest(proposalID, title, author, branch, commitHash string) (*PullRequest, error) {
	if proposalID == "" {
		return nil, fmt.Errorf("proposal ID is required")
	}
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if author == "" {
		return nil, fmt.Errorf("author is required")
	}
	if branch == "" {
		return nil, fmt.Errorf("branch is required")
	}
	if commitHash == "" {
		return nil, fmt.Errorf("commit hash is required")
	}
	
	now := time.Now().UTC()
	return &PullRequest{
		ID:          uuid.New().String(),
		ProposalID:  proposalID,
		Title:       title,
		Author:      author,
		Branch:      branch,
		BaseBranch:  "main",
		CommitHash:  commitHash,
		Status:      StatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
		CourtReviewStatus: CourtReviewPending,
	}, nil
}

// Validate ensures the PR has all required fields.
func (pr *PullRequest) Validate() error {
	if pr.ID == "" {
		return fmt.Errorf("PR ID is required")
	}
	if _, err := uuid.Parse(pr.ID); err != nil {
		return fmt.Errorf("PR ID is not a valid UUID: %w", err)
	}
	if pr.ProposalID == "" {
		return fmt.Errorf("proposal ID is required")
	}
	if pr.Title == "" {
		return fmt.Errorf("title is required")
	}
	if pr.Author == "" {
		return fmt.Errorf("author is required")
	}
	if pr.Branch == "" {
		return fmt.Errorf("branch is required")
	}
	if pr.CommitHash == "" {
		return fmt.Errorf("commit hash is required")
	}
	if pr.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	if pr.UpdatedAt.IsZero() {
		return fmt.Errorf("updated_at is required")
	}
	
	// Validate status
	switch pr.Status {
	case StatusOpen, StatusMerged, StatusClosed:
	default:
		return fmt.Errorf("invalid status: %q", pr.Status)
	}
	
	// Validate court review status
	switch pr.CourtReviewStatus {
	case CourtReviewPending, CourtReviewInProgress, CourtReviewApproved, CourtReviewRejected, CourtReviewSkipped:
	default:
		return fmt.Errorf("invalid court review status: %q", pr.CourtReviewStatus)
	}
	
	return nil
}

// CanMerge returns true if the PR meets all requirements for merging.
func (pr *PullRequest) CanMerge() bool {
	if pr.Status != StatusOpen {
		return false
	}
	if !pr.BuildPassed || !pr.AnalysisPassed || !pr.SecurityGatesPassed {
		return false
	}
	if pr.CourtReviewRequired && pr.CourtReviewStatus != CourtReviewApproved {
		return false
	}
	if !pr.Approved {
		return false
	}
	return true
}
