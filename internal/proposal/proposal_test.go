package proposal

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewProposal(t *testing.T) {
	p, err := NewProposal("Add logging skill", "A skill that aggregates logs", CategoryNewSkill, "admin")
	if err != nil {
		t.Fatalf("NewProposal failed: %v", err)
	}
	if p.ID == "" {
		t.Error("expected non-empty ID")
	}
	if p.Status != StatusDraft {
		t.Errorf("expected status %q, got %q", StatusDraft, p.Status)
	}
	if p.Version != 1 {
		t.Errorf("expected version 1, got %d", p.Version)
	}
	if p.MerkleHash == "" {
		t.Error("expected non-empty merkle hash")
	}
	if len(p.History) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(p.History))
	}
	if p.Round != 0 {
		t.Errorf("expected round 0, got %d", p.Round)
	}
}

func TestNewProposalValidation(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		desc     string
		cat      Category
		author   string
		wantErr  bool
	}{
		{"empty title", "", "desc", CategoryNewSkill, "admin", true},
		{"empty description", "title", "", CategoryNewSkill, "admin", true},
		{"empty author", "title", "desc", CategoryNewSkill, "", true},
		{"invalid category", "title", "desc", Category("invalid"), "admin", true},
		{"valid", "title", "desc", CategoryNewSkill, "admin", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProposal(tt.title, tt.desc, tt.cat, tt.author)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProposal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProposalTransition(t *testing.T) {
	p, _ := NewProposal("Test", "Testing transitions", CategoryNewSkill, "admin")
	originalHash := p.MerkleHash

	// draft -> submitted
	if err := p.Transition(StatusSubmitted, "ready for review", "admin"); err != nil {
		t.Fatalf("Transition to submitted failed: %v", err)
	}
	if p.Status != StatusSubmitted {
		t.Errorf("expected %q, got %q", StatusSubmitted, p.Status)
	}
	if p.Version != 2 {
		t.Errorf("expected version 2, got %d", p.Version)
	}
	if p.MerkleHash == originalHash {
		t.Error("expected hash to change after transition")
	}
	if p.PrevHash != originalHash {
		t.Error("expected prev_hash to be the original hash")
	}
	if len(p.History) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(p.History))
	}

	// submitted -> in_review
	if err := p.Transition(StatusInReview, "court session started", "court"); err != nil {
		t.Fatalf("Transition to in_review failed: %v", err)
	}

	// in_review -> approved
	if err := p.Transition(StatusApproved, "all reviewers approved", "court"); err != nil {
		t.Fatalf("Transition to approved failed: %v", err)
	}

	// approved -> implementing
	if err := p.Transition(StatusImplementing, "starting build", "builder"); err != nil {
		t.Fatalf("Transition to implementing failed: %v", err)
	}

	// implementing -> complete
	if err := p.Transition(StatusComplete, "build successful", "builder"); err != nil {
		t.Fatalf("Transition to complete failed: %v", err)
	}

	if p.Version != 6 {
		t.Errorf("expected version 6 after 5 transitions, got %d", p.Version)
	}
}

func TestProposalInvalidTransition(t *testing.T) {
	p, _ := NewProposal("Test", "Testing bad transitions", CategoryNewSkill, "admin")

	// draft -> approved is not allowed
	if err := p.Transition(StatusApproved, "skip ahead", "admin"); err == nil {
		t.Error("expected error for invalid transition draft -> approved")
	}

	// draft -> complete is not allowed
	if err := p.Transition(StatusComplete, "skip everything", "admin"); err == nil {
		t.Error("expected error for invalid transition draft -> complete")
	}
}

func TestProposalTransitionRequiresReasonAndActor(t *testing.T) {
	p, _ := NewProposal("Test", "Testing validation", CategoryNewSkill, "admin")

	if err := p.Transition(StatusSubmitted, "", "admin"); err == nil {
		t.Error("expected error for empty reason")
	}
	if err := p.Transition(StatusSubmitted, "reason", ""); err == nil {
		t.Error("expected error for empty actor")
	}
}

func TestProposalAddReview(t *testing.T) {
	p, _ := NewProposal("Test", "Testing reviews", CategoryNewSkill, "admin")
	originalHash := p.MerkleHash

	review := Review{
		ID:        "review-1",
		Persona:   "CISO",
		Model:     "llama-3.2-3b",
		Round:     1,
		Verdict:   VerdictApprove,
		RiskScore: 3.5,
		Evidence:  []string{"No security issues found"},
		Comments:  "Looks good",
		Timestamp: time.Now().UTC(),
	}

	if err := p.AddReview(review); err != nil {
		t.Fatalf("AddReview failed: %v", err)
	}
	if len(p.Reviews) != 1 {
		t.Errorf("expected 1 review, got %d", len(p.Reviews))
	}
	if p.Version != 2 {
		t.Errorf("expected version 2, got %d", p.Version)
	}
	if p.MerkleHash == originalHash {
		t.Error("expected hash to change after adding review")
	}
}

func TestProposalAddReviewValidation(t *testing.T) {
	p, _ := NewProposal("Test", "Testing review validation", CategoryNewSkill, "admin")

	// Missing ID
	review := Review{
		Persona:   "CISO",
		Model:     "llama-3.2-3b",
		Round:     1,
		Verdict:   VerdictApprove,
		RiskScore: 3.5,
		Evidence:  []string{"Evidence"},
		Timestamp: time.Now().UTC(),
	}
	if err := p.AddReview(review); err == nil {
		t.Error("expected error for review with missing ID")
	}

	// Invalid risk score
	review.ID = "review-1"
	review.RiskScore = 15
	if err := p.AddReview(review); err == nil {
		t.Error("expected error for review with invalid risk score")
	}
}

func TestProposalAggregateRisk(t *testing.T) {
	p, _ := NewProposal("Test", "Testing risk aggregation", CategoryNewSkill, "admin")

	if p.AggregateRisk() != 0 {
		t.Error("expected 0 aggregate risk with no reviews")
	}

	reviews := []Review{
		{ID: "r1", Persona: "CISO", Model: "m1", Round: 1, Verdict: VerdictApprove, RiskScore: 2, Evidence: []string{"ok"}, Timestamp: time.Now().UTC()},
		{ID: "r2", Persona: "Coder", Model: "m1", Round: 1, Verdict: VerdictApprove, RiskScore: 4, Evidence: []string{"ok"}, Timestamp: time.Now().UTC()},
		{ID: "r3", Persona: "Tester", Model: "m1", Round: 1, Verdict: VerdictApprove, RiskScore: 6, Evidence: []string{"ok"}, Timestamp: time.Now().UTC()},
	}
	for _, r := range reviews {
		if err := p.AddReview(r); err != nil {
			t.Fatalf("AddReview failed: %v", err)
		}
	}

	avg := p.AggregateRisk()
	if avg != 4.0 {
		t.Errorf("expected aggregate risk 4.0, got %f", avg)
	}
}

func TestProposalRiskHeatmap(t *testing.T) {
	p, _ := NewProposal("Test", "Testing heatmap", CategoryNewSkill, "admin")
	p.Round = 1

	reviews := []Review{
		{ID: "r1", Persona: "CISO", Model: "m1", Round: 1, Verdict: VerdictApprove, RiskScore: 2, Evidence: []string{"ok"}, Timestamp: time.Now().UTC()},
		{ID: "r2", Persona: "Coder", Model: "m1", Round: 1, Verdict: VerdictReject, RiskScore: 8, Evidence: []string{"issues"}, Timestamp: time.Now().UTC()},
	}
	for _, r := range reviews {
		if err := p.AddReview(r); err != nil {
			t.Fatalf("AddReview failed: %v", err)
		}
	}

	heatmap := p.RiskHeatmap()
	if heatmap["CISO"] != 2 {
		t.Errorf("expected CISO risk 2, got %f", heatmap["CISO"])
	}
	if heatmap["Coder"] != 8 {
		t.Errorf("expected Coder risk 8, got %f", heatmap["Coder"])
	}
}

func TestProposalMarshalUnmarshal(t *testing.T) {
	p, _ := NewProposal("Marshal Test", "Testing serialization", CategoryEditSkill, "admin")
	p.TargetSkill = "logging"
	p.Spec = json.RawMessage(`{"key": "value"}`)

	data, err := p.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	restored, err := UnmarshalProposal(data)
	if err != nil {
		t.Fatalf("UnmarshalProposal failed: %v", err)
	}

	if restored.ID != p.ID {
		t.Errorf("ID mismatch: %q vs %q", restored.ID, p.ID)
	}
	if restored.Title != p.Title {
		t.Errorf("Title mismatch: %q vs %q", restored.Title, p.Title)
	}
	if restored.Category != p.Category {
		t.Errorf("Category mismatch: %q vs %q", restored.Category, p.Category)
	}
	if restored.MerkleHash != p.MerkleHash {
		t.Errorf("MerkleHash mismatch: %q vs %q", restored.MerkleHash, p.MerkleHash)
	}
	if restored.Version != p.Version {
		t.Errorf("Version mismatch: %d vs %d", restored.Version, p.Version)
	}
}

func TestProposalBranchName(t *testing.T) {
	p, _ := NewProposal("Branch Test", "Testing branch name", CategoryNewSkill, "admin")
	expected := "proposal-" + p.ID
	if p.BranchName() != expected {
		t.Errorf("expected branch %q, got %q", expected, p.BranchName())
	}
}

func TestProposalValidate(t *testing.T) {
	p, _ := NewProposal("Valid", "Testing validation", CategoryNewSkill, "admin")

	if err := p.Validate(); err != nil {
		t.Errorf("expected valid proposal, got: %v", err)
	}

	// Test invalid: empty title
	p.Title = ""
	if err := p.Validate(); err == nil {
		t.Error("expected error for empty title")
	}
	p.Title = "Valid"

	// Test invalid: title too long
	longTitle := make([]byte, 201)
	for i := range longTitle {
		longTitle[i] = 'a'
	}
	p.Title = string(longTitle)
	if err := p.Validate(); err == nil {
		t.Error("expected error for title > 200 chars")
	}
}

func TestReviewValidation(t *testing.T) {
	tests := []struct {
		name    string
		review  Review
		wantErr bool
	}{
		{
			"valid",
			Review{ID: "r1", Persona: "CISO", Model: "m1", Round: 1, Verdict: VerdictApprove, RiskScore: 5, Evidence: []string{"ok"}, Timestamp: time.Now()},
			false,
		},
		{
			"missing persona",
			Review{ID: "r1", Model: "m1", Round: 1, Verdict: VerdictApprove, RiskScore: 5, Evidence: []string{"ok"}, Timestamp: time.Now()},
			true,
		},
		{
			"invalid verdict",
			Review{ID: "r1", Persona: "CISO", Model: "m1", Round: 1, Verdict: ReviewVerdict("maybe"), RiskScore: 5, Evidence: []string{"ok"}, Timestamp: time.Now()},
			true,
		},
		{
			"risk too high",
			Review{ID: "r1", Persona: "CISO", Model: "m1", Round: 1, Verdict: VerdictApprove, RiskScore: 11, Evidence: []string{"ok"}, Timestamp: time.Now()},
			true,
		},
		{
			"no evidence",
			Review{ID: "r1", Persona: "CISO", Model: "m1", Round: 1, Verdict: VerdictApprove, RiskScore: 5, Timestamp: time.Now()},
			true,
		},
		{
			"zero round",
			Review{ID: "r1", Persona: "CISO", Model: "m1", Round: 0, Verdict: VerdictApprove, RiskScore: 5, Evidence: []string{"ok"}, Timestamp: time.Now()},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.review.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Review.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRejectionLoop(t *testing.T) {
	p, _ := NewProposal("Rejected Proposal", "Will be rejected then resubmitted", CategoryNewSkill, "admin")

	// draft -> submitted -> in_review -> rejected -> draft
	if err := p.Transition(StatusSubmitted, "submit", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := p.Transition(StatusInReview, "start review", "court"); err != nil {
		t.Fatal(err)
	}
	if err := p.Transition(StatusRejected, "security issues", "court"); err != nil {
		t.Fatal(err)
	}
	if err := p.Transition(StatusDraft, "fixing issues", "admin"); err != nil {
		t.Fatal(err)
	}

	// Can resubmit after rejection
	if err := p.Transition(StatusSubmitted, "fixed and resubmitting", "admin"); err != nil {
		t.Fatal(err)
	}

	if len(p.History) != 6 {
		t.Errorf("expected 6 history entries, got %d", len(p.History))
	}
}

func TestWithdrawal(t *testing.T) {
	p, _ := NewProposal("Withdrawn Proposal", "Will be withdrawn", CategoryNewSkill, "admin")

	// draft -> withdrawn
	if err := p.Transition(StatusWithdrawn, "changed my mind", "admin"); err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusWithdrawn {
		t.Errorf("expected %q, got %q", StatusWithdrawn, p.Status)
	}

	// withdrawn is a terminal state (no transitions out)
	if err := p.Transition(StatusDraft, "undo", "admin"); err == nil {
		t.Error("expected error transitioning from withdrawn")
	}
}
