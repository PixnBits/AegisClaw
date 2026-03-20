package court

import (
	"context"
	"fmt"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

// mockLauncher implements SandboxLauncher for testing without Firecracker.
type mockLauncher struct {
	responses   map[string]*ReviewResponse
	launchErr   error
	sendErr     error
	stopErr     error
	launchCount int
	stopCount   int
}

func newMockLauncher() *mockLauncher {
	return &mockLauncher{
		responses: make(map[string]*ReviewResponse),
	}
}

func (m *mockLauncher) SetResponse(model string, resp *ReviewResponse) {
	m.responses[model] = resp
}

func (m *mockLauncher) LaunchReviewer(ctx context.Context, persona *Persona, model string) (string, error) {
	m.launchCount++
	if m.launchErr != nil {
		return "", m.launchErr
	}
	return uuid.New().String(), nil
}

func (m *mockLauncher) SendReviewRequest(ctx context.Context, sandboxID string, req *ReviewRequest) (*ReviewResponse, error) {
	if m.sendErr != nil {
		return nil, m.sendErr
	}
	if resp, ok := m.responses[req.Model]; ok {
		return resp, nil
	}
	return &ReviewResponse{
		Verdict:   "approve",
		RiskScore: 3.0,
		Evidence:  []string{"Default mock response"},
		Comments:  "Mock review from " + req.Model,
	}, nil
}

func (m *mockLauncher) StopReviewer(ctx context.Context, sandboxID string) error {
	m.stopCount++
	return m.stopErr
}

func TestReviewerExecuteApproval(t *testing.T) {
	logger := zaptest.NewLogger(t)
	launcher := newMockLauncher()
	reviewer := NewReviewer(launcher, 2, logger)

	persona := &Persona{
		Name:         "CISO",
		Role:         "security",
		SystemPrompt: "Review security",
		Models:       []string{"model-a", "model-b"},
		Weight:       0.3,
	}

	p, _ := proposal.NewProposal("Test", "Testing reviewer", proposal.CategoryNewSkill, "admin")
	p.Round = 1

	review, err := reviewer.Execute(context.Background(), p, persona)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if review.Persona != "CISO" {
		t.Errorf("expected persona CISO, got %s", review.Persona)
	}
	if review.Verdict != proposal.VerdictApprove {
		t.Errorf("expected approve, got %s", review.Verdict)
	}
	if launcher.launchCount != 2 {
		t.Errorf("expected 2 launches, got %d", launcher.launchCount)
	}
	if launcher.stopCount != 2 {
		t.Errorf("expected 2 stops, got %d", launcher.stopCount)
	}
}

func TestReviewerCrossVerifyDisagreement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	launcher := newMockLauncher()
	launcher.SetResponse("model-a", &ReviewResponse{
		Verdict:   "approve",
		RiskScore: 2.0,
		Evidence:  []string{"Looks safe"},
		Comments:  "Model A approves",
	})
	launcher.SetResponse("model-b", &ReviewResponse{
		Verdict:   "reject",
		RiskScore: 8.0,
		Evidence:  []string{"Security flaw found"},
		Comments:  "Model B rejects",
	})

	reviewer := NewReviewer(launcher, 2, logger)
	persona := &Persona{
		Name:         "CISO",
		Role:         "security",
		SystemPrompt: "Review",
		Models:       []string{"model-a", "model-b"},
		Weight:       0.3,
	}

	p, _ := proposal.NewProposal("Disagreement Test", "Models will disagree", proposal.CategoryNewSkill, "admin")
	p.Round = 1

	review, err := reviewer.Execute(context.Background(), p, persona)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// When models disagree and one rejects, the verdict should be reject (safety first)
	if review.Verdict != proposal.VerdictReject {
		t.Errorf("expected reject on disagreement with rejection, got %s", review.Verdict)
	}

	// Risk should be average of both
	expectedRisk := 5.0
	if review.RiskScore != expectedRisk {
		t.Errorf("expected risk %f, got %f", expectedRisk, review.RiskScore)
	}
}

func TestReviewerInsufficientModels(t *testing.T) {
	logger := zaptest.NewLogger(t)
	launcher := newMockLauncher()
	reviewer := NewReviewer(launcher, 2, logger)

	persona := &Persona{
		Name:         "CISO",
		Role:         "security",
		SystemPrompt: "Review",
		Models:       []string{"single-model"},
		Weight:       0.3,
	}

	p, _ := proposal.NewProposal("Test", "One model only", proposal.CategoryNewSkill, "admin")
	p.Round = 1

	// With fewer models than minModels the reviewer should still succeed
	// (warn, not error) — cross-verification is best-effort.
	result, err := reviewer.Execute(context.Background(), p, persona)
	if err != nil {
		t.Fatalf("unexpected error for insufficient models: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestReviewerLaunchFailure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	launcher := newMockLauncher()
	launcher.launchErr = fmt.Errorf("sandbox creation failed")
	reviewer := NewReviewer(launcher, 2, logger)

	persona := &Persona{
		Name:         "CISO",
		Role:         "security",
		SystemPrompt: "Review",
		Models:       []string{"model-a", "model-b"},
		Weight:       0.3,
	}

	p, _ := proposal.NewProposal("Test", "Launch will fail", proposal.CategoryNewSkill, "admin")
	p.Round = 1

	_, err := reviewer.Execute(context.Background(), p, persona)
	if err == nil {
		t.Error("expected error when all launches fail")
	}
}

func TestReviewerSendFailure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	launcher := newMockLauncher()
	launcher.sendErr = fmt.Errorf("vsock communication failed")
	reviewer := NewReviewer(launcher, 2, logger)

	persona := &Persona{
		Name:         "CISO",
		Role:         "security",
		SystemPrompt: "Review",
		Models:       []string{"model-a", "model-b"},
		Weight:       0.3,
	}

	p, _ := proposal.NewProposal("Test", "Send will fail", proposal.CategoryNewSkill, "admin")
	p.Round = 1

	_, err := reviewer.Execute(context.Background(), p, persona)
	if err == nil {
		t.Error("expected error when all sends fail")
	}
}

func TestReviewResponseValidation(t *testing.T) {
	tests := []struct {
		name    string
		resp    ReviewResponse
		wantErr bool
	}{
		{"valid approve", ReviewResponse{Verdict: "approve", RiskScore: 3, Evidence: []string{"ok"}}, false},
		{"valid reject", ReviewResponse{Verdict: "reject", RiskScore: 8, Evidence: []string{"issue"}}, false},
		{"invalid verdict", ReviewResponse{Verdict: "maybe", RiskScore: 3, Evidence: []string{"ok"}}, true},
		{"risk too high", ReviewResponse{Verdict: "approve", RiskScore: 11, Evidence: []string{"ok"}}, true},
		{"risk too low", ReviewResponse{Verdict: "approve", RiskScore: -1, Evidence: []string{"ok"}}, true},
		{"no evidence", ReviewResponse{Verdict: "approve", RiskScore: 3}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.resp.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReviewRequestFields(t *testing.T) {
	req := ReviewRequest{
		ProposalID:  "test-id",
		Title:       "Test",
		Description: "Test Desc",
		Category:    "new_skill",
		PersonaName: "CISO",
		PersonaRole: "security",
		Prompt:      "Evaluate this",
		Model:       "test-model",
		Round:       1,
	}
	if req.ProposalID != "test-id" {
		t.Error("unexpected proposal ID")
	}
	if req.Round != 1 {
		t.Error("unexpected round")
	}
}

func TestNewReviewerFunc(t *testing.T) {
	logger := zaptest.NewLogger(t)
	launcher := newMockLauncher()
	reviewer := NewReviewer(launcher, 2, logger)

	fn := NewReviewerFunc(reviewer)
	persona := &Persona{
		Name:         "CISO",
		Role:         "security",
		SystemPrompt: "Review",
		Models:       []string{"model-a", "model-b"},
		Weight:       0.3,
	}

	p, _ := proposal.NewProposal("Test", "Testing adapter", proposal.CategoryNewSkill, "admin")
	p.Round = 1

	review, err := fn(context.Background(), p, persona)
	if err != nil {
		t.Fatalf("ReviewerFunc failed: %v", err)
	}
	if review.Persona != "CISO" {
		t.Errorf("expected CISO, got %s", review.Persona)
	}
}
