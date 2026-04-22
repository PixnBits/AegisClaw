package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/builder/securitygate"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/testutil"
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
	createResult, err := handleProposalCreateDraft(env, context.Background(), createArgs)
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
	getResult, err := handleProposalGetDraft(env, context.Background(), fmt.Sprintf(`{"id":"%s"}`, proposalID))
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
	engine, err := court.NewEngine(cfg, store, kern, personas, reviewerFn, logger, auditDir)
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
	createResult, err := handleProposalCreateDraft(env, context.Background(), createArgs)
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

// greeterSkillFiles returns the minimal source files for the time-of-day greeter
// skill. These are used by both the security-gate and artifact tests so the
// code is defined once and matches what the tutorial describes.
func greeterSkillFiles() map[string]string {
	return map[string]string{
		"main.go": `package main

import (
	"encoding/json"
	"os"
	"time"
)

// Request mirrors the vsock message envelope sent by the daemon.
type Request struct {
	ID      string          ` + "`" + `json:"id"` + "`" + `
	Type    string          ` + "`" + `json:"type"` + "`" + `
	Payload json.RawMessage ` + "`" + `json:"payload"` + "`" + `
}

// Response is the vsock reply envelope.
type Response struct {
	ID      string ` + "`" + `json:"id"` + "`" + `
	Success bool   ` + "`" + `json:"success"` + "`" + `
	Error   string ` + "`" + `json:"error,omitempty"` + "`" + `
	Data    any    ` + "`" + `json:"data,omitempty"` + "`" + `
}

// greet returns a locale-aware greeting for the current Eastern Time hour.
// America/New_York observes US DST automatically via the IANA tz database.
func greet() string {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.UTC
	}
	hour := time.Now().In(loc).Hour()
	switch {
	case hour >= 5 && hour < 12:
		return "Good morning!"
	case hour >= 12 && hour < 17:
		return "Good afternoon!"
	case hour >= 17 && hour < 21:
		return "Good evening!"
	default:
		return "Good night!"
	}
}

func main() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	var req Request
	if err := dec.Decode(&req); err != nil {
		enc.Encode(Response{Error: "decode error"}) //nolint:errcheck
		return
	}

	switch req.Type {
	case "greet":
		enc.Encode(Response{ //nolint:errcheck
			ID:      req.ID,
			Success: true,
			Data:    map[string]string{"greeting": greet()},
		})
	default:
		enc.Encode(Response{ID: req.ID, Error: "unknown tool: " + req.Type}) //nolint:errcheck
	}
}
`,
		"go.mod": `module github.com/PixnBits/AegisClaw/skills/time-of-day-greeter

go 1.25
`,
	}
}

// greeterSkillSpec returns the SkillSpec for the time-of-day greeter.
func greeterSkillSpec() *builder.SkillSpec {
	return &builder.SkillSpec{
		Name:        "time-of-day-greeter",
		Description: "A skill that greets the user with a time-appropriate message (good morning, good afternoon, good evening, good night) based on the current local time, respecting DST, in en-US locale.",
		Tools: []builder.ToolSpec{
			{
				Name:         "greet",
				Description:  "Returns a locale-aware, DST-respecting greeting appropriate for the current time of day",
				InputSchema:  `{"type":"object","properties":{}}`,
				OutputSchema: `{"type":"object","properties":{"greeting":{"type":"string"}}}`,
			},
		},
		NetworkPolicy:       builder.SkillNetworkPolicy{DefaultDeny: true},
		Language:            "go",
		EntryPoint:          "main.go",
		PersonaRequirements: []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"},
	}
}

// TestFirstSkillTutorialSecurityGates exercises Tutorial Step 6 — the mandatory
// security gates (D8) that run before any skill artifact can be deployed.
//
// The four gates tested are:
//   - SAST: static analysis for anti-patterns (injection, weak crypto, hardcoded creds)
//   - SCA: dependency scanning for banned/vulnerable packages
//   - Secrets scanning: detects accidentally embedded keys/tokens
//   - Policy-as-code: enforces isolation invariants (no host FS, no privileged ops)
//
// The greeter skill uses only stdlib time/encoding/os, so it must pass all gates.
func TestFirstSkillTutorialSecurityGates(t *testing.T) {
	files := greeterSkillFiles()

	// ── Step 6a: Run the full default security gate pipeline ───────────
	sgPipeline := securitygate.DefaultPipeline(securitygate.DefaultPolicies())
	req := &securitygate.EvalRequest{
		ProposalID: "tutorial-sg-test-001",
		SkillName:  "time-of-day-greeter",
		Files:      files,
	}

	sgResult, err := sgPipeline.Evaluate(req)
	if err != nil {
		t.Fatalf("Step 6a - security gate evaluation: %v", err)
	}

	// ── Step 6b: All four gates must pass ──────────────────────────────
	if !sgResult.Passed {
		t.Errorf("Step 6b - security gate pipeline failed (blocking: %d, total: %d)",
			sgResult.BlockingFindings, sgResult.TotalFindings)
		for _, gr := range sgResult.Gates {
			for _, f := range gr.Findings {
				if f.Severity == securitygate.SeverityError || f.Severity == securitygate.SeverityCritical {
					t.Logf("  BLOCKING [%s] %s:%d %s (%s)", f.Rule, f.File, f.Line, f.Message, f.Severity)
				}
			}
		}
	}

	gatesSeen := make(map[securitygate.GateType]bool)
	for _, gr := range sgResult.Gates {
		gatesSeen[gr.Gate] = true
	}
	for _, g := range []securitygate.GateType{
		securitygate.GateSAST,
		securitygate.GateSCA,
		securitygate.GateSecretsScanning,
		securitygate.GatePolicy,
	} {
		if !gatesSeen[g] {
			t.Errorf("Step 6b - gate %q did not run", g)
		}
	}

	// ── Step 6c: No blocking findings ─────────────────────────────────
	if sgResult.BlockingFindings != 0 {
		t.Errorf("Step 6c - expected 0 blocking findings, got %d", sgResult.BlockingFindings)
	}

	// ── Step 6d: SAST gate produces no error/critical findings ─────────
	for _, gr := range sgResult.Gates {
		if gr.Gate != securitygate.GateSAST {
			continue
		}
		for _, f := range gr.Findings {
			if f.Severity == securitygate.SeverityError || f.Severity == securitygate.SeverityCritical {
				t.Errorf("Step 6d - SAST blocking finding: [%s] %s", f.Rule, f.Message)
			}
		}
	}

	// ── Step 6e: SCA gate passes (no banned deps in go.mod) ───────────
	for _, gr := range sgResult.Gates {
		if gr.Gate == securitygate.GateSCA && !gr.Passed {
			t.Errorf("Step 6e - SCA gate failed: %v", gr.Findings)
		}
	}

	// ── Step 6f: Secrets gate passes ──────────────────────────────────
	for _, gr := range sgResult.Gates {
		if gr.Gate == securitygate.GateSecretsScanning && !gr.Passed {
			t.Errorf("Step 6f - secrets gate failed: %v", gr.Findings)
		}
	}

	// ── Step 6g: Policy gate — no blocking violations ──────────────────
	for _, gr := range sgResult.Gates {
		if gr.Gate != securitygate.GatePolicy {
			continue
		}
		for _, f := range gr.Findings {
			if f.Severity == securitygate.SeverityError || f.Severity == securitygate.SeverityCritical {
				t.Errorf("Step 6g - policy blocking violation: [%s] %s", f.Rule, f.Message)
			}
		}
	}

	t.Logf("Security gates: passed=%v  blocking=%d  total=%d  gates=%d",
		sgResult.Passed, sgResult.BlockingFindings, sgResult.TotalFindings, len(sgResult.Gates))
}

// TestFirstSkillTutorialArtifactSigning exercises Tutorial Step 7 — artifact
// packaging, cryptographic signing, and verification.
//
// After the builder pipeline completes, the skill binary is:
//  1. Signed with the kernel's Ed25519 key
//  2. Stored in the artifact store alongside a manifest and checksum file
//  3. Verifiable — the manifest signature and binary hash must round-trip
func TestFirstSkillTutorialArtifactSigning(t *testing.T) {
	kernel.ResetInstance()
	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()
	artifactDir := filepath.Join(t.TempDir(), "artifacts")

	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("kernel init: %v", err)
	}
	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})

	store, err := builder.NewArtifactStore(artifactDir, kern, logger)
	if err != nil {
		t.Fatalf("NewArtifactStore: %v", err)
	}

	spec := greeterSkillSpec()

	// Simulate the binary produced by the builder pipeline.
	// In production this is the compiled Go binary; here we use a stub.
	simulatedBinary := []byte("ELF-binary-stub-time-of-day-greeter-v1.0.0")

	// File hashes as the pipeline's computeFileHashes would produce.
	fileHashes := map[string]string{
		"main.go": "abc123deadbeef",
		"go.mod":  "fedcba987654",
	}

	proposalID := "tutorial-artifact-" + uuid.New().String()[:8]

	// ── Step 7a: Package and sign the artifact ─────────────────────────
	manifest, err := store.PackageArtifact(
		"time-of-day-greeter",
		proposalID,
		"v1.0.0",
		"abc1234def5678",
		simulatedBinary,
		fileHashes,
		spec,
	)
	if err != nil {
		t.Fatalf("Step 7a - PackageArtifact: %v", err)
	}

	// ── Step 7b: Manifest fields match the proposal and spec ──────────
	if manifest.SkillID != "time-of-day-greeter" {
		t.Errorf("Step 7b - SkillID = %q, want 'time-of-day-greeter'", manifest.SkillID)
	}
	if manifest.ProposalID != proposalID {
		t.Errorf("Step 7b - ProposalID = %q, want %q", manifest.ProposalID, proposalID)
	}
	if manifest.Version != "v1.0.0" {
		t.Errorf("Step 7b - Version = %q, want 'v1.0.0'", manifest.Version)
	}
	if manifest.Language != "go" {
		t.Errorf("Step 7b - Language = %q, want 'go'", manifest.Language)
	}
	if manifest.EntryPoint != "main.go" {
		t.Errorf("Step 7b - EntryPoint = %q, want 'main.go'", manifest.EntryPoint)
	}

	// ── Step 7c: Binary is signed with the kernel key ─────────────────
	if manifest.Signature == "" {
		t.Error("Step 7c - Signature is empty")
	}
	if manifest.KernelPubKey == "" {
		t.Error("Step 7c - KernelPubKey is empty")
	}
	// Verify the stored public key matches the actual kernel key.
	pubKeyHex := hex.EncodeToString(kern.PublicKey())
	if manifest.KernelPubKey != pubKeyHex {
		t.Errorf("Step 7c - KernelPubKey mismatch: manifest=%s kernel=%s",
			manifest.KernelPubKey[:16]+"...", pubKeyHex[:16]+"...")
	}

	// ── Step 7d: Sandbox manifest enforces isolation ───────────────────
	if !manifest.Sandbox.ReadOnlyRoot {
		t.Error("Step 7d - sandbox.ReadOnlyRoot must be true (immutable rootfs)")
	}
	if manifest.Sandbox.VCPUs < 1 {
		t.Errorf("Step 7d - sandbox.VCPUs = %d, want >= 1", manifest.Sandbox.VCPUs)
	}
	if manifest.Sandbox.MemoryMB < 1 {
		t.Errorf("Step 7d - sandbox.MemoryMB = %d, want >= 1", manifest.Sandbox.MemoryMB)
	}
	// Greeter needs no network — policy string should say "deny".
	if !strings.Contains(manifest.Sandbox.NetworkPolicy, "deny") {
		t.Errorf("Step 7d - sandbox.NetworkPolicy should contain 'deny', got %q",
			manifest.Sandbox.NetworkPolicy)
	}

	// ── Step 7e: Validate the manifest round-trips cleanly ────────────
	if err := manifest.Validate(); err != nil {
		t.Errorf("Step 7e - manifest.Validate: %v", err)
	}

	// ── Step 7f: VerifyArtifact confirms hash and signature integrity ──
	verified, err := store.VerifyArtifact("time-of-day-greeter")
	if err != nil {
		t.Fatalf("Step 7f - VerifyArtifact: %v", err)
	}
	if verified.BinaryHash != manifest.BinaryHash {
		t.Errorf("Step 7f - verified BinaryHash mismatch: got %s, want %s",
			verified.BinaryHash[:16]+"...", manifest.BinaryHash[:16]+"...")
	}
	if verified.Signature != manifest.Signature {
		t.Errorf("Step 7f - verified Signature mismatch")
	}

	// ── Step 7g: Listing confirms the artifact is stored ──────────────
	ids, err := store.ListArtifacts()
	if err != nil {
		t.Fatalf("Step 7g - ListArtifacts: %v", err)
	}
	found := false
	for _, id := range ids {
		if id == "time-of-day-greeter" {
			found = true
		}
	}
	if !found {
		t.Errorf("Step 7g - 'time-of-day-greeter' not found in artifact list: %v", ids)
	}

	// ── Step 7h: Audit log captured the packaging event ───────────────
	auditLog := kern.AuditLog()
	if auditLog.EntryCount() < 1 {
		t.Error("Step 7h - expected at least 1 audit entry from artifact packaging")
	}

	t.Logf("Artifact signed: skill=%s  version=%s  size=%d  sig=%s...",
		manifest.SkillID, manifest.Version, manifest.BinarySize, manifest.Signature[:16])
}

