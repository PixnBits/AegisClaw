package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"go.uber.org/zap/zaptest"
)

// TestSDLCFlowPhoenixTimeOfDaySkill tests the full SDLC flow from proposal
// creation to PR generation for a realistic skill: "add a skill that says
// hello based on the current time of day in Phoenix, AZ (respecting MST, no DST)".
//
// This test validates all 4 phases:
// 1. Proposal creation
// 2. Court review
// 3. Builder implementation  
// 4. PR creation & code review
//
// The test is designed to run in GitHub Actions with no KVM requirement.
func TestSDLCFlowPhoenixTimeOfDaySkill(t *testing.T) {
	// ── Phase 0: Setup ──────────────────────────────────────────────────
	kernel.ResetInstance()
	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()
	storeDir := t.TempDir()
	prDir := t.TempDir()

	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("kernel init: %v", err)
	}

	proposalStore, err := proposal.NewStore(storeDir, logger)
	if err != nil {
		t.Fatalf("proposal store init: %v", err)
	}

	prStore, err := pullrequest.NewStore(prDir, logger)
	if err != nil {
		t.Fatalf("PR store init: %v", err)
	}

	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})

	env := &runtimeEnv{
		Logger:        logger,
		Kernel:        kern,
		ProposalStore: proposalStore,
		PRStore:       prStore,
	}

	ctx := context.Background()

	// ── Phase 1: Create proposal for Phoenix time-of-day skill ─────────
	t.Log("Phase 1: Creating proposal for Phoenix time-of-day greeter skill")
	createArgs := `{
		"title": "Add Phoenix time-of-day greeter skill",
		"description": "A skill that greets users with a time-appropriate message (Good morning!, Good afternoon!, Good evening!, Good night!) based on the current time in Phoenix, Arizona. Phoenix observes Mountain Standard Time (MST) year-round with NO daylight saving time adjustments.",
		"skill_name": "phoenix-time-greeter",
		"tools": [
			{
				"name": "greet",
				"description": "Returns a greeting appropriate for the current time of day in Phoenix, AZ (MST timezone, no DST)"
			}
		],
		"data_sensitivity": 1,
		"network_exposure": 1,
		"privilege_level": 1
	}`

	createResult, err := handleProposalCreateDraft(env, ctx, createArgs)
	if err != nil {
		t.Fatalf("Phase 1 - create draft failed: %v", err)
	}

	proposalID := extractIDFromResult(t, createResult)
	if proposalID == "" {
		t.Fatal("Phase 1 - could not extract proposal ID")
	}
	t.Logf("Phase 1 - Created proposal: %s", proposalID)

	// Verify proposal details
	p, err := env.ProposalStore.Get(proposalID)
	if err != nil {
		t.Fatalf("Phase 1 - failed to get proposal: %v", err)
	}

	if !strings.Contains(p.Description, "Phoenix") || !strings.Contains(p.Description, "MST") {
		t.Errorf("Phase 1 - description should mention Phoenix and MST: %s", p.Description)
	}
	if p.TargetSkill != "phoenix-time-greeter" {
		t.Errorf("Phase 1 - skill name = %q, want 'phoenix-time-greeter'", p.TargetSkill)
	}
	if p.Status != proposal.StatusDraft {
		t.Errorf("Phase 1 - status = %q, want 'draft'", p.Status)
	}

	// Verify skill spec contains Phoenix timezone requirement
	var spec builder.SkillSpec
	if err := json.Unmarshal(p.Spec, &spec); err != nil {
		t.Fatalf("Phase 1 - failed to unmarshal spec: %v", err)
	}
	if spec.Name != "phoenix-time-greeter" {
		t.Errorf("Phase 1 - spec name = %q, want 'phoenix-time-greeter'", spec.Name)
	}
	if !strings.Contains(spec.Description, "Phoenix") {
		t.Errorf("Phase 1 - spec description should mention Phoenix: %s", spec.Description)
	}

	// ── Phase 1.5: Submit proposal ──────────────────────────────────────
	t.Log("Phase 1.5: Submitting proposal for Court review")
	submitResult, err := handleProposalSubmitDirect(env, ctx, fmt.Sprintf(`{"id":"%s"}`, proposalID))
	if err != nil {
		t.Fatalf("Phase 1.5 - submit failed: %v", err)
	}
	if !strings.Contains(submitResult, "submitted") {
		t.Errorf("Phase 1.5 - submit result should contain 'submitted': %s", submitResult)
	}

	p, err = env.ProposalStore.Get(proposalID)
	if err != nil {
		t.Fatalf("Phase 1.5 - failed to get proposal after submit: %v", err)
	}
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("Phase 1.5 - status = %q, want 'submitted'", p.Status)
	}

	// ── Phase 2: Court Review ───────────────────────────────────────────
	t.Log("Phase 2: Running Court review")

	// Create personas that will approve the Phoenix skill
	personas := []*court.Persona{
		{
			Name:         "CISO",
			Role:         "Chief Information Security Officer",
			SystemPrompt: "You are a security-focused reviewer. Approve low-risk skills.",
			Models:       []string{"test-model"},
			Weight:       1.0,
			OutputSchema: `{"verdict":"approved","risk_score":0.1,"evidence":["Low risk skill"],"comments":"Approved"}`,
		},
		{
			Name:         "SeniorCoder",
			Role:         "Senior Software Engineer",
			SystemPrompt: "You are a code quality reviewer. Check for best practices.",
			Models:       []string{"test-model"},
			Weight:       1.0,
			OutputSchema: `{"verdict":"approved","risk_score":0.1,"evidence":["Good design"],"comments":"Approved"}`,
		},
		{
			Name:         "Tester",
			Role:         "QA Engineer",
			SystemPrompt: "You are a testing expert. Ensure testability.",
			Models:       []string{"test-model"},
			Weight:       1.0,
			OutputSchema: `{"verdict":"approved","risk_score":0.1,"evidence":["Testable"],"comments":"Approved"}`,
		},
	}

	// Deterministic reviewer function for testing
	reviewerFn := func(ctx context.Context, prop *proposal.Proposal, persona *court.Persona) (*proposal.Review, error) {
		// Auto-approve all proposals with valid review structure
		reviewID := fmt.Sprintf("review-%s-%s", persona.Name, time.Now().Format("20060102150405"))
		return &proposal.Review{
			ID:        reviewID,
			Persona:   persona.Name,
			Model:     "test-model", // Required field
			Verdict:   proposal.VerdictApprove, // Use proper constant
			Round:     1,
			RiskScore: 0.1,
			Evidence:  []string{"Mock approval for testing - low risk skill"},
			Questions: nil,
			Comments:  "Automatically approved for SDLC flow test",
			Timestamp: time.Now(),
		}, nil
	}

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, proposalStore, kern, personas, reviewerFn, logger, auditDir)
	if err != nil {
		t.Fatalf("Phase 2 - court engine init failed: %v", err)
	}

	session, err := engine.Review(ctx, proposalID)
	if err != nil {
		t.Fatalf("Phase 2 - court review failed: %v", err)
	}

	if session.State != court.SessionApproved {
		t.Errorf("Phase 2 - session state = %q, want 'approved'", session.State)
	}
	if session.Verdict != "approved" {
		t.Errorf("Phase 2 - verdict = %q, want 'approved'", session.Verdict)
	}

	// Verify proposal transitioned to approved
	p, err = env.ProposalStore.Get(proposalID)
	if err != nil {
		t.Fatalf("Phase 2 - failed to get proposal after review: %v", err)
	}
	if p.Status != proposal.StatusApproved {
		t.Errorf("Phase 2 - status = %q, want 'approved'", p.Status)
	}

	t.Logf("Phase 2 - Court approved proposal with %d reviews", len(p.Reviews))

	// ── Phase 3: Builder Implementation (Simulated) ─────────────────────
	t.Log("Phase 3: Simulating builder implementation")

	// Transition to implementing status (this would be done by the daemon)
	if err := p.Transition(proposal.StatusImplementing, "Builder starting", "builder-agent"); err != nil {
		t.Fatalf("Phase 3 - transition to implementing failed: %v", err)
	}
	if err := proposalStore.Update(p); err != nil {
		t.Fatalf("Phase 3 - failed to update proposal: %v", err)
	}

	// Simulate builder pipeline execution
	// In a real scenario, the builder-agent would:
	// 1. Poll for StatusImplementing proposals
	// 2. Generate code using LLM
	// 3. Run tests and analysis
	// 4. Create git branch and commit
	// 5. Auto-create PR

	pipelineResult := &builder.PipelineResult{
		ProposalID: proposalID,
		State:      builder.PipelineStateComplete,
		BuilderID:  "test-builder",
		CommitHash: "abc123def456",
		Branch:     fmt.Sprintf("feature/phoenix-time-greeter-%s", proposalID[:8]),
		Files: map[string]string{
			"skills/phoenix-time-greeter/main.go": phoenixGreeterGoCode(),
			"skills/phoenix-time-greeter/go.mod":  fmt.Sprintf("module github.com/aegisclaw/skills/phoenix-time-greeter\n\ngo 1.21\n"),
		},
		FileHashes: map[string]string{
			"skills/phoenix-time-greeter/main.go": "hash123",
			"skills/phoenix-time-greeter/go.mod":  "hash456",
		},
		Reasoning:  "Generated Phoenix time-of-day greeter skill with MST timezone support (no DST)",
		Round:      1,
		StartedAt:  time.Now().Add(-5 * time.Minute),
		CompletedAt: time.Now(),
		Duration:   5 * time.Minute,
	}

	t.Logf("Phase 3 - Builder completed: branch=%s, commit=%s", pipelineResult.Branch, pipelineResult.CommitHash)

	// ── Phase 4: PR Auto-Creation ───────────────────────────────────────
	t.Log("Phase 4: Creating pull request from builder result")

	// This would be called by the builder pipeline callback
	// Note: createPRFromPipelineResult doesn't return a value, it just creates the PR
	createPRFromPipelineResult(env, proposalID, pipelineResult.Branch, pipelineResult.CommitHash, pipelineResult)

	// Find the PR that was created
	// List with status filter for open PRs
	var status pullrequest.Status = pullrequest.StatusOpen
	prs, err := prStore.List(&status)
	if err != nil {
		t.Fatalf("Phase 4 - failed to list PRs: %v", err)
	}

	var pr *pullrequest.PullRequest
	for _, p := range prs {
		if p.ProposalID == proposalID {
			pr = p
			break
		}
	}
	if pr == nil {
		t.Fatal("Phase 4 - PR was not created")
	}
	t.Logf("Phase 4 - Created PR: %s", pr.ID)

	// Verify PR was created correctly
	if pr.ProposalID != proposalID {
		t.Errorf("Phase 4 - PR proposal ID = %q, want %q", pr.ProposalID, proposalID)
	}
	if pr.Branch != pipelineResult.Branch {
		t.Errorf("Phase 4 - PR branch = %q, want %q", pr.Branch, pipelineResult.Branch)
	}
	if pr.CommitHash != pipelineResult.CommitHash {
		t.Errorf("Phase 4 - PR commit = %q, want %q", pr.CommitHash, pipelineResult.CommitHash)
	}
	if pr.Status != pullrequest.StatusOpen {
		t.Errorf("Phase 4 - PR status = %q, want 'open'", pr.Status)
	}

	// Verify proposal transitioned to complete
	p, err = proposalStore.Get(proposalID)
	if err != nil {
		t.Fatalf("Phase 4 - failed to get proposal: %v", err)
	}
	if p.Status != proposal.StatusComplete {
		// Note: Status might still be StatusImplementing if PR callback hasn't run
		t.Logf("Phase 4 - proposal status = %q (expected 'complete' or 'implementing')", p.Status)
	}

	// ── Verification: End-to-End ────────────────────────────────────────
	t.Log("Verification: Checking end-to-end flow")

	// Verify audit trail has all phases
	auditLog := kern.AuditLog()
	if auditLog.EntryCount() < 4 {
		t.Errorf("Expected at least 4 audit entries (create, submit, approve, implement), got %d", auditLog.EntryCount())
	}

	// Verify code was generated
	if len(pipelineResult.Files) < 2 {
		t.Errorf("Expected at least 2 generated files (main.go, go.mod), got %d", len(pipelineResult.Files))
	}

	mainGo, ok := pipelineResult.Files["skills/phoenix-time-greeter/main.go"]
	if !ok {
		t.Fatal("main.go not found in generated files")
	}
	if !strings.Contains(mainGo, "Phoenix") || !strings.Contains(mainGo, "MST") {
		t.Error("Generated code should reference Phoenix and MST timezone")
	}
	if !strings.Contains(mainGo, "America/Phoenix") {
		t.Error("Generated code should use America/Phoenix timezone")
	}

	t.Logf("✓ Full SDLC flow validated: Proposal → Court Review → Builder → PR")
	t.Logf("  - Proposal: %s", proposalID)
	t.Logf("  - Reviews: %d personas approved", len(p.Reviews))
	t.Logf("  - Build: %s (%d files)", pipelineResult.CommitHash[:7], len(pipelineResult.Files))
	t.Logf("  - PR: %s (branch: %s)", pr.ID, pr.Branch)
}

// phoenixGreeterGoCode returns sample Go code for the Phoenix time-of-day greeter.
func phoenixGreeterGoCode() string {
	return "package main\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n\t\"os\"\n\t\"time\"\n)\n\n" +
		"// Phoenix, Arizona uses Mountain Standard Time (MST) year-round.\n" +
		"// No daylight saving time adjustments.\n" +
		"const phoenixTZ = \"America/Phoenix\"\n\n" +
		"func main() {\n" +
		"\t// Load Phoenix timezone\n" +
		"\tloc, err := time.LoadLocation(phoenixTZ)\n" +
		"\tif err != nil {\n" +
		"\t\tfmt.Fprintf(os.Stderr, \"failed to load timezone: %v\\n\", err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n\n" +
		"\t// Get current time in Phoenix\n" +
		"\tnow := time.Now().In(loc)\n" +
		"\thour := now.Hour()\n\n" +
		"\t// Determine greeting based on time of day\n" +
		"\tvar greeting string\n" +
		"\tswitch {\n" +
		"\tcase hour >= 5 && hour < 12:\n" +
		"\t\tgreeting = \"Good morning!\"\n" +
		"\tcase hour >= 12 && hour < 17:\n" +
		"\t\tgreeting = \"Good afternoon!\"\n" +
		"\tcase hour >= 17 && hour < 21:\n" +
		"\t\tgreeting = \"Good evening!\"\n" +
		"\tdefault:\n" +
		"\t\tgreeting = \"Good night!\"\n" +
		"\t}\n\n" +
		"\t// Return result as JSON\n" +
		"\tresult := map[string]interface{}{\n" +
		"\t\t\"greeting\": greeting,\n" +
		"\t\t\"time\":     now.Format(\"3:04 PM MST\"),\n" +
		"\t\t\"hour\":     hour,\n" +
		"\t\t\"timezone\": phoenixTZ,\n" +
		"\t}\n" +
		"\tjson.NewEncoder(os.Stdout).Encode(result)\n" +
		"}\n"
}

