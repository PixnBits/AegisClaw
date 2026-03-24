package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap/zaptest"
)

// testEnv creates a runtimeEnv with a real ProposalStore and Kernel backed
// by temp directories. It resets the kernel singleton on cleanup.
func testEnv(t *testing.T) *runtimeEnv {
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

	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})

	return &runtimeEnv{
		Logger:        logger,
		Kernel:        kern,
		ProposalStore: store,
	}
}

// stubDaemonClient returns a nil client — submit will skip the court.review
// call, which is fine for testing the proposal store flow.
func stubDaemonClient() *api.Client {
	return nil
}

// --- Proposal handler integration tests ---

func TestCreateDraftIntegration(t *testing.T) {
	env := testEnv(t)

	args := `{"title":"Hello World","description":"A greeting skill","skill_name":"hello-world","tools":[{"name":"greet","description":"Returns a greeting"}]}`
	result, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("handleProposalCreateDraft: %v", err)
	}

	if !strings.Contains(result, "Draft proposal created") {
		t.Errorf("expected 'Draft proposal created' in result, got: %s", result)
	}
	if !strings.Contains(result, "hello-world") {
		t.Errorf("expected skill name in result, got: %s", result)
	}

	// Extract the ID from the result.
	id := extractIDFromResult(t, result)
	if id == "" {
		t.Fatal("could not extract proposal ID from result")
	}

	// Verify it's a full UUID.
	if len(id) < 36 {
		t.Errorf("expected full UUID (36 chars), got %d chars: %s", len(id), id)
	}

	// Verify it exists in the store.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("store.Get(%s): %v", id, err)
	}
	if p.Title != "Hello World" {
		t.Errorf("expected title 'Hello World', got %q", p.Title)
	}
	if p.Status != proposal.StatusDraft {
		t.Errorf("expected status draft, got %s", p.Status)
	}
}

func TestCreateDraftMissingFields(t *testing.T) {
	env := testEnv(t)

	cases := []struct {
		name string
		args string
		want string
	}{
		{"missing title", `{"description":"desc","skill_name":"s","tools":[{"name":"t","description":"d"}]}`, "title is required"},
		{"missing skill_name", `{"title":"T","description":"desc","tools":[{"name":"t","description":"d"}]}`, "skill_name is required"},
		{"missing tools", `{"title":"T","description":"desc","skill_name":"s"}`, "at least one tool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := handleProposalCreateDraft(env, tc.args)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestListDraftsIntegration(t *testing.T) {
	env := testEnv(t)

	// Empty store.
	result, err := handleProposalListDrafts(env)
	if err != nil {
		t.Fatalf("handleProposalListDrafts (empty): %v", err)
	}
	if !strings.Contains(result, "No proposals found") {
		t.Errorf("expected 'No proposals found', got: %s", result)
	}

	// Create a draft.
	args := `{"title":"Test Skill","description":"A test","skill_name":"test","tools":[{"name":"run","description":"runs"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	// List should now have one proposal with full UUID.
	result, err = handleProposalListDrafts(env)
	if err != nil {
		t.Fatalf("handleProposalListDrafts: %v", err)
	}
	if !strings.Contains(result, id) {
		t.Errorf("expected full ID %q in list result, got: %s", id, result)
	}
	if !strings.Contains(result, "Test Skill") {
		t.Errorf("expected title in list result, got: %s", result)
	}
}

func TestListProposalsShowsFullID(t *testing.T) {
	env := testEnv(t)

	args := `{"title":"Full ID Test","description":"Testing full IDs","skill_name":"fid","tools":[{"name":"t","description":"d"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	// The list_proposals handler is the ExecuteTool callback.
	summaries, err := env.ProposalStore.List()
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}

	var lines []string
	for _, s := range summaries {
		lines = append(lines, fmt.Sprintf("  %s  %s  [%s]  %s", s.ID, s.Title, s.Status, s.Risk))
	}
	result := strings.Join(lines, "\n")

	if !strings.Contains(result, id) {
		t.Errorf("list_proposals should show full UUID %q, got: %s", id, result)
	}
}

func TestGetDraftIntegration(t *testing.T) {
	env := testEnv(t)

	// Create.
	args := `{"title":"Get Test","description":"Test get","skill_name":"getme","tools":[{"name":"do","description":"does"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	// Get by full ID.
	getResult, err := handleProposalGetDraft(env, fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("handleProposalGetDraft: %v", err)
	}
	if !strings.Contains(getResult, id) {
		t.Errorf("expected ID in get result, got: %s", getResult)
	}
	if !strings.Contains(getResult, "Get Test") {
		t.Errorf("expected title in get result, got: %s", getResult)
	}

	// Get by prefix (first 8 chars).
	prefix := id[:8]
	getResult2, err := handleProposalGetDraft(env, fmt.Sprintf(`{"id":"%s"}`, prefix))
	if err != nil {
		t.Fatalf("handleProposalGetDraft (prefix): %v", err)
	}
	if !strings.Contains(getResult2, id) {
		t.Errorf("prefix lookup should resolve to full ID %q, got: %s", id, getResult2)
	}
}

func TestUpdateDraftIntegration(t *testing.T) {
	env := testEnv(t)

	// Create.
	args := `{"title":"Update Test","description":"Before update","skill_name":"up","tools":[{"name":"do","description":"does"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	// Update title.
	updateArgs := fmt.Sprintf(`{"id":"%s","title":"Updated Title"}`, id)
	updateResult, err := handleProposalUpdateDraft(env, updateArgs)
	if err != nil {
		t.Fatalf("handleProposalUpdateDraft: %v", err)
	}
	if !strings.Contains(updateResult, "Updated Title") {
		t.Errorf("expected updated title in result, got: %s", updateResult)
	}

	// Verify in store.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if p.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", p.Title)
	}
}

func TestSubmitDraftIntegration(t *testing.T) {
	env := testEnv(t)

	// Create.
	args := `{"title":"Submit Test","description":"To be submitted","skill_name":"sub","tools":[{"name":"do","description":"does"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	// Submit.
	submitArgs := fmt.Sprintf(`{"id":"%s"}`, id)
	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), submitArgs)
	if err != nil {
		t.Fatalf("handleProposalSubmit: %v", err)
	}
	if !strings.Contains(submitResult, "Proposal submitted for court review") {
		t.Errorf("expected submission confirmation, got: %s", submitResult)
	}
	if !strings.Contains(submitResult, id) {
		t.Errorf("expected proposal ID in submit result, got: %s", submitResult)
	}

	// Verify status changed in store.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("store.Get after submit: %v", err)
	}
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("expected status submitted, got %s", p.Status)
	}
}

func TestSubmitByPrefixIntegration(t *testing.T) {
	env := testEnv(t)

	// Create.
	args := `{"title":"Prefix Submit","description":"Submit by prefix","skill_name":"pfx","tools":[{"name":"do","description":"does"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)
	prefix := id[:8]

	// Submit using prefix.
	submitArgs := fmt.Sprintf(`{"id":"%s"}`, prefix)
	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), submitArgs)
	if err != nil {
		t.Fatalf("handleProposalSubmit (prefix): %v", err)
	}
	if !strings.Contains(submitResult, "Proposal submitted") {
		t.Errorf("expected submission confirmation, got: %s", submitResult)
	}
	if !strings.Contains(submitResult, id) {
		t.Errorf("submit result should contain full ID %q, got: %s", id, submitResult)
	}

	// Verify.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("expected submitted, got %s", p.Status)
	}
}

func TestSubmitAlreadySubmittedIntegration(t *testing.T) {
	env := testEnv(t)

	args := `{"title":"Double Submit","description":"desc","skill_name":"ds","tools":[{"name":"t","description":"d"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	// First submit.
	submitArgs := fmt.Sprintf(`{"id":"%s"}`, id)
	_, err = handleProposalSubmit(env, stubDaemonClient(), context.Background(), submitArgs)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	// Second submit should not error but indicate already submitted.
	result, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), submitArgs)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if !strings.Contains(result, "already") {
		t.Errorf("expected 'already' in result for duplicate submit, got: %s", result)
	}
}

func TestSubmitWrongIDReturnsError(t *testing.T) {
	env := testEnv(t)

	// Try to submit a nonexistent ID.
	_, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), `{"id":"nonexistent-id-00000"}`)
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestProposalStatusIntegration(t *testing.T) {
	env := testEnv(t)

	args := `{"title":"Status Test","description":"Check status","skill_name":"st","tools":[{"name":"t","description":"d"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	result, err := handleProposalStatus(env, fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("handleProposalStatus: %v", err)
	}
	if !strings.Contains(result, "draft") {
		t.Errorf("expected 'draft' in status result, got: %s", result)
	}
}

// --- Full proposal journey integration test ---

func TestProposalJourneyCreateToSubmit(t *testing.T) {
	env := testEnv(t)

	// Step 1: Create draft.
	createArgs := `{
		"title": "Hello World Skill",
		"description": "A minimal greeting skill",
		"skill_name": "hello-world",
		"tools": [{"name": "greet", "description": "Returns a Hello World greeting message"}],
		"data_sensitivity": 1,
		"network_exposure": 1,
		"privilege_level": 1
	}`
	createResult, err := handleProposalCreateDraft(env, createArgs)
	if err != nil {
		t.Fatalf("Step 1 - Create: %v", err)
	}
	createdID := extractIDFromResult(t, createResult)
	t.Logf("Created proposal: %s", createdID)

	// Step 2: List drafts — the created ID must appear.
	listResult, err := handleProposalListDrafts(env)
	if err != nil {
		t.Fatalf("Step 2 - List: %v", err)
	}
	if !strings.Contains(listResult, createdID) {
		t.Fatalf("Step 2 - Created proposal %s not found in list:\n%s", createdID, listResult)
	}

	// Step 3: Get the draft and verify fields.
	getResult, err := handleProposalGetDraft(env, fmt.Sprintf(`{"id":"%s"}`, createdID))
	if err != nil {
		t.Fatalf("Step 3 - Get: %v", err)
	}
	if !strings.Contains(getResult, "Hello World Skill") {
		t.Errorf("Step 3 - Expected title in get result:\n%s", getResult)
	}
	if !strings.Contains(getResult, "hello-world") {
		t.Errorf("Step 3 - Expected skill name in get result:\n%s", getResult)
	}

	// Step 4: Submit the proposal using the EXACT ID from create.
	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, createdID))
	if err != nil {
		t.Fatalf("Step 4 - Submit: %v", err)
	}
	if !strings.Contains(submitResult, "Proposal submitted") {
		t.Errorf("Step 4 - Expected submission confirmation:\n%s", submitResult)
	}
	if !strings.Contains(submitResult, createdID) {
		t.Errorf("Step 4 - Submit result should contain proposal ID %s:\n%s", createdID, submitResult)
	}

	// Step 5: Verify the proposal status in the store.
	p, err := env.ProposalStore.Get(createdID)
	if err != nil {
		t.Fatalf("Step 5 - Store.Get: %v", err)
	}
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("Step 5 - Expected status 'submitted', got %q", p.Status)
	}
	if p.Title != "Hello World Skill" {
		t.Errorf("Step 5 - Expected title 'Hello World Skill', got %q", p.Title)
	}

	// Step 6: The status handler should report 'submitted'.
	statusResult, err := handleProposalStatus(env, fmt.Sprintf(`{"id":"%s"}`, createdID))
	if err != nil {
		t.Fatalf("Step 6 - Status: %v", err)
	}
	if !strings.Contains(statusResult, "submitted") {
		t.Errorf("Step 6 - Expected 'submitted' in status result:\n%s", statusResult)
	}

	// Step 7: List should now show the proposal as submitted.
	listResult2, err := handleProposalListDrafts(env)
	if err != nil {
		t.Fatalf("Step 7 - List after submit: %v", err)
	}
	if !strings.Contains(listResult2, "submitted") {
		t.Errorf("Step 7 - Expected 'submitted' status in list:\n%s", listResult2)
	}
}

func TestProposalJourneyMultipleProposals(t *testing.T) {
	env := testEnv(t)

	// Create two proposals and verify submitting one doesn't affect the other.
	args1 := `{"title":"Skill Alpha","description":"First skill","skill_name":"alpha","tools":[{"name":"a","description":"does A"}]}`
	args2 := `{"title":"Skill Beta","description":"Second skill","skill_name":"beta","tools":[{"name":"b","description":"does B"}]}`

	result1, err := handleProposalCreateDraft(env, args1)
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	id1 := extractIDFromResult(t, result1)

	result2, err := handleProposalCreateDraft(env, args2)
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}
	id2 := extractIDFromResult(t, result2)

	if id1 == id2 {
		t.Fatal("two proposals should have different IDs")
	}

	// Submit only the first one.
	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id1))
	if err != nil {
		t.Fatalf("submit alpha: %v", err)
	}
	if !strings.Contains(submitResult, id1) {
		t.Errorf("submit result should reference alpha's ID %s, got: %s", id1, submitResult)
	}

	// Alpha should be submitted.
	p1, err := env.ProposalStore.Get(id1)
	if err != nil {
		t.Fatalf("get alpha: %v", err)
	}
	if p1.Status != proposal.StatusSubmitted {
		t.Errorf("alpha should be submitted, got %s", p1.Status)
	}

	// Beta should still be draft.
	p2, err := env.ProposalStore.Get(id2)
	if err != nil {
		t.Fatalf("get beta: %v", err)
	}
	if p2.Status != proposal.StatusDraft {
		t.Errorf("beta should still be draft, got %s", p2.Status)
	}
}

func TestProposalJourneySubmitByWrongPrefixFails(t *testing.T) {
	env := testEnv(t)

	// Create a proposal.
	args := `{"title":"Correct One","description":"desc","skill_name":"c","tools":[{"name":"t","description":"d"}]}`
	createResult, err := handleProposalCreateDraft(env, args)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := extractIDFromResult(t, createResult)

	// Try submitting with a made-up prefix that doesn't match.
	_, err = handleProposalSubmit(env, stubDaemonClient(), context.Background(), `{"id":"zzzzaaaa"}`)
	if err == nil {
		t.Fatal("expected error for non-matching prefix")
	}

	// Original should still be draft.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Status != proposal.StatusDraft {
		t.Errorf("proposal should still be draft, got %s", p.Status)
	}
}

func TestResolveProposalIDIntegration(t *testing.T) {
	env := testEnv(t)

	// Create a proposal.
	p, err := proposal.NewProposal("Resolve Test", "desc", proposal.CategoryNewSkill, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.ProposalStore.Create(p); err != nil {
		t.Fatal(err)
	}

	// Resolve by full ID.
	got, err := resolveProposalID(env, p.ID)
	if err != nil {
		t.Fatalf("resolve full: %v", err)
	}
	if got != p.ID {
		t.Errorf("expected %s, got %s", p.ID, got)
	}

	// Resolve by 8-char prefix.
	got, err = resolveProposalID(env, p.ID[:8])
	if err != nil {
		t.Fatalf("resolve prefix: %v", err)
	}
	if got != p.ID {
		t.Errorf("expected %s, got %s", p.ID, got)
	}

	// Nonexistent prefix.
	_, err = resolveProposalID(env, "zzzzzzzz")
	if err == nil {
		t.Error("expected error for nonexistent prefix")
	}
}

// --- System prompt integration test ---

func TestBuildSystemPromptContainsProposalTools(t *testing.T) {
	prompt := buildSystemPrompt(context.Background(), stubDaemonClient())

	requiredFragments := []string{
		"proposal.create_draft",
		"proposal.update_draft",
		"proposal.get_draft",
		"proposal.list_drafts",
		"proposal.submit",
		"proposal.status",
		"CRITICAL RULES",
		"full proposal ID",
	}
	for _, frag := range requiredFragments {
		if !strings.Contains(prompt, frag) {
			t.Errorf("system prompt missing %q", frag)
		}
	}
}

// --- Parse tool calls integration test ---

func TestParseToolCallsJSON(t *testing.T) {
	content := "Sure, let me create that.\n```tool-call\n{\"skill\":\"proposal\",\"tool\":\"create_draft\",\"args\":{\"title\":\"Test\"}}\n```\n"
	calls := parseToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "proposal.create_draft" {
		t.Errorf("expected proposal.create_draft, got %s", calls[0].Name)
	}

	// Parse the args.
	var args struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(calls[0].Args), &args); err != nil {
		t.Fatalf("unmarshal args: %v", err)
	}
	if args.Title != "Test" {
		t.Errorf("expected title 'Test', got %q", args.Title)
	}
}

func TestParseToolCallsJSONBlock(t *testing.T) {
	// Some LLMs use ```json instead of ```tool-call.
	content := "```json\n{\"skill\":\"proposal\",\"tool\":\"submit\",\"args\":{\"id\":\"abc-123\"}}\n```"
	calls := parseToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "proposal.submit" {
		t.Errorf("expected proposal.submit, got %s", calls[0].Name)
	}
}

func TestCleanToolCallContent(t *testing.T) {
	content := "Let me submit that for you.\n```tool-call\n{\"skill\":\"proposal\",\"tool\":\"submit\",\"args\":{}}\n```\nDone!"
	cleaned := cleanToolCallContent(content)
	if strings.Contains(cleaned, "tool-call") {
		t.Error("cleaned content should not contain tool-call block")
	}
	if !strings.Contains(cleaned, "Let me submit that") {
		t.Error("cleaned content should preserve surrounding text")
	}
}

func TestParseToolCallsLimitsToOne(t *testing.T) {
	// If the LLM emits create_draft + submit in one response, only the first
	// should be returned so the second can use the ID from the first's result.
	content := "```tool-call\n{\"skill\":\"proposal\",\"tool\":\"create_draft\",\"args\":{\"title\":\"A\",\"skill_name\":\"a\",\"tools\":[{\"name\":\"t\",\"description\":\"d\"}]}}\n```\nNow submitting:\n```tool-call\n{\"skill\":\"proposal\",\"tool\":\"submit\",\"args\":{\"id\":\"stale-id\"}}\n```"
	calls := parseToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "proposal.create_draft" {
		t.Errorf("expected proposal.create_draft (the first call), got %s", calls[0].Name)
	}
}

// --- Helpers ---

// extractIDFromResult parses the proposal ID from a handler result string.
// Looks for the "ID: <uuid>" pattern in output.
func extractIDFromResult(t *testing.T, result string) string {
	t.Helper()
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID:") {
			id := strings.TrimSpace(strings.TrimPrefix(line, "ID:"))
			return id
		}
	}
	t.Fatalf("no 'ID:' line found in result:\n%s", result)
	return ""
}
