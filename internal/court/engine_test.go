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

func setupTestEngine(t *testing.T, reviewerFn ReviewerFunc) (*Engine, *proposal.Store) {
	t.Helper()
	logger := zaptest.NewLogger(t)

	// Create temp dirs
	storeDir := t.TempDir()
	auditDir := t.TempDir()

	// Initialize kernel
	kernel.ResetInstance()
	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("kernel init failed: %v", err)
	}

	store, err := proposal.NewStore(storeDir, logger)
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}

	personas := []*Persona{
		{Name: "CISO", Role: "security", SystemPrompt: "Review security", Models: []string{"test-model"}, Weight: 0.3},
		{Name: "SeniorCoder", Role: "code_quality", SystemPrompt: "Review code", Models: []string{"test-model"}, Weight: 0.3},
		{Name: "Tester", Role: "test_coverage", SystemPrompt: "Review tests", Models: []string{"test-model"}, Weight: 0.2},
		{Name: "SecurityArchitect", Role: "architecture", SystemPrompt: "Review architecture", Models: []string{"test-model"}, Weight: 0.1},
		{Name: "UserAdvocate", Role: "usability", SystemPrompt: "Review UX", Models: []string{"test-model"}, Weight: 0.1},
	}

	cfg := DefaultEngineConfig()
	engine, err := NewEngine(cfg, store, kern, personas, reviewerFn, logger)
	if err != nil {
		t.Fatalf("engine init failed: %v", err)
	}

	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})

	return engine, store
}

func createTestProposal(t *testing.T, store *proposal.Store) *proposal.Proposal {
	t.Helper()
	p, err := proposal.NewProposal("Test Proposal", "A proposal for testing", proposal.CategoryNewSkill, "admin")
	if err != nil {
		t.Fatalf("NewProposal failed: %v", err)
	}
	if err := store.Create(p); err != nil {
		t.Fatalf("store.Create failed: %v", err)
	}
	if err := p.Transition(proposal.StatusSubmitted, "ready for review", "admin"); err != nil {
		t.Fatalf("transition to submitted failed: %v", err)
	}
	if err := store.Update(p); err != nil {
		t.Fatalf("store.Update failed: %v", err)
	}
	return p
}

// allApproveReviewer returns a ReviewerFunc where all personas approve with low risk.
func allApproveReviewer() ReviewerFunc {
	return func(ctx context.Context, p *proposal.Proposal, persona *Persona) (*proposal.Review, error) {
		return &proposal.Review{
			ID:        uuid.New().String(),
			Persona:   persona.Name,
			Model:     persona.Models[0],
			Round:     1,
			Verdict:   proposal.VerdictApprove,
			RiskScore: 2.0,
			Evidence:  []string{"No issues found"},
			Comments:  "Approved",
			Timestamp: time.Now().UTC(),
		}, nil
	}
}

// allRejectReviewer returns a ReviewerFunc where all personas reject with high risk.
func allRejectReviewer() ReviewerFunc {
	return func(ctx context.Context, p *proposal.Proposal, persona *Persona) (*proposal.Review, error) {
		return &proposal.Review{
			ID:        uuid.New().String(),
			Persona:   persona.Name,
			Model:     persona.Models[0],
			Round:     1,
			Verdict:   proposal.VerdictReject,
			RiskScore: 9.0,
			Evidence:  []string{"Critical security flaw"},
			Comments:  "Rejected",
			Timestamp: time.Now().UTC(),
		}, nil
	}
}

// splitReviewer returns mixed results: first 4 approve, last one rejects.
func splitReviewer() ReviewerFunc {
	var mu = new(int)
	_ = mu
	callCount := 0
	return func(ctx context.Context, p *proposal.Proposal, persona *Persona) (*proposal.Review, error) {
		callCount++
		verdict := proposal.VerdictApprove
		risk := 3.0
		if persona.Name == "UserAdvocate" {
			verdict = proposal.VerdictReject
			risk = 8.0
		}
		return &proposal.Review{
			ID:        uuid.New().String(),
			Persona:   persona.Name,
			Model:     persona.Models[0],
			Round:     1,
			Verdict:   verdict,
			RiskScore: risk,
			Evidence:  []string{"Reviewed"},
			Comments:  "Review complete",
			Timestamp: time.Now().UTC(),
		}, nil
	}
}

func TestEngineApproval(t *testing.T) {
	engine, store := setupTestEngine(t, allApproveReviewer())
	p := createTestProposal(t, store)

	session, err := engine.Review(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}
	if session.State != SessionApproved {
		t.Errorf("expected approved, got %q", session.State)
	}
	if session.Verdict != "approved" {
		t.Errorf("expected verdict approved, got %q", session.Verdict)
	}
	if session.Round != 1 {
		t.Errorf("expected 1 round, got %d", session.Round)
	}
	if session.EndedAt == nil {
		t.Error("expected session to be finalized with EndedAt")
	}

	// Verify proposal was persisted as approved
	loaded, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if loaded.Status != proposal.StatusApproved {
		t.Errorf("expected proposal status %q, got %q", proposal.StatusApproved, loaded.Status)
	}
}

func TestEngineRejection(t *testing.T) {
	engine, store := setupTestEngine(t, allRejectReviewer())
	p := createTestProposal(t, store)

	session, err := engine.Review(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}

	// All reject but consensus is on rejection -> state depends on risk logic
	// With 0% approval rate < 80% quorum, no consensus. After 3 rounds -> escalated.
	if session.State != SessionEscalated {
		t.Errorf("expected escalated (no consensus), got %q", session.State)
	}
}

func TestEngineSplitVote(t *testing.T) {
	engine, store := setupTestEngine(t, splitReviewer())
	p := createTestProposal(t, store)

	session, err := engine.Review(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}

	// 4/5 approve = 0.8 = meets quorum. Should be approved.
	if session.State != SessionApproved {
		t.Errorf("expected approved with 4/5 approval, got %q", session.State)
	}
}

func TestEngineGetSession(t *testing.T) {
	engine, store := setupTestEngine(t, allApproveReviewer())
	p := createTestProposal(t, store)

	session, _ := engine.Review(context.Background(), p.ID)
	found, ok := engine.GetSession(session.ID)
	if !ok {
		t.Fatal("session not found")
	}
	if found.ID != session.ID {
		t.Error("session ID mismatch")
	}
}

func TestEngineRiskHeatmap(t *testing.T) {
	engine, store := setupTestEngine(t, allApproveReviewer())
	p := createTestProposal(t, store)

	session, _ := engine.Review(context.Background(), p.ID)
	heatmap, err := engine.RiskHeatmap(session.ID)
	if err != nil {
		t.Fatalf("RiskHeatmap failed: %v", err)
	}
	if len(heatmap) != 5 {
		t.Errorf("expected 5 entries in heatmap, got %d", len(heatmap))
	}
	for persona, risk := range heatmap {
		if risk != 2.0 {
			t.Errorf("expected risk 2.0 for %s, got %f", persona, risk)
		}
	}
}

func TestEngineInvalidProposalState(t *testing.T) {
	engine, store := setupTestEngine(t, allApproveReviewer())

	// Create a proposal but do NOT submit it
	p, _ := proposal.NewProposal("Not Submitted", "Still in draft", proposal.CategoryNewSkill, "admin")
	if err := store.Create(p); err != nil {
		t.Fatal(err)
	}

	_, err := engine.Review(context.Background(), p.ID)
	if err == nil {
		t.Error("expected error for draft proposal")
	}
}

func TestEngineNilDeps(t *testing.T) {
	logger := zaptest.NewLogger(t)
	personas := []*Persona{{Name: "CISO", Role: "sec", SystemPrompt: "check", Models: []string{"m"}, Weight: 0.5}}
	reviewFn := allApproveReviewer()
	cfg := DefaultEngineConfig()

	_, err := NewEngine(cfg, nil, nil, personas, reviewFn, logger)
	if err == nil {
		t.Error("expected error for nil store")
	}

	storeDir := t.TempDir()
	store, _ := proposal.NewStore(storeDir, logger)
	_, err = NewEngine(cfg, store, nil, personas, reviewFn, logger)
	if err == nil {
		t.Error("expected error for nil kernel")
	}
}

func TestEngineActiveSessions(t *testing.T) {
	engine, store := setupTestEngine(t, allApproveReviewer())
	p := createTestProposal(t, store)

	// Before review, no active sessions
	if len(engine.ActiveSessions()) != 0 {
		t.Error("expected no active sessions before review")
	}

	// After review completes (approval), the session is finalized
	engine.Review(context.Background(), p.ID)
	active := engine.ActiveSessions()
	if len(active) != 0 {
		t.Errorf("expected 0 active sessions after approval, got %d", len(active))
	}
}

func TestEngineConfigValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	storeDir := t.TempDir()
	auditDir := t.TempDir()
	kernel.ResetInstance()
	kern, _ := kernel.GetInstance(logger, auditDir)
	defer func() {
		kern.Shutdown()
		kernel.ResetInstance()
	}()
	store, _ := proposal.NewStore(storeDir, logger)
	personas := []*Persona{{Name: "CISO", Role: "sec", SystemPrompt: "check", Models: []string{"m"}, Weight: 0.5}}
	reviewFn := allApproveReviewer()

	// Invalid max rounds
	cfg := DefaultEngineConfig()
	cfg.MaxRounds = 0
	_, err := NewEngine(cfg, store, kern, personas, reviewFn, logger)
	if err == nil {
		t.Error("expected error for MaxRounds=0")
	}

	// Invalid quorum
	cfg = DefaultEngineConfig()
	cfg.ConsensusQuorum = 1.5
	_, err = NewEngine(cfg, store, kern, personas, reviewFn, logger)
	if err == nil {
		t.Error("expected error for ConsensusQuorum > 1")
	}

	// Invalid risk threshold
	cfg = DefaultEngineConfig()
	cfg.MaxRiskThreshold = 0
	_, err = NewEngine(cfg, store, kern, personas, reviewFn, logger)
	if err == nil {
		t.Error("expected error for MaxRiskThreshold=0")
	}
}
