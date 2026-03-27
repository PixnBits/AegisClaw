package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

// TestFirstSkillTutorialJourney exercises the same flow described in
// docs/first-skill-tutorial.md: create a "time-of-day greeter" skill via the
// chat handler, verify the spec, submit for court review, and confirm approval.
//
// This test does NOT require Ollama or Firecracker — it uses the in-process
// proposal handlers and a deterministic all-approve Court reviewer.
func TestFirstSkillTutorialJourney(t *testing.T) {
	// ── Step 1: Initialize runtime (mirrors "aegisclaw init") ──────────
	kernel.ResetInstance()
	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()
	storeDir := t.TempDir()

	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("kernel init: %v", err)
	}

	store, err := proposal.NewStore(storeDir, logger)
	if err != nil {
		t.Fatalf("proposal store init: %v", err)
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

	// ── Step 2: Create draft proposal via chat handler ─────────────────
	// This mirrors what the main agent does when the user types:
	//   "please add a skill that says hello to the user with a message
	//    appropriate for the time of day ... respecting DST, in en-US"
	createArgs := `{
		"title": "Add time-of-day greeter skill",
		"description": "A skill that greets the user with a time-appropriate message (good morning, good afternoon, good evening, good night) based on the current local time, respecting DST, in en-US locale.",
		"skill_name": "time-of-day-greeter",
		"tools": [
			{
				"name": "greet",
				"description": "Returns a locale-aware, DST-respecting greeting appropriate for the current time of day (e.g. Good morning!, Good evening!)"
			}
		],
		"data_sensitivity": 1,
		"network_exposure": 1,
		"privilege_level": 1
	}`
	createResult, err := handleProposalCreateDraft(env, createArgs)
	if err != nil {
		t.Fatalf("Step 2 - create draft: %v", err)
	}

	// Extract and validate the proposal ID.
	proposalID := extractIDFromResult(t, createResult)
	if proposalID == "" {
		t.Fatal("Step 2 - could not extract proposal ID from result")
	}
	t.Logf("Created proposal: %s", proposalID)

	if !strings.Contains(createResult, "Draft proposal created") {
		t.Errorf("Step 2 - expected 'Draft proposal created', got: %s", createResult)
	}
	if !strings.Contains(createResult, "time-of-day-greeter") {
		t.Errorf("Step 2 - expected skill name in result, got: %s", createResult)
	}

	// ── Step 3: Verify proposal fields in the store ────────────────────
	p, err := env.ProposalStore.Get(proposalID)
	if err != nil {
		t.Fatalf("Step 3 - store.Get: %v", err)
	}

	if p.Title != "Add time-of-day greeter skill" {
		t.Errorf("Step 3 - title = %q, want 'Add time-of-day greeter skill'", p.Title)
	}
	if p.TargetSkill != "time-of-day-greeter" {
		t.Errorf("Step 3 - skill name = %q, want 'time-of-day-greeter'", p.TargetSkill)
	}
	if p.Status != proposal.StatusDraft {
		t.Errorf("Step 3 - status = %q, want 'draft'", p.Status)
	}
	if p.Category != proposal.CategoryNewSkill {
		t.Errorf("Step 3 - category = %q, want 'new_skill'", p.Category)
	}
	if string(p.Risk) != "low" {
		t.Errorf("Step 3 - risk = %q, want 'low'", p.Risk)
	}

	// Verify the SkillSpec contains the greet tool.
	if len(p.Spec) == 0 {
		t.Fatal("Step 3 - spec is empty")
	}
	specStr := string(p.Spec)
	if !strings.Contains(specStr, "greet") {
		t.Errorf("Step 3 - spec missing 'greet' tool:\n%s", specStr)
	}
	if !strings.Contains(specStr, "time-of-day-greeter") {
		t.Errorf("Step 3 - spec missing skill name:\n%s", specStr)
	}

	// Verify network policy is default-deny with no allowed hosts.
	if p.NetworkPolicy == nil {
		t.Fatal("Step 3 - network policy is nil")
	}
	if !p.NetworkPolicy.DefaultDeny {
		t.Error("Step 3 - network policy should be default-deny")
	}
	if len(p.NetworkPolicy.AllowedHosts) > 0 {
		t.Errorf("Step 3 - expected no allowed hosts, got %v", p.NetworkPolicy.AllowedHosts)
	}

	// ── Step 4: Get draft details via handler ──────────────────────────
	getResult, err := handleProposalGetDraft(env, fmt.Sprintf(`{"id":"%s"}`, proposalID))
	if err != nil {
		t.Fatalf("Step 4 - get draft: %v", err)
	}
	if !strings.Contains(getResult, "time-of-day-greeter") {
		t.Errorf("Step 4 - get result missing skill name:\n%s", getResult)
	}
	if !strings.Contains(getResult, "Add time-of-day greeter skill") {
		t.Errorf("Step 4 - get result missing title:\n%s", getResult)
	}
	if !strings.Contains(getResult, "draft") {
		t.Errorf("Step 4 - get result should show draft status:\n%s", getResult)
	}

	// ── Step 5: Submit for Court review ────────────────────────────────
	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, proposalID))
	if err != nil {
		t.Fatalf("Step 5 - submit: %v", err)
	}
	if !strings.Contains(submitResult, "Proposal submitted") {
		t.Errorf("Step 5 - expected 'Proposal submitted', got: %s", submitResult)
	}
	if !strings.Contains(submitResult, proposalID) {
		t.Errorf("Step 5 - submit result should contain ID %s:\n%s", proposalID, submitResult)
	}

	// Verify status changed to submitted.
	p, err = env.ProposalStore.Get(proposalID)
	if err != nil {
		t.Fatalf("Step 5 - store.Get after submit: %v", err)
	}
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("Step 5 - status = %q, want 'submitted'", p.Status)
	}

	// ── Step 6: Verify audit trail has entries ─────────────────────────
	auditLog := kern.AuditLog()
	if auditLog.EntryCount() < 2 {
		t.Errorf("Step 6 - expected at least 2 audit entries (create + submit), got %d", auditLog.EntryCount())
	}

	// ── Step 7: Court review (deterministic all-approve) ───────────────
	personas := []*court.Persona{
		{Name: "CISO", Role: "security", SystemPrompt: "Review security", Models: []string{"test-model"}, Weight: 0.3},
		{Name: "SeniorCoder", Role: "code_quality", SystemPrompt: "Review code", Models: []string{"test-model"}, Weight: 0.3},
		{Name: "SecurityArchitect", Role: "architecture", SystemPrompt: "Review arch", Models: []string{"test-model"}, Weight: 0.2},
		{Name: "Tester", Role: "test_coverage", SystemPrompt: "Review tests", Models: []string{"test-model"}, Weight: 0.1},
		{Name: "UserAdvocate", Role: "usability", SystemPrompt: "Review UX", Models: []string{"test-model"}, Weight: 0.1},
	}

	reviewerFn := func(ctx context.Context, prop *proposal.Proposal, persona *court.Persona) (*proposal.Review, error) {
		return &proposal.Review{
			ID:        uuid.New().String(),
			Persona:   persona.Name,
			Model:     persona.Models[0],
			Round:     prop.Round + 1,
			Verdict:   proposal.VerdictApprove,
			RiskScore: 1.5,
			Evidence:  []string{"No network access", "No secrets needed", "Low privilege", "Minimal attack surface"},
			Comments:  "Low-risk greeter skill. Approved.",
			Timestamp: time.Now(),
		}, nil
	}

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, store, kern, personas, reviewerFn, logger)
	if err != nil {
		t.Fatalf("Step 7 - court engine: %v", err)
	}

	session, err := engine.Review(context.Background(), proposalID)
	if err != nil {
		t.Fatalf("Step 7 - court review: %v", err)
	}

	if session.State != court.SessionApproved {
		t.Errorf("Step 7 - session state = %q, want 'approved'", session.State)
	}
	if session.Verdict != "approved" {
		t.Errorf("Step 7 - verdict = %q, want 'approved'", session.Verdict)
	}

	// ── Step 8: Verify final proposal state ────────────────────────────
	p, err = env.ProposalStore.Get(proposalID)
	if err != nil {
		t.Fatalf("Step 8 - store.Get after review: %v", err)
	}
	if p.Status != proposal.StatusApproved {
		t.Errorf("Step 8 - final status = %q, want 'approved'", p.Status)
	}
	if len(p.Reviews) == 0 {
		t.Error("Step 8 - expected reviews to be attached to proposal")
	}

	// Verify all 5 personas reviewed.
	reviewPersonas := make(map[string]bool)
	for _, r := range p.Reviews {
		reviewPersonas[r.Persona] = true
	}
	for _, name := range []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"} {
		if !reviewPersonas[name] {
			t.Errorf("Step 8 - missing review from persona %q", name)
		}
	}

	// ── Step 9: Verify audit trail covers the full lifecycle ───────────
	// We expect at least: proposal.create, proposal.submit, proposal.approve
	if auditLog.EntryCount() < 3 {
		t.Errorf("Step 9 - expected at least 3 audit entries, got %d", auditLog.EntryCount())
	}
	t.Logf("Audit log has %d entries, chain head: %s", auditLog.EntryCount(), auditLog.LastHash()[:16])
}

// TestFirstSkillTutorialCLIPath exercises the CLI alternative described in the
// tutorial: using "skill add --non-interactive" flags instead of chat.
// This tests buildSkillAddResult which is the same path as
//
//	aegisclaw skill add "time-of-day greeter" --non-interactive --name ... --tool ...
func TestFirstSkillTutorialCLIPath(t *testing.T) {
	// Save and restore global flag state.
	origName := skillAddName
	origTitle := skillAddTitle
	origDesc := skillAddDescription
	origTools := skillAddTools
	origSens := skillAddSensitivity
	origExpo := skillAddExposure
	origPriv := skillAddPrivilege
	origHosts := skillAddHosts
	t.Cleanup(func() {
		skillAddName = origName
		skillAddTitle = origTitle
		skillAddDescription = origDesc
		skillAddTools = origTools
		skillAddSensitivity = origSens
		skillAddExposure = origExpo
		skillAddPrivilege = origPriv
		skillAddHosts = origHosts
	})

	// Set flags to match the tutorial CLI example.
	skillAddName = "time-of-day-greeter"
	skillAddTitle = ""
	skillAddDescription = ""
	skillAddTools = []string{"greet:Returns a locale-aware DST-respecting greeting appropriate for the current time of day"}
	skillAddSensitivity = 1
	skillAddExposure = 1
	skillAddPrivilege = 1
	skillAddHosts = nil

	result, err := buildSkillAddResult("time-of-day greeter")
	if err != nil {
		t.Fatalf("buildSkillAddResult: %v", err)
	}

	// Verify the result matches what we'd expect.
	if result.SkillName != "time-of-day-greeter" {
		t.Errorf("skill name = %q, want 'time-of-day-greeter'", result.SkillName)
	}
	if result.Title != "Add time-of-day greeter skill" {
		t.Errorf("title = %q, want 'Add time-of-day greeter skill'", result.Title)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(result.Tools))
	}
	if result.Tools[0].Name != "greet" {
		t.Errorf("tool name = %q, want 'greet'", result.Tools[0].Name)
	}
	if result.DataSensitivity != 1 {
		t.Errorf("data sensitivity = %d, want 1", result.DataSensitivity)
	}
	if result.NetworkExposure != 1 {
		t.Errorf("network exposure = %d, want 1", result.NetworkExposure)
	}
	if result.PrivilegeLevel != 1 {
		t.Errorf("privilege level = %d, want 1", result.PrivilegeLevel)
	}
	if result.NeedsNetwork {
		t.Error("expected no network access needed")
	}
	if result.Risk != "low" {
		t.Errorf("risk = %q, want 'low'", result.Risk)
	}

	// Verify the spec can be generated.
	spec, err := result.ToProposalJSON()
	if err != nil {
		t.Fatalf("ToProposalJSON: %v", err)
	}
	specStr := string(spec)
	if !strings.Contains(specStr, "time-of-day-greeter") {
		t.Errorf("spec missing skill name:\n%s", specStr)
	}
	if !strings.Contains(specStr, "greet") {
		t.Errorf("spec missing greet tool:\n%s", specStr)
	}
}

// TestFirstSkillTutorialSpecFields validates the detailed SkillSpec structure
// generated for the tutorial's greeter skill, ensuring all fields expected
// by the builder pipeline are present.
func TestFirstSkillTutorialSpecFields(t *testing.T) {
	kernel.ResetInstance()
	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()
	storeDir := t.TempDir()

	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("kernel init: %v", err)
	}
	store, err := proposal.NewStore(storeDir, logger)
	if err != nil {
		t.Fatalf("store init: %v", err)
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

	createArgs := `{
		"title": "Add time-of-day greeter skill",
		"description": "Greets the user based on time of day, respecting DST, in en-US.",
		"skill_name": "time-of-day-greeter",
		"tools": [{"name": "greet", "description": "Returns a time-appropriate greeting"}],
		"data_sensitivity": 1,
		"network_exposure": 1,
		"privilege_level": 1
	}`
	createResult, err := handleProposalCreateDraft(env, createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	proposalID := extractIDFromResult(t, createResult)

	p, err := env.ProposalStore.Get(proposalID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Parse the spec as a map to validate structure.
	var spec map[string]interface{}
	if err := json.Unmarshal(p.Spec, &spec); err != nil {
		t.Fatalf("spec unmarshal: %v", err)
	}

	// Required top-level fields.
	if spec["name"] != "time-of-day-greeter" {
		t.Errorf("spec.name = %v, want 'time-of-day-greeter'", spec["name"])
	}
	if _, ok := spec["description"]; !ok {
		t.Error("spec missing 'description'")
	}

	// Tools array must have exactly one "greet" tool.
	tools, ok := spec["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatal("spec.tools missing or empty")
	}
	tool0, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatal("spec.tools[0] is not an object")
	}
	if tool0["name"] != "greet" {
		t.Errorf("spec.tools[0].name = %v, want 'greet'", tool0["name"])
	}

	// Network policy in spec should be default-deny.
	netPolicy, ok := spec["network_policy"].(map[string]interface{})
	if !ok {
		t.Fatal("spec missing 'network_policy'")
	}
	if netPolicy["default_deny"] != true {
		t.Errorf("spec.network_policy.default_deny = %v, want true", netPolicy["default_deny"])
	}

	// Required personas (field name is "persona_requirements" in spec).
	personas, ok := spec["persona_requirements"].([]interface{})
	if !ok || len(personas) < 5 {
		t.Errorf("spec.persona_requirements should have 5 entries, got %v", personas)
	}
}
