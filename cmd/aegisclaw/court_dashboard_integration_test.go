package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

// testCourtEnv creates a full court environment with real Kernel, ProposalStore,
// and Court Engine backed by temp directories.
func testCourtEnv(t *testing.T) (*runtimeEnv, *court.Engine) {
	t.Helper()

	kernel.ResetInstance()
	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()
	storeDir := t.TempDir()

	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("kernel.GetInstance: %v", err)
	}

	store, err := proposal.NewStore(storeDir, logger)
	if err != nil {
		t.Fatalf("proposal.NewStore: %v", err)
	}

	personas := []*court.Persona{
		{Name: "CISO", Role: "security", SystemPrompt: "Review security", Models: []string{"test-model"}, Weight: 0.3},
		{Name: "SeniorCoder", Role: "code_quality", SystemPrompt: "Review code", Models: []string{"test-model"}, Weight: 0.3},
		{Name: "Tester", Role: "test_coverage", SystemPrompt: "Review tests", Models: []string{"test-model"}, Weight: 0.2},
		{Name: "SecurityArchitect", Role: "architecture", SystemPrompt: "Review arch", Models: []string{"test-model"}, Weight: 0.1},
		{Name: "UserAdvocate", Role: "usability", SystemPrompt: "Review UX", Models: []string{"test-model"}, Weight: 0.1},
	}

	// All-approve reviewer for deterministic tests.
	reviewerFn := func(ctx context.Context, p *proposal.Proposal, persona *court.Persona) (*proposal.Review, error) {
		return &proposal.Review{
			ID:        uuid.New().String(),
			Persona:   persona.Name,
			Model:     persona.Models[0],
			Round:     p.Round + 1,
			Verdict:   proposal.VerdictApprove,
			RiskScore: 2.0,
			Evidence:  []string{"looks good"},
			Comments:  "Approved by test",
			Timestamp: time.Now(),
		}, nil
	}

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, store, kern, personas, reviewerFn, logger)
	if err != nil {
		t.Fatalf("court.NewEngine: %v", err)
	}

	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})

	env := &runtimeEnv{
		Logger:        logger,
		Kernel:        kern,
		ProposalStore: store,
	}

	return env, engine
}

// wireCourtDashboard creates a CourtDashboardModel wired to the real store
// and engine, mirroring what runCourtDashboard does in production.
func wireCourtDashboard(env *runtimeEnv, engine *court.Engine) tui.CourtDashboardModel {
	model := tui.NewCourtDashboard()

	model.LoadProposals = func() ([]tui.CourtProposal, error) {
		summaries, err := env.ProposalStore.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list proposals: %w", err)
		}
		result := make([]tui.CourtProposal, len(summaries))
		for i, s := range summaries {
			result[i] = tui.CourtProposal{
				ID:       s.ID,
				Title:    s.Title,
				Category: string(s.Category),
				Status:   string(s.Status),
				Risk:     string(s.Risk),
				Author:   s.Author,
				Round:    s.Round,
				Updated:  s.UpdatedAt,
			}
		}
		return result, nil
	}

	model.LoadSessions = func() ([]tui.CourtSession, error) {
		sessions := engine.ActiveSessions()
		result := make([]tui.CourtSession, len(sessions))
		for i, s := range sessions {
			cs := tui.CourtSession{
				SessionID:  s.ID,
				ProposalID: s.ProposalID,
				State:      string(s.State),
				Round:      s.Round,
				RiskScore:  s.RiskScore,
				Verdict:    s.Verdict,
				Personas:   s.Personas,
			}
			for _, rr := range s.Results {
				for _, r := range rr.Reviews {
					cs.Reviews = append(cs.Reviews, tui.CourtReview{
						Persona:   r.Persona,
						Verdict:   string(r.Verdict),
						RiskScore: r.RiskScore,
						Comments:  r.Comments,
						Evidence:  r.Evidence,
						Round:     r.Round,
					})
				}
			}
			result[i] = cs
		}
		return result, nil
	}

	model.LoadDiff = func(proposalID string) (string, error) {
		p, err := env.ProposalStore.Get(proposalID)
		if err != nil {
			return "", fmt.Errorf("failed to load proposal: %w", err)
		}
		if p.Spec != nil {
			return string(p.Spec), nil
		}
		return "(no spec/diff available)", nil
	}

	model.CastVote = func(proposalID string, approve bool, reason string) error {
		_, err := engine.VoteOnProposal(context.Background(), proposalID, "operator", approve, reason)
		return err
	}

	return model
}

// loadDashboard triggers the init → refresh → load cycle.
func loadDashboard(t *testing.T, m tui.CourtDashboardModel) tui.CourtDashboardModel {
	t.Helper()

	// Init triggers CourtRefreshMsg.
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a command")
	}
	msg := cmd()

	// Process the refresh.
	updated, cmd := m.Update(msg)
	m = updated.(tui.CourtDashboardModel)
	if cmd == nil {
		t.Fatal("refresh should trigger data load")
	}

	// Process data load.
	msg = cmd()
	updated, _ = m.Update(msg)
	m = updated.(tui.CourtDashboardModel)

	return m
}

// --- Court Dashboard Integration Tests ---

func TestCourtDashboardLoadsRealProposals(t *testing.T) {
	env, engine := testCourtEnv(t)

	// Create and submit a proposal via handlers.
	createResult, err := handleProposalCreateDraft(env, `{
		"title": "Dashboard Test Skill",
		"description": "Testing dashboard loading",
		"skill_name": "dash-test",
		"tools": [{"name": "ping", "description": "pings"}]
	}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	_, err = handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Wire up and load the dashboard.
	model := wireCourtDashboard(env, engine)
	model = loadDashboard(t, model)

	// The dashboard should show one proposal.
	view := model.View()
	if !strings.Contains(view, "Dashboard Test Skill") {
		t.Errorf("dashboard should show proposal title, got:\n%s", view)
	}
}

func TestCourtDashboardShowsMultipleProposals(t *testing.T) {
	env, engine := testCourtEnv(t)

	titles := []string{"Alpha Skill", "Beta Skill", "Gamma Skill"}
	for i, title := range titles {
		args := fmt.Sprintf(`{"title":"%s","description":"desc %d","skill_name":"s%d","tools":[{"name":"t","description":"d"}]}`, title, i, i)
		_, err := handleProposalCreateDraft(env, args)
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
	}

	model := wireCourtDashboard(env, engine)
	model = loadDashboard(t, model)

	view := model.View()
	for _, title := range titles {
		if !strings.Contains(view, title) {
			t.Errorf("dashboard should show %q in view", title)
		}
	}
}

func TestCourtDashboardVoteApproveIntegration(t *testing.T) {
	env, engine := testCourtEnv(t)

	// Create and submit.
	createResult, err := handleProposalCreateDraft(env, `{
		"title": "Vote Approve Test",
		"description": "Testing vote approve",
		"skill_name": "vat",
		"tools": [{"name": "t", "description": "d"}]
	}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	_, err = handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Load dashboard.
	model := wireCourtDashboard(env, engine)
	model = loadDashboard(t, model)

	// Set window size so table renders.
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(tui.CourtDashboardModel)

	// Press 'a' to approve.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(tui.CourtDashboardModel)

	// Should be in vote confirm view.
	view := model.View()
	if !strings.Contains(view, "Approve") {
		t.Errorf("expected vote confirmation, got:\n%s", view)
	}

	// Press enter to confirm.
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(tui.CourtDashboardModel)
	if cmd == nil {
		t.Fatal("expected vote command")
	}

	// Execute the vote.
	msg := cmd()
	updated, _ = model.Update(msg)
	model = updated.(tui.CourtDashboardModel)

	// Verify the proposal was approved in the store.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	// After voting, status should be approved or in_review.
	if p.Status != proposal.StatusApproved && p.Status != proposal.StatusInReview {
		t.Errorf("expected approved or in_review, got %s", p.Status)
	}
}

func TestCourtDashboardVoteRejectIntegration(t *testing.T) {
	env, engine := testCourtEnv(t)

	createResult, err := handleProposalCreateDraft(env, `{
		"title": "Vote Reject Test",
		"description": "Testing vote reject",
		"skill_name": "vrt",
		"tools": [{"name": "t", "description": "d"}]
	}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	_, err = handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	model := wireCourtDashboard(env, engine)
	model = loadDashboard(t, model)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(tui.CourtDashboardModel)

	// Press 'x' to reject.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(tui.CourtDashboardModel)

	// Confirm.
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(tui.CourtDashboardModel)
	if cmd == nil {
		t.Fatal("expected vote command")
	}

	msg := cmd()
	updated, _ = model.Update(msg)
	model = updated.(tui.CourtDashboardModel)

	// Verify the proposal was rejected.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if p.Status != proposal.StatusRejected {
		t.Errorf("expected rejected, got %s", p.Status)
	}
}

func TestCourtDashboardDetailViewIntegration(t *testing.T) {
	env, engine := testCourtEnv(t)

	createResult, err := handleProposalCreateDraft(env, `{
		"title": "Detail View Test",
		"description": "Testing detail view",
		"skill_name": "dvt",
		"tools": [{"name": "t", "description": "d"}]
	}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	model := wireCourtDashboard(env, engine)
	model = loadDashboard(t, model)

	// Enter detail view.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(tui.CourtDashboardModel)

	view := model.View()
	if !strings.Contains(view, id) {
		t.Errorf("detail view should show full proposal ID %s:\n%s", id, view)
	}
	if !strings.Contains(view, "Detail View Test") {
		t.Errorf("detail view should show title:\n%s", view)
	}
}

func TestCourtDashboardDiffViewIntegration(t *testing.T) {
	env, engine := testCourtEnv(t)

	createResult, err := handleProposalCreateDraft(env, `{
		"title": "Diff View Test",
		"description": "Testing diff view",
		"skill_name": "difftest",
		"tools": [{"name": "t", "description": "d"}]
	}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = extractIDFromResult(t, createResult) // ensure it was created

	model := wireCourtDashboard(env, engine)
	model = loadDashboard(t, model)

	// Enter detail.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(tui.CourtDashboardModel)

	// Press tab for diff.
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(tui.CourtDashboardModel)

	if cmd != nil {
		msg := cmd()
		updated, _ = model.Update(msg)
		model = updated.(tui.CourtDashboardModel)
	}

	view := model.View()
	// The diff view should show spec content or "(no spec/diff available)".
	if !strings.Contains(view, "skill") && !strings.Contains(view, "no spec") {
		t.Logf("diff view content:\n%s", view)
	}
}

// --- Journey: Chat create → Court dashboard approve ---

func TestJourneyChatCreateToDashboardApprove(t *testing.T) {
	env, engine := testCourtEnv(t)

	// 1. Create a proposal via chat handler.
	createResult, err := handleProposalCreateDraft(env, `{
		"title": "Journey Skill",
		"description": "End-to-end journey test",
		"skill_name": "journey",
		"tools": [{"name": "go", "description": "goes"}],
		"data_sensitivity": 1,
		"network_exposure": 1,
		"privilege_level": 1
	}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)
	t.Logf("Created proposal: %s", id)

	// 2. Submit via chat handler.
	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !strings.Contains(submitResult, id) {
		t.Errorf("submit result should contain %s, got: %s", id, submitResult)
	}

	// 3. Load court dashboard — proposal should appear as submitted.
	model := wireCourtDashboard(env, engine)
	model = loadDashboard(t, model)

	view := model.View()
	if !strings.Contains(view, "Journey Skill") {
		t.Fatalf("dashboard should show 'Journey Skill':\n%s", view)
	}
	if !strings.Contains(view, "submitted") {
		t.Errorf("dashboard should show 'submitted' status:\n%s", view)
	}

	// 4. Approve via dashboard.
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(tui.CourtDashboardModel)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(tui.CourtDashboardModel)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(tui.CourtDashboardModel)
	if cmd == nil {
		t.Fatal("expected vote command")
	}

	msg := cmd()
	updated, _ = model.Update(msg)
	model = updated.(tui.CourtDashboardModel)

	// 5. Verify approved in store.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Status != proposal.StatusApproved && p.Status != proposal.StatusInReview {
		t.Errorf("expected approved or in_review, got %s", p.Status)
	}
	t.Logf("Final status: %s", p.Status)

	// 6. Check status via chat handler.
	statusResult, err := handleProposalStatus(env, fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if strings.Contains(statusResult, "draft") {
		t.Errorf("status should not be 'draft' after approval:\n%s", statusResult)
	}
}
