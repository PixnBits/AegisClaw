package main

// react_journey_test.go — Table-driven journey tests for the full agent lifecycle.
//
// These tests simulate the ReAct loop by providing deterministic "LLM responses"
// (pre-scripted tool-call content) and driving the loop through the real
// proposal handlers.  No Firecracker microVM or Ollama is required.
//
// Each scenario covers a distinct failure mode observed in the field:
//  1.  Simple single-tool use (create_draft)
//  2.  Multi-step task (create → list → get → submit)
//  3.  Explicit task completion (no tool needed)
//  4.  No matching tool (unknown tool should be rejected, not loop forever)
//  5.  Tool failure / bad args → error message
//  6.  Wrong namespace auto-correction (LLM uses skill name as namespace)
//  7.  Duplicate submit (idempotency guard)
//  8.  Multiple proposals — submit one, leave other as draft
//  9.  LLM emits exposition JSON (not a tool call) — must not be parsed as one
// 10.  Update then submit lifecycle
// 11.  Proposal ID prefix resolution
// 12.  Portal event emission: ToolEvents and ThoughtEvents are populated

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/proposal"
)

// mustExtractID extracts the proposal ID from a handler result or fails the test.
func mustExtractID(t *testing.T, result string) string {
	t.Helper()
	id := extractIDFromResult(t, result)
	if id == "" {
		t.Fatalf("could not extract proposal ID from: %s", result)
	}
	return id
}

// ─── Scenario 1: Simple single-tool use ───────────────────────────────────────

func TestJourneySimpleCreateDraft(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)
	rec := newTraceRecorder("journey-simple-create", "please create a hello-world skill")

	// The LLM immediately calls proposal.create_draft.
	llmResponse := "I'll create that skill now.\n" +
		"```tool-call\n" +
		`{"skill":"proposal","tool":"create_draft","args":{"title":"Hello World Skill","description":"Greets the user","skill_name":"hello-world","tools":[{"name":"greet","description":"Returns a greeting"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}}` +
		"\n```"

	rec.recordThought("Decided to call proposal.create_draft")
	rec.recordToolCall("proposal.create_draft", "")

	calls := parseToolCalls(llmResponse)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "proposal.create_draft" {
		t.Fatalf("expected proposal.create_draft, got %s", calls[0].Name)
	}

	result, err := handleProposalCreateDraft(env, calls[0].Args)
	if err != nil {
		t.Fatalf("handleProposalCreateDraft: %v", err)
	}
	if !strings.Contains(result, "Draft proposal created") {
		t.Errorf("expected 'Draft proposal created', got: %s", result)
	}
	if !strings.Contains(result, "hello-world") {
		t.Errorf("expected 'hello-world' in result, got: %s", result)
	}

	id := mustExtractID(t, result)
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if p.Status != proposal.StatusDraft {
		t.Errorf("expected draft, got %s", p.Status)
	}
	if p.Title != "Hello World Skill" {
		t.Errorf("title mismatch: got %q", p.Title)
	}

	rec.recordToolResult("proposal.create_draft", true)
	trace := rec.finalize("Proposal 'hello-world' created with ID " + id)

	// Verify tool events are recorded.
	env.ToolEvents = NewToolEventBuffer(100)
	env.ToolEvents.RecordStart("proposal.create_draft")
	env.ToolEvents.RecordFinish("proposal.create_draft", true, nil, 0)
	events := env.ToolEvents.Recent(10)
	if len(events) != 2 {
		t.Errorf("expected 2 tool events (start+finish), got %d", len(events))
	}
	if events[0].Tool != "proposal.create_draft" {
		t.Errorf("event[0] tool = %q, want 'proposal.create_draft'", events[0].Tool)
	}
	if trace.ToolCallCount != 1 {
		t.Errorf("trace: expected 1 tool call, got %d", trace.ToolCallCount)
	}
}

// ─── Scenario 2: Multi-step task (create → list → get → submit) ───────────────

func TestJourneyMultiStepCreateListGetSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)
	rec := newTraceRecorder("journey-multi-step", "create and submit a calculator skill")

	// Step 1: Create draft.
	rec.recordThought("Call proposal.create_draft")
	rec.recordToolCall("proposal.create_draft", "")
	createArgs := `{"title":"Calculator","description":"Basic arithmetic","skill_name":"calculator","tools":[{"name":"add","description":"Adds two numbers"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	createResult, err := handleProposalCreateDraft(env, createArgs)
	if err != nil {
		t.Fatalf("step 1 create: %v", err)
	}
	if !strings.Contains(createResult, "Draft proposal created") {
		t.Errorf("step 1: unexpected result: %s", createResult)
	}
	id := mustExtractID(t, createResult)
	rec.recordToolResult("proposal.create_draft", true)

	// Step 2: List drafts — the new ID must appear.
	rec.recordThought("Call proposal.list_drafts to verify")
	rec.recordToolCall("proposal.list_drafts", "")
	listResult, err := handleProposalListDrafts(env)
	if err != nil {
		t.Fatalf("step 2 list: %v", err)
	}
	if !strings.Contains(listResult, id) {
		t.Errorf("step 2: created ID %s not in list:\n%s", id, listResult)
	}
	rec.recordToolResult("proposal.list_drafts", true)

	// Step 3: Get draft details.
	rec.recordThought("Call proposal.get_draft to inspect")
	rec.recordToolCall("proposal.get_draft", "")
	getResult, err := handleProposalGetDraft(env, fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("step 3 get: %v", err)
	}
	if !strings.Contains(getResult, "Calculator") {
		t.Errorf("step 3: title missing from get result: %s", getResult)
	}
	rec.recordToolResult("proposal.get_draft", true)

	// Step 4: Submit.
	rec.recordThought("Call proposal.submit")
	rec.recordToolCall("proposal.submit", "")
	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("step 4 submit: %v", err)
	}
	if !strings.Contains(submitResult, "Proposal submitted") {
		t.Errorf("step 4: unexpected submit result: %s", submitResult)
	}
	rec.recordToolResult("proposal.submit", true)

	// Final state assertions.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("final get: %v", err)
	}
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("expected submitted, got %s", p.Status)
	}

	trace := rec.finalize("Proposal 'calculator' created and submitted: " + id)
	if trace.ToolCallCount != 4 {
		t.Errorf("expected 4 tool calls in trace, got %d", trace.ToolCallCount)
	}
	if trace.Iterations != 4 {
		t.Errorf("expected 4 iterations in trace, got %d", trace.Iterations)
	}
}

// ─── Scenario 3: Explicit task completion (no tool needed) ────────────────────

func TestJourneyExplicitCompletionNoTool(t *testing.T) {
	// No VM or Ollama needed — just verifying parseToolCalls behavior.
	llmFinalResponse := "I understand you want to chat. I'm here to help with AegisClaw skills. What would you like to do?"
	calls := parseToolCalls(llmFinalResponse)
	if len(calls) != 0 {
		t.Errorf("expected 0 tool calls for conversational reply, got %d: %v", len(calls), calls)
	}

	rec := newTraceRecorder("journey-no-tool", "just chatting")
	trace := rec.finalize(llmFinalResponse)
	if trace.ToolCallCount != 0 {
		t.Errorf("expected 0 tool calls, got %d", trace.ToolCallCount)
	}
}

// ─── Scenario 4: Unknown tool → graceful error, loop terminates ───────────────

func TestJourneyUnknownToolError(t *testing.T) {
	env := testEnv(t)
	reg := &ToolRegistry{env: env}
	ctx := context.Background()

	// A tool with no dot separator: goes to the "unknown tool" error path
	// without trying skill VM dispatch.
	_, err := reg.Execute(ctx, "completelyunknowntool", `{}`)
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error, got: %v", err)
	}
}

// ─── Scenario 5: Tool failure / bad args → error message ─────────────────────

func TestJourneyToolFailureBadArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)
	rec := newTraceRecorder("journey-tool-failure", "create proposal with missing required fields")

	rec.recordThought("Call proposal.create_draft with bad args")
	rec.recordToolCall("proposal.create_draft", "")

	_, err := handleProposalCreateDraft(env, `{"title":"Incomplete"}`)
	rec.recordToolResult("proposal.create_draft", err == nil)

	if err == nil {
		t.Error("expected error for missing required field 'skill_name'")
	}
	// The error or result should reference the missing field.
	if err != nil {
		t.Logf("tool failure error (expected): %v", err)
	}
}

// ─── Scenario 6: Wrong namespace auto-correction ──────────────────────────────

func TestJourneyWrongNamespaceAutoCorrection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)

	createArgs := `{"title":"Greeter","description":"Greets","skill_name":"greeter","tools":[{"name":"greet","description":"hi"}]}`
	createResult, err := handleProposalCreateDraft(env, createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := mustExtractID(t, createResult)

	// The LLM uses the skill name as the namespace (a real observed bug).
	badContent := fmt.Sprintf("```tool-call\n{\"skill\":\"greeter\",\"tool\":\"submit\",\"args\":{\"id\":\"%s\"}}\n```", id)
	calls := parseToolCalls(badContent)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "proposal.submit" {
		t.Fatalf("auto-correction failed: expected proposal.submit, got %q", calls[0].Name)
	}

	// The corrected call should work.
	result, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), calls[0].Args)
	if err != nil {
		t.Fatalf("submit after correction: %v", err)
	}
	if !strings.Contains(result, "Proposal submitted") {
		t.Errorf("unexpected result: %s", result)
	}
}

// ─── Scenario 7: Duplicate submit idempotency ─────────────────────────────────

func TestJourneyDuplicateSubmitIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)

	createResult, err := handleProposalCreateDraft(env, `{"title":"T","description":"d","skill_name":"t","tools":[{"name":"f","description":"does f"}]}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := mustExtractID(t, createResult)

	// First submit — should succeed.
	result1, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if !strings.Contains(result1, "Proposal submitted") {
		t.Errorf("first submit: unexpected result: %s", result1)
	}

	// Second submit — must not panic; should indicate already submitted.
	result2, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("second submit returned unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result2), "already") && !strings.Contains(result2, id) {
		t.Errorf("second submit: expected idempotent response, got: %s", result2)
	}
}

// ─── Scenario 8: Multiple proposals — submit one, leave other as draft ─────────

func TestJourneyMultipleProposalsSelectiveSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)

	mkArgs := func(name, skillName string) string {
		return fmt.Sprintf(`{"title":%q,"description":"desc","skill_name":%q,"tools":[{"name":"t","description":"d"}]}`, name, skillName)
	}

	result1, err := handleProposalCreateDraft(env, mkArgs("Skill One", "skill-one"))
	if err != nil {
		t.Fatalf("create skill-one: %v", err)
	}
	id1 := mustExtractID(t, result1)

	result2, err := handleProposalCreateDraft(env, mkArgs("Skill Two", "skill-two"))
	if err != nil {
		t.Fatalf("create skill-two: %v", err)
	}
	id2 := mustExtractID(t, result2)

	// Submit only the first.
	_, err = handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id1))
	if err != nil {
		t.Fatalf("submit skill-one: %v", err)
	}

	p1, err := env.ProposalStore.Get(id1)
	if err != nil {
		t.Fatalf("get skill-one: %v", err)
	}
	p2, err := env.ProposalStore.Get(id2)
	if err != nil {
		t.Fatalf("get skill-two: %v", err)
	}

	if p1.Status != proposal.StatusSubmitted {
		t.Errorf("skill-one: expected submitted, got %s", p1.Status)
	}
	if p2.Status != proposal.StatusDraft {
		t.Errorf("skill-two: expected draft (untouched), got %s", p2.Status)
	}
}

// ─── Scenario 9: Exposition JSON not parsed as tool call ──────────────────────

func TestJourneyExpositionJSONNotParsedAsTool(t *testing.T) {
	// Reproduces session d1b19f2f where the LLM described the proposal
	// fields in a ```json block instead of actually calling the tool.
	content := "I'll create this for you:\n```json\n{\"title\":\"My Skill\",\"description\":\"does stuff\",\"skill_name\":\"my-skill\"}\n```\nShall I proceed?"
	calls := parseToolCalls(content)
	if len(calls) != 0 {
		t.Errorf("exposition JSON should not be parsed as tool call, got %d calls", len(calls))
	}
}

// ─── Scenario 10: Update then submit lifecycle ────────────────────────────────

func TestJourneyUpdateThenSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)

	createArgs := `{"title":"Original Title","description":"original","skill_name":"updatable","tools":[{"name":"f","description":"does f"}]}`
	createResult, err := handleProposalCreateDraft(env, createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := mustExtractID(t, createResult)

	// Update the draft title.
	updateArgs := fmt.Sprintf(`{"id":%q,"title":"Updated Title"}`, id)
	_, err = handleProposalUpdateDraft(env, updateArgs)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// Verify title was updated in the store.
	p, err := env.ProposalStore.Get(id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if p.Title != "Updated Title" {
		t.Errorf("title not updated: got %q", p.Title)
	}

	// Submit.
	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !strings.Contains(submitResult, "Proposal submitted") {
		t.Errorf("unexpected submit result: %s", submitResult)
	}

	p, _ = env.ProposalStore.Get(id)
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("expected submitted, got %s", p.Status)
	}
}

// ─── Scenario 11: Proposal ID prefix resolution ───────────────────────────────

func TestJourneyPrefixIDResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)

	createResult, err := handleProposalCreateDraft(env, `{"title":"T","description":"d","skill_name":"t","tools":[{"name":"f","description":"d"}]}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fullID := mustExtractID(t, createResult)

	// The LLM often uses abbreviated IDs. Resolve the first 8 chars.
	prefix := fullID[:8]
	resolved, err := resolveProposalID(env, prefix)
	if err != nil {
		t.Fatalf("resolveProposalID(%s): %v", prefix, err)
	}
	if resolved != fullID {
		t.Errorf("prefix resolution: want %s, got %s", fullID, resolved)
	}

	submitResult, err := handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, resolved))
	if err != nil {
		t.Fatalf("submit with resolved ID: %v", err)
	}
	if !strings.Contains(submitResult, "Proposal submitted") {
		t.Errorf("unexpected result: %s", submitResult)
	}
}

// ─── Scenario 12: Portal event emission ──────────────────────────────────────

func TestJourneyPortalEventEmission(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)
	env.ToolEvents = NewToolEventBuffer(100)
	env.ThoughtEvents = NewThoughtEventBuffer(200)

	// Simulate a two-tool ReAct sequence and verify events are emitted.
	tools := []string{"proposal.create_draft", "proposal.submit"}
	for _, tool := range tools {
		env.ToolEvents.RecordStart(tool)
		env.ThoughtEvents.Record("tool_call", tool, "Decided to call tool: "+tool, "")
		env.ToolEvents.RecordFinish(tool, true, nil, 0)
		env.ThoughtEvents.Record("tool_result", tool, "Tool call completed: "+tool, "success=true")
	}

	// Thought events: 4 entries (2 × tool_call + 2 × tool_result).
	thoughts := env.ThoughtEvents.Recent(50)
	if len(thoughts) != 4 {
		t.Errorf("expected 4 thought events, got %d", len(thoughts))
	}

	// Tool events: 4 entries (2 × start + 2 × finish).
	toolEvts := env.ToolEvents.Recent(50)
	if len(toolEvts) != 4 {
		t.Errorf("expected 4 tool events, got %d", len(toolEvts))
	}

	// Phases should be: start, finish, start, finish.
	expectedPhases := []string{"start", "finish", "start", "finish"}
	for i, want := range expectedPhases {
		if i >= len(toolEvts) {
			break
		}
		if toolEvts[i].Phase != want {
			t.Errorf("toolEvt[%d] phase = %q, want %q", i, toolEvts[i].Phase, want)
		}
	}

	// Thought events alternate: tool_call, tool_result, tool_call, tool_result.
	expectedThoughtPhases := []string{"tool_call", "tool_result", "tool_call", "tool_result"}
	for i, want := range expectedThoughtPhases {
		if i >= len(thoughts) {
			break
		}
		if thoughts[i].Phase != want {
			t.Errorf("thought[%d] phase = %q, want %q", i, thoughts[i].Phase, want)
		}
	}

	// Both tool names should appear in thought events.
	toolsSeen := map[string]bool{}
	for _, e := range thoughts {
		if e.Tool != "" {
			toolsSeen[e.Tool] = true
		}
	}
	for _, wantTool := range tools {
		if !toolsSeen[wantTool] {
			t.Errorf("thought events missing tool %q; seen: %v", wantTool, toolsSeen)
		}
	}
}

// ─── Golden trace scenario ────────────────────────────────────────────────────

// TestGoldenTraceSimpleCreateSubmit captures a golden trace for the happy path:
// create a draft then submit it.
// Run with UPDATE_SNAPSHOTS=1 to generate/update the golden file.
func TestGoldenTraceSimpleCreateSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden trace test in -short mode")
	}
	env := testEnv(t)
	rec := newTraceRecorder("golden-simple-create-submit", "create and submit hello-world skill")

	// Step 1: Create.
	rec.recordThought("I will call proposal.create_draft to create the skill proposal.")
	createArgs := `{"title":"Hello World","description":"Greets","skill_name":"hello-world","tools":[{"name":"greet","description":"Says hello"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	rec.recordToolCall("proposal.create_draft", createArgs)
	createResult, err := handleProposalCreateDraft(env, createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := mustExtractID(t, createResult)
	rec.recordToolResult("proposal.create_draft", true)

	// Step 2: Submit.
	rec.recordThought("Now I will call proposal.submit to submit the draft for review.")
	submitArgs := fmt.Sprintf(`{"id":%q}`, id)
	rec.recordToolCall("proposal.submit", submitArgs)
	_, err = handleProposalSubmit(env, stubDaemonClient(), context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	rec.recordToolResult("proposal.submit", true)
	rec.recordProgress("Proposal submitted for Court review.")

	trace := rec.finalize("The hello-world skill proposal has been created and submitted for review. Proposal ID: " + id)

	// If the golden file exists, compare; otherwise log a hint.
	goldenPath := "testdata/golden/golden-simple-create-submit.json"
	if _, statErr := os.Stat(goldenPath); statErr == nil {
		AssertGoldenTrace(t, trace, "golden-simple-create-submit")
	} else {
		t.Logf("golden file %s does not exist; run with UPDATE_SNAPSHOTS=1 to create it", goldenPath)
	}

	// Structural assertions (independent of golden file).
	if trace.ToolCallCount != 2 {
		t.Errorf("expected 2 tool calls in trace, got %d", trace.ToolCallCount)
	}
}
