package proposal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
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
	StatusEscalated    Status = "escalated"
	StatusImplementing Status = "implementing"
	StatusComplete     Status = "complete"
	StatusFailed       Status = "failed"
	StatusWithdrawn    Status = "withdrawn"
)

var validStatuses = map[Status]bool{
	StatusDraft: true, StatusSubmitted: true, StatusInReview: true,
	StatusApproved: true, StatusRejected: true, StatusEscalated: true,
	StatusImplementing: true, StatusComplete: true, StatusFailed: true,
	StatusWithdrawn: true,
}

var allowedTransitions = map[Status][]Status{
	StatusDraft:        {StatusSubmitted, StatusWithdrawn},
	StatusSubmitted:    {StatusInReview, StatusWithdrawn},
	StatusInReview:     {StatusApproved, StatusRejected, StatusEscalated, StatusWithdrawn},
	StatusApproved:     {StatusImplementing, StatusWithdrawn},
	StatusRejected:     {StatusDraft},
	StatusEscalated:    {StatusApproved, StatusRejected, StatusDraft},
	StatusImplementing: {StatusComplete, StatusFailed},
	StatusFailed:       {StatusDraft},
}

// Category classifies what the proposal modifies.
type Category string

const (
	CategoryNewSkill     Category = "new_skill"
	CategoryEditSkill    Category = "edit_skill"
	CategoryDeleteSkill  Category = "delete_skill"
	CategoryKernelPatch  Category = "kernel_patch"
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

// ProposalNetworkPolicy defines network access rules for a proposed skill.
type ProposalNetworkPolicy struct {
	DefaultDeny      bool     `json:"default_deny"`
	AllowedHosts     []string `json:"allowed_hosts,omitempty"`
	AllowedPorts     []uint16 `json:"allowed_ports,omitempty"`
	AllowedProtocols []string `json:"allowed_protocols,omitempty"`
}

// SkillCapabilities declares the sandbox capabilities a skill requires.
// These are reviewed by the Governance Court and enforced at the sandbox
// level (Firecracker rootfs / Docker seccomp+AppArmor).
// Inspired by the OpenClaw capability model; aligned with AegisClaw's
// zero-trust isolation principles.
type SkillCapabilities struct {
	// Network declares that the skill needs outbound network access.
	// If true, a NetworkPolicy must also be provided.
	Network bool `json:"network,omitempty"`
	// FilesystemWrite declares that the skill writes to the host workspace
	// (mounted read-write overlay). Read-only access is always available;
	// write access must be explicitly declared and Court-approved.
	FilesystemWrite bool `json:"filesystem_write,omitempty"`
	// HostDevices lists host device paths the skill needs proxied access to
	// (e.g. "/dev/snd" for audio, "/dev/video0" for camera). Each entry is
	// reviewed by the CISO persona during Court review.
	HostDevices []string `json:"host_devices,omitempty"`
	// Secrets lists the secret reference names the skill reads at runtime.
	// Mirrors Proposal.SecretsRefs but scoped to capabilities for clarity.
	Secrets []string `json:"secrets,omitempty"`
	// CanAccessOtherSessions permits this skill to call sessions_send /
	// sessions_history targeting other AegisClaw sessions. Requires explicit
	// Court approval and is denied by default.
	CanAccessOtherSessions bool `json:"can_access_other_sessions,omitempty"`
}

var proposalSecretRefRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]{0,127}$`)

// Proposal represents a governance proposal that must pass through the Court
// before any change is applied to the AegisClaw system.
type Proposal struct {
	ID            string                 `json:"id"`
	Title         string                 `json:"title"`
	Description   string                 `json:"description"`
	Category      Category               `json:"category"`
	Status        Status                 `json:"status"`
	Risk          RiskLevel              `json:"risk"`
	Author        string                 `json:"author"`
	TargetSkill   string                 `json:"target_skill,omitempty"`
	Spec          json.RawMessage        `json:"spec,omitempty"`
	SecretsRefs   []string               `json:"secrets_refs,omitempty"`
	NetworkPolicy *ProposalNetworkPolicy `json:"network_policy,omitempty"`
	// Capabilities declares the sandbox capabilities this skill requires.
	// Populated by proposal.create_draft and reviewed by the Governance Court.
	// Enforcement happens at sandbox launch time (Firecracker rootfs flags or
	// Docker seccomp/AppArmor profiles).
	Capabilities *SkillCapabilities `json:"capabilities,omitempty"`
	Reviews       []Review               `json:"reviews,omitempty"`
	History       []StatusChange         `json:"history"`
	Round         int                    `json:"round"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	MerkleHash    string                 `json:"merkle_hash"`
	PrevHash      string                 `json:"prev_hash"`
	Version       int                    `json:"version"`
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
	for i, ref := range p.SecretsRefs {
		if !proposalSecretRefRegex.MatchString(ref) {
			return fmt.Errorf("secrets_refs[%d] %q is not a valid secret name", i, ref)
		}
	}
	if p.NetworkPolicy != nil {
		if !p.NetworkPolicy.DefaultDeny {
			return fmt.Errorf("network_policy.default_deny must be true")
		}
		for _, proto := range p.NetworkPolicy.AllowedProtocols {
			switch proto {
			case "tcp", "udp", "icmp":
			default:
				return fmt.Errorf("unsupported network protocol %q", proto)
			}
		}
	}
	return nil
}

// IsSandboxedLowRisk returns true when the proposal meets all conditions for
// automatic approval without LLM court review:
//   - overall risk is "low" (all three risk dimensions score 1)
//   - network policy is default-deny with no allowed hosts
//   - no secrets references
//   - no elevated capabilities (network, host devices, cross-session access)
//
// Engine callers may use this to short-circuit expensive LLM reviewer rounds
// when the skill is fully isolated and carries minimal privilege.
func (p *Proposal) IsSandboxedLowRisk() bool {
	if p.Risk != RiskLow {
		return false
	}
	if p.NetworkPolicy == nil || !p.NetworkPolicy.DefaultDeny {
		return false
	}
	if len(p.NetworkPolicy.AllowedHosts) > 0 {
		return false
	}
	if len(p.SecretsRefs) > 0 {
		return false
	}
	if p.Capabilities != nil {
		if p.Capabilities.Network ||
			len(p.Capabilities.Secrets) > 0 ||
			len(p.Capabilities.HostDevices) > 0 ||
			p.Capabilities.CanAccessOtherSessions {
			return false
		}
	}
	return true
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

// ApplyFeedback appends reviewer feedback to the proposal description and
// updates metadata (version, timestamps, merkle hash) so changes are
// tracked in the proposal git history when persisted via Store.Update.
func (p *Proposal) ApplyFeedback(feedback, actor, reason string) error {
	if feedback == "" {
		return nil
	}
	now := time.Now().UTC()
	// Record an informational history entry (no status change).
	p.History = append(p.History, StatusChange{
		From:      p.Status,
		To:        p.Status,
		Reason:    reason,
		Actor:     actor,
		Timestamp: now,
	})
	p.PrevHash = p.MerkleHash
	if p.Description == "" {
		p.Description = feedback
	} else {
		p.Description = p.Description + "\n\n" + feedback
	}
	p.UpdatedAt = now
	p.Version++
	p.MerkleHash = p.computeHash()
	return nil
}

// BumpVersion increments the version counter, updates timestamps, and
// recomputes the Merkle hash. Call this after mutating proposal fields
// directly (e.g., from tool handlers) rather than through Transition,
// AddReview, or ApplyFeedback which bump the version internally.
func (p *Proposal) BumpVersion() {
	p.PrevHash = p.MerkleHash
	p.UpdatedAt = time.Now().UTC()
	p.Version++
	p.MerkleHash = p.computeHash()
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
