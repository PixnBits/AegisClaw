package court

import (
	"context"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

func TestEvaluateConsensusAllApprove(t *testing.T) {
	personas := []*Persona{
		{Name: "CISO", Weight: 0.3},
		{Name: "Coder", Weight: 0.3},
		{Name: "Tester", Weight: 0.2},
		{Name: "Architect", Weight: 0.1},
		{Name: "Advocate", Weight: 0.1},
	}
	reviews := []proposal.Review{
		{Persona: "CISO", Verdict: proposal.VerdictApprove, RiskScore: 2.0, Evidence: []string{"ok"}},
		{Persona: "Coder", Verdict: proposal.VerdictApprove, RiskScore: 1.0, Evidence: []string{"ok"}},
		{Persona: "Tester", Verdict: proposal.VerdictApprove, RiskScore: 3.0, Evidence: []string{"ok"}},
		{Persona: "Architect", Verdict: proposal.VerdictApprove, RiskScore: 2.0, Evidence: []string{"ok"}},
		{Persona: "Advocate", Verdict: proposal.VerdictApprove, RiskScore: 1.0, Evidence: []string{"ok"}},
	}

	result := EvaluateConsensus(reviews, personas, 0.8)
	if !result.Reached {
		t.Error("expected consensus reached with 100% approval")
	}
	if result.ApprovalRate != 1.0 {
		t.Errorf("expected 1.0 approval rate, got %f", result.ApprovalRate)
	}
	if result.AvgRisk != 1.8 {
		t.Errorf("expected 1.8 avg risk, got %f", result.AvgRisk)
	}
}

func TestEvaluateConsensusWeightedMajority(t *testing.T) {
	// CISO (0.3) + Coder (0.3) + Tester (0.2) approve = 0.8 weight
	// Architect (0.1) + Advocate (0.1) reject = 0.2 weight
	// Approval rate = 0.8/1.0 = 80% >= 80% quorum
	personas := []*Persona{
		{Name: "CISO", Weight: 0.3},
		{Name: "Coder", Weight: 0.3},
		{Name: "Tester", Weight: 0.2},
		{Name: "Architect", Weight: 0.1},
		{Name: "Advocate", Weight: 0.1},
	}
	reviews := []proposal.Review{
		{Persona: "CISO", Verdict: proposal.VerdictApprove, RiskScore: 3.0, Evidence: []string{"ok"}},
		{Persona: "Coder", Verdict: proposal.VerdictApprove, RiskScore: 2.0, Evidence: []string{"ok"}},
		{Persona: "Tester", Verdict: proposal.VerdictApprove, RiskScore: 4.0, Evidence: []string{"ok"}},
		{Persona: "Architect", Verdict: proposal.VerdictReject, RiskScore: 7.0, Evidence: []string{"concern"}, Comments: "Architecture issue"},
		{Persona: "Advocate", Verdict: proposal.VerdictReject, RiskScore: 6.0, Evidence: []string{"ux issue"}},
	}

	result := EvaluateConsensus(reviews, personas, 0.8)
	if !result.Reached {
		t.Error("expected consensus reached with 80% weighted approval")
	}
	if result.RejectRate != 0.2 {
		t.Errorf("expected 0.2 reject rate, got %f", result.RejectRate)
	}
}

func TestEvaluateConsensusWeightedBelowQuorum(t *testing.T) {
	// CISO (0.3) approves, rest reject = 0.3/1.0 = 30% < 80%
	personas := []*Persona{
		{Name: "CISO", Weight: 0.3},
		{Name: "Coder", Weight: 0.3},
		{Name: "Tester", Weight: 0.2},
		{Name: "Architect", Weight: 0.1},
		{Name: "Advocate", Weight: 0.1},
	}
	reviews := []proposal.Review{
		{Persona: "CISO", Verdict: proposal.VerdictApprove, RiskScore: 3.0, Evidence: []string{"ok"}},
		{Persona: "Coder", Verdict: proposal.VerdictReject, RiskScore: 8.0, Evidence: []string{"bad"}},
		{Persona: "Tester", Verdict: proposal.VerdictReject, RiskScore: 7.0, Evidence: []string{"bad"}},
		{Persona: "Architect", Verdict: proposal.VerdictReject, RiskScore: 9.0, Evidence: []string{"bad"}},
		{Persona: "Advocate", Verdict: proposal.VerdictReject, RiskScore: 6.0, Evidence: []string{"bad"}},
	}

	result := EvaluateConsensus(reviews, personas, 0.8)
	if result.Reached {
		t.Error("expected no consensus with 30% weighted approval")
	}
}

func TestEvaluateConsensusAskFeedback(t *testing.T) {
	personas := []*Persona{
		{Name: "CISO", Weight: 0.3},
		{Name: "Coder", Weight: 0.3},
		{Name: "Tester", Weight: 0.2},
	}
	reviews := []proposal.Review{
		{Persona: "CISO", Verdict: proposal.VerdictApprove, RiskScore: 3.0, Evidence: []string{"ok"}},
		{Persona: "Coder", Verdict: proposal.VerdictAsk, RiskScore: 5.0, Evidence: []string{"need info"}, Questions: []string{"What about edge cases?", "Is concurrency safe?"}, Comments: "Need clarification"},
		{Persona: "Tester", Verdict: proposal.VerdictApprove, RiskScore: 4.0, Evidence: []string{"tests ok"}},
	}

	result := EvaluateConsensus(reviews, personas, 0.8)

	// 0.3 + 0.2 = 0.5 approved out of 0.8 total = 62.5% < 80%
	if result.Reached {
		t.Error("expected no consensus with ask verdict preventing quorum")
	}
	if !result.Feedback.HasQuestions {
		t.Error("expected feedback to have questions")
	}
	if len(result.Feedback.Questions) != 2 {
		t.Errorf("expected 2 questions, got %d", len(result.Feedback.Questions))
	}
	if len(result.Feedback.Concerns) != 1 {
		t.Errorf("expected 1 concern, got %d", len(result.Feedback.Concerns))
	}
}

func TestEvaluateConsensusEmptyReviews(t *testing.T) {
	result := EvaluateConsensus(nil, nil, 0.8)
	if result.Reached {
		t.Error("expected no consensus for empty reviews")
	}
	if result.AvgRisk != 0 {
		t.Error("expected 0 avg risk for empty reviews")
	}
}

func TestIterationFeedbackFormat(t *testing.T) {
	fb := &IterationFeedback{
		Questions:    []string{"What about X?", "Is Y safe?"},
		Concerns:     []string{"[CISO] Performance impact"},
		RoundNumber:  2,
		HasQuestions: true,
	}

	prompt := fb.FormatFeedbackPrompt()
	if prompt == "" {
		t.Error("expected non-empty feedback prompt")
	}
	if len(prompt) < 50 {
		t.Errorf("feedback prompt too short: %q", prompt)
	}
}

func TestIterationFeedbackEmpty(t *testing.T) {
	fb := &IterationFeedback{}
	if fb.FormatFeedbackPrompt() != "" {
		t.Error("expected empty prompt for no feedback")
	}
}

func TestEngineVoteApprove(t *testing.T) {
	// Use allRejectReviewer to get escalated, then vote approve
	engine, store := setupTestEngine(t, allRejectReviewer())
	p := createTestProposal(t, store)

	session, err := engine.Review(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}
	if session.State != SessionEscalated {
		t.Fatalf("expected escalated, got %q", session.State)
	}

	voted, err := engine.VoteOnProposal(context.Background(), p.ID, "admin-user", true, "Override: acceptable risk after manual review")
	if err != nil {
		t.Fatalf("VoteOnProposal failed: %v", err)
	}
	if voted.State != SessionApproved {
		t.Errorf("expected approved after vote, got %q", voted.State)
	}
	if voted.Verdict != "approved" {
		t.Errorf("expected verdict approved, got %q", voted.Verdict)
	}

	// Verify proposal is approved in store
	loaded, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if loaded.Status != proposal.StatusApproved {
		t.Errorf("expected proposal approved, got %q", loaded.Status)
	}
}

func TestEngineVoteReject(t *testing.T) {
	engine, store := setupTestEngine(t, allRejectReviewer())
	p := createTestProposal(t, store)

	session, err := engine.Review(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}
	if session.State != SessionEscalated {
		t.Fatalf("expected escalated, got %q", session.State)
	}

	voted, err := engine.VoteOnProposal(context.Background(), p.ID, "admin-user", false, "Confirmed: too risky")
	if err != nil {
		t.Fatalf("VoteOnProposal failed: %v", err)
	}
	if voted.State != SessionRejected {
		t.Errorf("expected rejected after vote, got %q", voted.State)
	}

	loaded, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if loaded.Status != proposal.StatusRejected {
		t.Errorf("expected proposal rejected, got %q", loaded.Status)
	}
}

func TestEngineVoteNoEscalated(t *testing.T) {
	engine, store := setupTestEngine(t, allApproveReviewer())
	p := createTestProposal(t, store)

	// Review succeeds (approved), so no escalated session
	engine.Review(context.Background(), p.ID)

	_, err := engine.VoteOnProposal(context.Background(), p.ID, "admin", true, "test")
	if err == nil {
		t.Error("expected error voting on non-escalated proposal")
	}
}

func TestEngineVoteValidation(t *testing.T) {
	engine, store := setupTestEngine(t, allRejectReviewer())
	p := createTestProposal(t, store)
	engine.Review(context.Background(), p.ID)

	_, err := engine.VoteOnProposal(context.Background(), p.ID, "", true, "test")
	if err == nil {
		t.Error("expected error for empty voter")
	}
	_, err = engine.VoteOnProposal(context.Background(), p.ID, "admin", true, "")
	if err == nil {
		t.Error("expected error for empty reason")
	}
}

func TestEngineWeightedConsensusIntegration(t *testing.T) {
	// Custom reviewer: heavy personas approve, light ones ask
	logger := zaptest.NewLogger(t)
	storeDir := t.TempDir()
	auditDir := t.TempDir()
	kernel.ResetInstance()
	kern, _ := kernel.GetInstance(logger, auditDir)
	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})

	store, _ := proposal.NewStore(storeDir, logger)
	personas := []*Persona{
		{Name: "CISO", Role: "security", SystemPrompt: "sec", Models: []string{"m"}, Weight: 0.4},
		{Name: "Coder", Role: "code", SystemPrompt: "code", Models: []string{"m"}, Weight: 0.4},
		{Name: "Tester", Role: "test", SystemPrompt: "test", Models: []string{"m"}, Weight: 0.2},
	}

	reviewFn := func(ctx context.Context, p *proposal.Proposal, persona *Persona) (*proposal.Review, error) {
		verdict := proposal.VerdictApprove
		risk := 3.0
		if persona.Name == "Tester" {
			verdict = proposal.VerdictAsk
			risk = 5.0
		}
		return &proposal.Review{
			ID:        uuid.New().String(),
			Persona:   persona.Name,
			Model:     "test-model",
			Round:     p.Round,
			Verdict:   verdict,
			RiskScore: risk,
			Evidence:  []string{"reviewed"},
			Questions: func() []string {
				if persona.Name == "Tester" {
					return []string{"Need more test coverage info"}
				}
				return nil
			}(),
			Comments:  "Review",
			Timestamp: time.Now().UTC(),
		}, nil
	}

	cfg := DefaultEngineConfig()
	engine, _ := NewEngine(cfg, store, kern, personas, reviewFn, logger, auditDir)

	p := createTestProposal(t, store)
	session, err := engine.Review(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}

	// CISO(0.4) + Coder(0.4) = 0.8 approve weight, total = 1.0, rate = 80% = quorum
	if session.State != SessionApproved {
		t.Errorf("expected approved with 80%% weighted approval, got %q", session.State)
	}
}
