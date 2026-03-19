package proposal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Status represents the lifecycle state of a proposal.
type Status string

const (
	StatusDraft        Status = "draft"
	StatusSubmitted    Status = "submitted"
	StatusInReview     Status = "in_review"
	StatusApproved     Status = "approved"
	StatusRejected     Status = "rejected"
	StatusImplementing Status = "implementing"
	StatusComplete     Status = "complete"
	StatusFailed       Status = "failed"
	StatusWithdrawn    Status = "withdrawn"
)

var validStatuses = map[Status]bool{
	StatusDraft: true, StatusSubmitted: true, StatusInReview: true,
	StatusApproved: true, StatusRejected: true, StatusImplementing: true,
	StatusComplete: true, StatusFailed: true, StatusWithdrawn: true,
}

var allowedTransitions = map[Status][]Status{
	StatusDraft:        {StatusSubmitted, StatusWithdrawn},
	StatusSubmitted:    {StatusInReview, StatusWithdrawn},
	StatusInReview:     {StatusApproved, StatusRejected, StatusWithdrawn},
	StatusApproved:     {StatusImplementing, StatusWithdrawn},
	StatusRejected:     {StatusDraft},
	StatusImplementing: {StatusComplete, StatusFailed},
	StatusFailed:       {StatusDraft},
}

// Category classifies what the proposal modifies.
type Category string

const (
	CategoryNewSkill    Category = "new_skill"
	CategoryEditSkill   Category = "edit_skill"
	CategoryDeleteSkill Category = "delete_skill"
	CategoryKernelPatch Category = "kernel_patch"
	CategoryConfigChange Category = "config_change"
)

var validCategories = map[Category]bool{
	CategoryNewSkill: true, CategoryEditSkill: true,
	CategoryDeleteSkill: true, CategoryKernelPatch: true,
	CategoryConfigChange: true,
}

// RiskLevel indicates the assessed risk of implementing the proposal.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// ReviewVerdict is the persona's decision on a proposal.
type ReviewVerdict string

const (
	VerdictApprove ReviewVerdict = "approve"
	VerdictReject  ReviewVerdict = "reject"
	VerdictAsk     ReviewVerdict = "ask"
	VerdictAbstain ReviewVerdict = "abstain"
)

// Review captures one persona's evaluation of a proposal.
type Review struct {
	ID        string          `json:"id"`
	Persona   string          `json:"persona"`
	Model     string          `json:"model"`
	Round     int             `json:"round"`
	Verdict   ReviewVerdict   `json:"verdict"`
	RiskScore float64         `json:"risk_score"`
	Evidence  []string        `json:"evidence"`
	Questions []string        `json:"questions,omitempty"`
	Comments  string          `json:"comments"`
	Timestamp time.Time       `json:"timestamp"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

// Validate ensures the review has required fields.
func (r *Review) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("review ID is required")
	}
	if r.Persona == "" {
		return fmt.Errorf("review persona is required")
	}
	if r.Model == "" {
		return fmt.Errorf("review model is required")
	}
	if r.Round < 1 {
		return fmt.Errorf("review round must be >= 1")
	}
	switch r.Verdict {
	case VerdictApprove, VerdictReject, VerdictAsk, VerdictAbstain:
	default:
		return fmt.Errorf("invalid review verdict: %q", r.Verdict)
	}
	if r.RiskScore < 0 || r.RiskScore > 10 {
		return fmt.Errorf("risk score must be between 0 and 10, got %f", r.RiskScore)
	}
	if len(r.Evidence) == 0 {
		return fmt.Errorf("at least one evidence item is required")
	}
	if r.Timestamp.IsZero() {
		return fmt.Errorf("review timestamp is required")
	}
	return nil
}

// StatusChange records a state transition with metadata.
type StatusChange struct {
	From      Status    `json:"from"`
	To        Status    `json:"to"`
	Reason    string    `json:"reason"`
	Actor     string    `json:"actor"`
	Timestamp time.Time `json:"timestamp"`
}

// Proposal represents a governance proposal that must pass through the Court
// before any change is applied to the AegisClaw system.
type Proposal struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Category    Category        `json:"category"`
	Status      Status          `json:"status"`
	Risk        RiskLevel       `json:"risk"`
	Author      string          `json:"author"`
	TargetSkill string          `json:"target_skill,omitempty"`
	Spec        json.RawMessage `json:"spec,omitempty"`
	Reviews     []Review        `json:"reviews,omitempty"`
	History     []StatusChange  `json:"history"`
	Round       int             `json:"round"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	MerkleHash  string          `json:"merkle_hash"`
	PrevHash    string          `json:"prev_hash"`
	Version     int             `json:"version"`
}

// NewProposal creates a new proposal in draft status.
func NewProposal(title, description string, category Category, author string) (*Proposal, error) {
	if title == "" {
		return nil, fmt.Errorf("proposal title is required")
	}
	if description == "" {
		return nil, fmt.Errorf("proposal description is required")
	}
	if author == "" {
		return nil, fmt.Errorf("proposal author is required")
	}
	if !validCategories[category] {
		return nil, fmt.Errorf("invalid proposal category: %q", category)
	}

	now := time.Now().UTC()
	p := &Proposal{
		ID:          uuid.New().String(),
		Title:       title,
		Description: description,
		Category:    category,
		Status:      StatusDraft,
		Risk:        RiskMedium,
		Author:      author,
		Round:       0,
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     1,
		History: []StatusChange{
			{
				From:      "",
				To:        StatusDraft,
				Reason:    "proposal created",
				Actor:     author,
				Timestamp: now,
			},
		},
	}
	p.MerkleHash = p.computeHash()
	return p, nil
}

// Validate ensures the proposal has all required fields and consistent state.
func (p *Proposal) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("proposal ID is required")
	}
	if _, err := uuid.Parse(p.ID); err != nil {
		return fmt.Errorf("proposal ID is not a valid UUID: %w", err)
	}
	if p.Title == "" {
		return fmt.Errorf("proposal title is required")
	}
	if len(p.Title) > 200 {
		return fmt.Errorf("proposal title too long: max 200 characters")
	}
	if p.Description == "" {
		return fmt.Errorf("proposal description is required")
	}
	if p.Author == "" {
		return fmt.Errorf("proposal author is required")
	}
	if !validStatuses[p.Status] {
		return fmt.Errorf("invalid proposal status: %q", p.Status)
	}
	if !validCategories[p.Category] {
		return fmt.Errorf("invalid proposal category: %q", p.Category)
	}
	if p.CreatedAt.IsZero() {
		return fmt.Errorf("proposal created_at is required")
	}
	if p.UpdatedAt.IsZero() {
		return fmt.Errorf("proposal updated_at is required")
	}
	if p.Version < 1 {
		return fmt.Errorf("proposal version must be >= 1")
	}
	if len(p.History) == 0 {
		return fmt.Errorf("proposal must have at least one history entry")
	}
	return nil
}

// Transition moves the proposal to a new status if the transition is allowed.
func (p *Proposal) Transition(newStatus Status, reason, actor string) error {
	if reason == "" {
		return fmt.Errorf("transition reason is required")
	}
	if actor == "" {
		return fmt.Errorf("transition actor is required")
	}

	allowed, ok := allowedTransitions[p.Status]
	if !ok {
		return fmt.Errorf("no transitions allowed from status %q", p.Status)
	}

	valid := false
	for _, s := range allowed {
		if s == newStatus {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("transition from %q to %q is not allowed", p.Status, newStatus)
	}

	now := time.Now().UTC()
	p.History = append(p.History, StatusChange{
		From:      p.Status,
		To:        newStatus,
		Reason:    reason,
		Actor:     actor,
		Timestamp: now,
	})

	p.PrevHash = p.MerkleHash
	p.Status = newStatus
	p.UpdatedAt = now
	p.Version++
	p.MerkleHash = p.computeHash()
	return nil
}

// AddReview appends a validated review and updates the proposal hash.
func (p *Proposal) AddReview(review Review) error {
	if err := review.Validate(); err != nil {
		return fmt.Errorf("invalid review: %w", err)
	}
	p.Reviews = append(p.Reviews, review)
	p.PrevHash = p.MerkleHash
	p.UpdatedAt = time.Now().UTC()
	p.Version++
	p.MerkleHash = p.computeHash()
	return nil
}

// AggregateRisk computes the average risk score from all reviews.
func (p *Proposal) AggregateRisk() float64 {
	if len(p.Reviews) == 0 {
		return 0
	}
	var total float64
	for _, r := range p.Reviews {
		total += r.RiskScore
	}
	return total / float64(len(p.Reviews))
}

// RiskHeatmap returns a map of persona -> risk score for the latest round.
func (p *Proposal) RiskHeatmap() map[string]float64 {
	heatmap := make(map[string]float64)
	for _, r := range p.Reviews {
		if r.Round == p.Round {
			heatmap[r.Persona] = r.RiskScore
		}
	}
	return heatmap
}

// ReviewsForRound returns all reviews for a specific round.
func (p *Proposal) ReviewsForRound(round int) []Review {
	var result []Review
	for _, r := range p.Reviews {
		if r.Round == round {
			result = append(result, r)
		}
	}
	return result
}

// Marshal serializes the Proposal to JSON.
func (p *Proposal) Marshal() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// UnmarshalProposal deserializes a Proposal from JSON.
func UnmarshalProposal(data []byte) (*Proposal, error) {
	var p Proposal
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proposal: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid proposal data: %w", err)
	}
	return &p, nil
}

// BranchName returns the git branch name for this proposal.
func (p *Proposal) BranchName() string {
	return fmt.Sprintf("proposal-%s", p.ID)
}

// computeHash generates a SHA-256 hash over the proposal's mutable content.
func (p *Proposal) computeHash() string {
	h := sha256.New()
	h.Write([]byte(p.ID))
	h.Write([]byte(p.Title))
	h.Write([]byte(p.Description))
	h.Write([]byte(p.Status))
	h.Write([]byte(p.Category))
	h.Write([]byte(p.Author))
	h.Write([]byte(p.PrevHash))
	h.Write([]byte(fmt.Sprintf("%d", p.Version)))
	h.Write([]byte(p.UpdatedAt.Format(time.RFC3339Nano)))
	if p.Spec != nil {
		h.Write(p.Spec)
	}
	for _, r := range p.Reviews {
		h.Write([]byte(r.ID))
		h.Write([]byte(r.Persona))
		h.Write([]byte(r.Verdict))
	}
	return hex.EncodeToString(h.Sum(nil))
}
