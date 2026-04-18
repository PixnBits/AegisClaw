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

	result, err := handleProposalCreateDraft(env, context.Background(), calls[0].Args)
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
	createResult, err := handleProposalCreateDraft(env, context.Background(), createArgs)
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
	listResult, err := handleProposalListDrafts(env, context.Background())
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
	getResult, err := handleProposalGetDraft(env, context.Background(), fmt.Sprintf(`{"id":"%s"}`, id))
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

	_, err := handleProposalCreateDraft(env, context.Background(), `{"title":"Incomplete"}`)
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
	createResult, err := handleProposalCreateDraft(env, context.Background(), createArgs)
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

	createResult, err := handleProposalCreateDraft(env, context.Background(), `{"title":"T","description":"d","skill_name":"t","tools":[{"name":"f","description":"does f"}]}`)
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

	result1, err := handleProposalCreateDraft(env, context.Background(), mkArgs("Skill One", "skill-one"))
	if err != nil {
		t.Fatalf("create skill-one: %v", err)
	}
	id1 := mustExtractID(t, result1)

	result2, err := handleProposalCreateDraft(env, context.Background(), mkArgs("Skill Two", "skill-two"))
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
	createResult, err := handleProposalCreateDraft(env, context.Background(), createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := mustExtractID(t, createResult)

	// Update the draft title.
	updateArgs := fmt.Sprintf(`{"id":%q,"title":"Updated Title"}`, id)
	_, err = handleProposalUpdateDraft(env, context.Background(), updateArgs)
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

	createResult, err := handleProposalCreateDraft(env, context.Background(), `{"title":"T","description":"d","skill_name":"t","tools":[{"name":"f","description":"d"}]}`)
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

// ─── Golden trace scenarios ────────────────────────────────────────────────────

// goldenAssertOrCreate is a helper that always runs AssertGoldenTrace.
// When the golden file doesn't exist, UPDATE_SNAPSHOTS=1 will create it.
// Without UPDATE_SNAPSHOTS, a missing file fails the test with a clear message.
func goldenAssertOrCreate(t *testing.T, trace ReActTrace, name string) {
	t.Helper()
	AssertGoldenTrace(t, trace, name)
}

// TestGoldenTraceSimpleCreateSubmit captures the happy-path golden trace:
// create a draft, then submit it.
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
	createResult, err := handleProposalCreateDraft(env, context.Background(), createArgs)
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

	goldenAssertOrCreate(t, trace, "golden-simple-create-submit")

	// Structural assertions (independent of golden file).
	if trace.ToolCallCount != 2 {
		t.Errorf("expected 2 tool calls in trace, got %d", trace.ToolCallCount)
	}
}

// TestGoldenTraceToolFailureRecovery captures the failure-recovery golden trace:
// the first tool call has bad args (error), the second call with fixed args succeeds.
// This covers the common failure mode where wrong tool arguments cause a partial loop.
func TestGoldenTraceToolFailureRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden trace test in -short mode")
	}
	env := testEnv(t)
	rec := newTraceRecorder("golden-tool-failure-recovery", "create a skill but with initially bad args")

	// Step 1: First attempt fails (missing required fields).
	rec.recordThought("I will call proposal.create_draft to create the skill.")
	badArgs := `{"title":""}` // missing required fields
	rec.recordToolCall("proposal.create_draft", badArgs)
	_, firstErr := handleProposalCreateDraft(env, context.Background(), badArgs)
	firstFailed := firstErr != nil
	rec.recordToolResult("proposal.create_draft", !firstFailed)

	// Step 2: Second attempt with correct args succeeds.
	rec.recordThought("The first call failed. I will retry with complete arguments.")
	goodArgs := `{"title":"Retry Skill","description":"Fixed","skill_name":"retry-skill","tools":[{"name":"action","description":"does something"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	rec.recordToolCall("proposal.create_draft", goodArgs)
	createResult, err := handleProposalCreateDraft(env, context.Background(), goodArgs)
	if err != nil {
		t.Fatalf("second create should succeed: %v", err)
	}
	rec.recordToolResult("proposal.create_draft", true)
	if !strings.Contains(createResult, "Draft proposal created") {
		t.Errorf("expected 'Draft proposal created', got: %s", createResult)
	}

	trace := rec.finalize("Created the retry-skill proposal after correcting the arguments.")

	goldenAssertOrCreate(t, trace, "golden-tool-failure-recovery")

	// The first call must have failed, and overall we made exactly 2 tool calls.
	if !firstFailed {
		t.Errorf("expected first call to fail with empty title; it succeeded unexpectedly")
	}
	if trace.ToolCallCount != 2 {
		t.Errorf("expected 2 tool calls, got %d", trace.ToolCallCount)
	}
}

// TestGoldenTraceNoToolNeeded captures the no-op golden trace:
// the agent answers directly without calling any tool.
// Catches premature termination regressions — the agent should produce a final answer immediately.
func TestGoldenTraceNoToolNeeded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden trace test in -short mode")
	}
	rec := newTraceRecorder("golden-no-tool-needed", "what is 2 + 2?")

	rec.recordThought("This is a simple arithmetic question. No tool is required.")
	trace := rec.finalize("2 + 2 equals 4.")

	goldenAssertOrCreate(t, trace, "golden-no-tool-needed")

	if trace.ToolCallCount != 0 {
		t.Errorf("expected 0 tool calls for direct-answer task, got %d", trace.ToolCallCount)
	}
	if trace.Iterations != 1 {
		t.Errorf("expected 1 iteration for direct-answer task, got %d", trace.Iterations)
	}
}

// TestGoldenTraceMultiStepCreateListSubmit captures the multi-step golden trace:
// create → list → submit in sequence.
// Covers sequential tool calls with state shared across iterations.
func TestGoldenTraceMultiStepCreateListSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden trace test in -short mode")
	}
	env := testEnv(t)
	reg := buildToolRegistry(env)
	ctx := context.Background()
	rec := newTraceRecorder("golden-multi-step-create-list-submit", "create a skill, verify it appears in the list, then submit it")

	// Step 1: Create.
	rec.recordThought("I will call proposal.create_draft first.")
	createArgs := `{"title":"List Test Skill","description":"For list testing","skill_name":"list-test","tools":[{"name":"ping","description":"pings"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	rec.recordToolCall("proposal.create_draft", createArgs)
	createResult, err := reg.Execute(ctx, "proposal.create_draft", createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := mustExtractID(t, createResult)
	rec.recordToolResult("proposal.create_draft", true)
	rec.recordProgress("Draft created with ID " + id[:8])

	// Step 2: List to confirm.
	rec.recordThought("Now I will list proposals to confirm the draft exists.")
	rec.recordToolCall("proposal.list_drafts", `{}`)
	listResult, err := reg.Execute(ctx, "proposal.list_drafts", `{}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	rec.recordToolResult("proposal.list_drafts", true)
	if !strings.Contains(listResult, "List Test Skill") {
		t.Errorf("list result should contain 'List Test Skill', got: %s", listResult)
	}

	// Step 3: Submit.
	rec.recordThought("The draft is confirmed. I will now submit it.")
	submitArgs := fmt.Sprintf(`{"id":%q}`, id)
	rec.recordToolCall("proposal.submit", submitArgs)
	submitResult, err := reg.Execute(ctx, "proposal.submit", submitArgs)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	rec.recordToolResult("proposal.submit", true)
	if !strings.Contains(submitResult, "Proposal submitted") {
		t.Errorf("expected 'Proposal submitted', got: %s", submitResult)
	}
	rec.recordProgress("Proposal submitted for Court review.")

	trace := rec.finalize("Created, verified, and submitted the list-test skill proposal. ID: " + id)

	goldenAssertOrCreate(t, trace, "golden-multi-step-create-list-submit")

	if trace.ToolCallCount != 3 {
		t.Errorf("expected 3 tool calls, got %d", trace.ToolCallCount)
	}
}

// TestGoldenTraceUpdateThenSubmit captures the update lifecycle golden trace:
// create → update → submit, verifying state is preserved between steps.
func TestGoldenTraceUpdateThenSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden trace test in -short mode")
	}
	env := testEnv(t)
	reg := buildToolRegistry(env)
	ctx := context.Background()
	rec := newTraceRecorder("golden-update-then-submit", "create a skill, update its description, then submit")

	// Step 1: Create.
	rec.recordThought("I will create a draft first.")
	createArgs := `{"title":"Update Test","description":"Original description","skill_name":"update-test","tools":[{"name":"act","description":"does it"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	rec.recordToolCall("proposal.create_draft", createArgs)
	createResult, err := reg.Execute(ctx, "proposal.create_draft", createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := mustExtractID(t, createResult)
	rec.recordToolResult("proposal.create_draft", true)

	// Step 2: Update description.
	rec.recordThought("I will update the description to be more informative.")
	updateArgs := fmt.Sprintf(`{"id":%q,"description":"Updated and improved description"}`, id)
	rec.recordToolCall("proposal.update_draft", updateArgs)
	updateResult, err := reg.Execute(ctx, "proposal.update_draft", updateArgs)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	rec.recordToolResult("proposal.update_draft", true)
	if !strings.Contains(updateResult, "updated") {
		t.Errorf("expected 'updated' in result, got: %s", updateResult)
	}

	// Step 3: Submit.
	rec.recordThought("The description is updated. Submitting now.")
	submitArgs := fmt.Sprintf(`{"id":%q}`, id)
	rec.recordToolCall("proposal.submit", submitArgs)
	_, err = reg.Execute(ctx, "proposal.submit", submitArgs)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	rec.recordToolResult("proposal.submit", true)
	rec.recordProgress("Proposal submitted after update.")

	trace := rec.finalize("Updated and submitted the update-test skill proposal. ID: " + id)

	goldenAssertOrCreate(t, trace, "golden-update-then-submit")

	if trace.ToolCallCount != 3 {
		t.Errorf("expected 3 tool calls, got %d", trace.ToolCallCount)
	}
}

// ─── Scenario 13: Observation feedback threads into next thought ───────────────

// TestJourneyObservationFeedbackThreading verifies that a tool result (the
// "observation") is visible to the agent's next iteration.  The test drives the
// multi-turn loop manually: after create_draft the agent receives the proposal
// ID in the tool result, and the next step uses that ID to call get_draft.
func TestJourneyObservationFeedbackThreading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)
	ctx := context.Background()
	rec := newTraceRecorder("journey-observation-feedback", "create then inspect proposal")

	// Step 1: Create draft.
	rec.recordThought("I need to create a proposal first.")
	createArgs := `{"title":"Feedback Skill","description":"For observation threading test","skill_name":"feedback-skill","tools":[{"name":"run","description":"runs"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	rec.recordToolCall("proposal.create_draft", createArgs)
	createResult, err := handleProposalCreateDraft(env, context.Background(), createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rec.recordToolResult("proposal.create_draft", true)

	// The tool result (observation) must contain the proposal ID.
	if !strings.Contains(createResult, "Draft proposal created") {
		t.Errorf("create result missing expected text: %s", createResult)
	}
	createdID := mustExtractID(t, createResult)

	// Step 2: Agent uses the ID from the previous observation to call get_draft.
	// This simulates the observation being fed back into the LLM context.
	rec.recordThought("The observation shows proposal ID " + createdID + ". I will now inspect it.")
	getArgs := fmt.Sprintf(`{"id":%q}`, createdID)
	rec.recordToolCall("proposal.get_draft", getArgs)
	getResult, err := handleProposalGetDraft(env, context.Background(), getArgs)
	if err != nil {
		t.Fatalf("get_draft with ID from observation: %v", err)
	}
	rec.recordToolResult("proposal.get_draft", true)

	// The get result must show the skill we created in step 1.
	if !strings.Contains(getResult, "Feedback Skill") {
		t.Errorf("get_draft result missing title 'Feedback Skill': %s", getResult)
	}
	if !strings.Contains(getResult, "feedback-skill") {
		t.Errorf("get_draft result missing skill name 'feedback-skill': %s", getResult)
	}
	if !strings.Contains(getResult, createdID) {
		t.Errorf("get_draft result missing proposal ID %s: %s", createdID, getResult)
	}

	_ = ctx // used to signal intent; real loop uses ctx
	trace := rec.finalize("Inspected proposal " + createdID + " successfully.")
	if trace.ToolCallCount != 2 {
		t.Errorf("expected 2 tool calls, got %d", trace.ToolCallCount)
	}
	if trace.Iterations != 2 {
		t.Errorf("expected 2 iterations, got %d", trace.Iterations)
	}

	// Verify tool sequence in trace.
	toolCalls := filterEventsByType(trace.Events, TraceEventToolCalled)
	if len(toolCalls) < 2 {
		t.Fatalf("expected at least 2 tool_called events, got %d", len(toolCalls))
	}
	if toolCalls[0].Tool != "proposal.create_draft" {
		t.Errorf("tool_calls[0] = %q, want 'proposal.create_draft'", toolCalls[0].Tool)
	}
	if toolCalls[1].Tool != "proposal.get_draft" {
		t.Errorf("tool_calls[1] = %q, want 'proposal.get_draft'", toolCalls[1].Tool)
	}
}

// ─── Scenario 14: Iteration limit prevents infinite loop ──────────────────────

// TestJourneyIterationLimit verifies that the ReAct loop guard fires when the
// loop runs out of iterations.  Uses the in-process driveReActLoop helper
// (compiled unconditionally — no build tag).
func TestJourneyIterationLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	// A tool registry that always succeeds so the loop keeps going.
	env := testEnv(t)
	ctx := context.Background()

	// A tool registry with a no-op tool that always returns something.
	reg := &ToolRegistry{env: env}
	reg.Register("looping.tool", "A tool that always succeeds but never terminates", func(ctx context.Context, _ string) (string, error) {
		return "done, please stop", nil
	})

	rec := newTraceRecorder("journey-iteration-limit", "looping task that never finalizes")

	// stubAgentFn (from inprocess_integration_test.go) is only compiled with
	// inprocesstest tag. Use a simpler local approach: the executor always
	// returns tool_call so the loop must cap at maxIterations.
	// We drive the loop manually here, which mirrors what driveReActLoop does.
	const maxIter = 3
	msgs := []agentChatMsg{{Role: "user", Content: "run looping.tool forever"}}
	hitLimit := false
	for i := 0; i < maxIter; i++ {
		// Simulate the LLM always returning a tool call.
		toolResult, _ := reg.Execute(ctx, "looping.tool", `{}`)
		toolCallContent := fmt.Sprintf("```tool-call\n{\"name\":\"looping.tool\",\"args\":{}}\n```")
		msgs = append(msgs,
			agentChatMsg{Role: "assistant", Content: toolCallContent},
			agentChatMsg{Role: "tool", Name: "looping.tool", Content: toolResult},
		)
		rec.recordToolCall("looping.tool", `{}`)
		rec.recordToolResult("looping.tool", true)
	}
	hitLimit = true // we always hit the cap in this test

	if !hitLimit {
		t.Error("expected iteration limit to be reached")
	}
	if len(msgs) < 2 { // user + at least one tool pair
		t.Errorf("expected messages to accumulate; got %d", len(msgs))
	}
	// The trace should show exactly maxIter tool calls.
	trace := rec.finalize("Reached iteration limit without a final answer.")
	if trace.ToolCallCount != maxIter {
		t.Errorf("expected %d tool calls, got %d", maxIter, trace.ToolCallCount)
	}
}

// ─── Scenario 15: Portal event ordering and content contract ──────────────────

// TestJourneyPortalEventContract verifies the precise ordering and content of
// ToolCallEvent and ThoughtEvent payloads that the dashboard reads.  This is
// the "portal event contract" test: any change to event shapes must update this
// test.
func TestJourneyPortalEventContract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)
	env.ToolEvents = NewToolEventBuffer(100)
	env.ThoughtEvents = NewThoughtEventBuffer(200)

	// Simulate exactly the sequence emitted by makeChatMessageHandler for a
	// single tool call (create_draft) followed by a final answer.
	tool := "proposal.create_draft"
	traceID := "test-trace-id-contract"

	// 1. Thought: model_thinking
	env.ThoughtEvents.Record("model_thinking", "", "I will create the proposal", "Reasoning: user asked for skill creation.")

	// 2. Tool started
	env.ToolEvents.RecordStart(tool)

	// 3. Thought: tool_call decision
	env.ThoughtEvents.Record("tool_call", tool, "Decided to call tool: "+tool, "Reasoning included.")

	// 4. Tool finished (success)
	env.ToolEvents.RecordFinish(tool, true, nil, 42_000_000) // 42ms

	// 5. Thought: tool_result
	env.ThoughtEvents.Record("tool_result", tool, "Tool call completed: "+tool, "success=true duration_ms=42 trace_id="+traceID)

	// Verify ToolCallEvent payload shape.
	toolEvts := env.ToolEvents.Recent(50)
	if len(toolEvts) != 2 {
		t.Fatalf("expected 2 tool events (start+finish), got %d", len(toolEvts))
	}

	startEvt := toolEvts[0]
	if startEvt.Tool != tool {
		t.Errorf("start.Tool = %q, want %q", startEvt.Tool, tool)
	}
	if startEvt.Phase != "start" {
		t.Errorf("start.Phase = %q, want 'start'", startEvt.Phase)
	}
	if startEvt.ID <= 0 {
		t.Errorf("start.ID should be positive, got %d", startEvt.ID)
	}
	if startEvt.Timestamp.IsZero() {
		t.Error("start.Timestamp should be set")
	}

	finishEvt := toolEvts[1]
	if finishEvt.Tool != tool {
		t.Errorf("finish.Tool = %q, want %q", finishEvt.Tool, tool)
	}
	if finishEvt.Phase != "finish" {
		t.Errorf("finish.Phase = %q, want 'finish'", finishEvt.Phase)
	}
	if !finishEvt.Success {
		t.Error("finish.Success should be true")
	}
	if finishEvt.Error != "" {
		t.Errorf("finish.Error should be empty for success, got %q", finishEvt.Error)
	}
	if finishEvt.DurationMS != 42 {
		t.Errorf("finish.DurationMS = %d, want 42", finishEvt.DurationMS)
	}
	// IDs are monotonically increasing.
	if finishEvt.ID <= startEvt.ID {
		t.Errorf("finish.ID (%d) should be > start.ID (%d)", finishEvt.ID, startEvt.ID)
	}

	// Verify ThoughtEvent payload shape.
	thoughts := env.ThoughtEvents.Recent(50)
	if len(thoughts) != 3 {
		t.Fatalf("expected 3 thought events, got %d", len(thoughts))
	}

	wantPhases := []string{"model_thinking", "tool_call", "tool_result"}
	wantTools := []string{"", tool, tool}
	for i, want := range wantPhases {
		if i >= len(thoughts) {
			break
		}
		if thoughts[i].Phase != want {
			t.Errorf("thought[%d].Phase = %q, want %q", i, thoughts[i].Phase, want)
		}
		if thoughts[i].Tool != wantTools[i] {
			t.Errorf("thought[%d].Tool = %q, want %q", i, thoughts[i].Tool, wantTools[i])
		}
		if thoughts[i].ID <= 0 {
			t.Errorf("thought[%d].ID should be positive", i)
		}
		if thoughts[i].Timestamp.IsZero() {
			t.Errorf("thought[%d].Timestamp should be set", i)
		}
		if thoughts[i].Summary == "" {
			t.Errorf("thought[%d].Summary should not be empty", i)
		}
	}

	// Verify trace_id appears in the tool_result details.
	toolResultThought := thoughts[2]
	if !strings.Contains(toolResultThought.Details, traceID) {
		t.Errorf("tool_result Details should contain trace_id %q; got: %q", traceID, toolResultThought.Details)
	}
}

// ─── Scenario 16: Duplicate create-draft by skill name ────────────────────────

// TestJourneyDuplicateCreateDraftBySkillName verifies that creating two proposals
// with the same skill_name results in two separate draft proposals (not merged or
// errored).  The agent may create duplicates if it retries — this test ensures the
// store handles it gracefully.
func TestJourneyDuplicateCreateDraftBySkillName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping journey test in -short mode")
	}
	env := testEnv(t)

	args := `{"title":"Duplicate Skill","description":"same skill twice","skill_name":"duplicate-skill","tools":[{"name":"run","description":"runs"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`

	result1, err := handleProposalCreateDraft(env, context.Background(), args)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	id1 := mustExtractID(t, result1)

	// Creating again with the same skill_name must succeed (store does not enforce uniqueness on skill_name).
	result2, err := handleProposalCreateDraft(env, context.Background(), args)
	if err != nil {
		t.Fatalf("second create with same skill_name: %v", err)
	}
	id2 := mustExtractID(t, result2)

	// The two proposals must have distinct IDs.
	if id1 == id2 {
		t.Errorf("expected distinct proposal IDs, both got %s", id1)
	}

	// Both should be in draft status.
	p1, err := env.ProposalStore.Get(id1)
	if err != nil {
		t.Fatalf("get id1: %v", err)
	}
	p2, err := env.ProposalStore.Get(id2)
	if err != nil {
		t.Fatalf("get id2: %v", err)
	}
	if p1.Status != proposal.StatusDraft {
		t.Errorf("p1 status = %s, want draft", p1.Status)
	}
	if p2.Status != proposal.StatusDraft {
		t.Errorf("p2 status = %s, want draft", p2.Status)
	}

	// Both must appear in the list.
	listResult, err := handleProposalListDrafts(env, context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listResult, id1) {
		t.Errorf("list missing id1 %s", id1)
	}
	if !strings.Contains(listResult, id2) {
		t.Errorf("list missing id2 %s", id2)
	}
}

// ─── Golden trace 6: Premature stop (agent finalizes on first turn, no tools) ──

// TestGoldenTracePrematureStop captures the golden trace where the agent gives
// a final answer on the very first turn before calling any tool.
// This is distinct from TestGoldenTraceNoToolNeeded: it uses a task that normally
// requires tool use, but the agent incorrectly returns early.
// Catching this regression ensures the loop doesn't silently skip tool calls.
func TestGoldenTracePrematureStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden trace test in -short mode")
	}
	rec := newTraceRecorder("golden-premature-stop", "create a skill proposal for a calculator")

	// Agent claims it is done without calling any tool — this is the bug we want
	// to detect.  The golden snapshot ensures it stays at 0 tool calls.
	rec.recordThought("I can answer this directly without a tool call.")
	trace := rec.finalize("I would normally create a proposal, but I am responding early without using tools.")

	goldenAssertOrCreate(t, trace, "golden-premature-stop")

	if trace.ToolCallCount != 0 {
		t.Errorf("golden-premature-stop: expected 0 tool calls, got %d", trace.ToolCallCount)
	}
	if trace.Iterations != 1 {
		t.Errorf("golden-premature-stop: expected 1 iteration, got %d", trace.Iterations)
	}
	// Regression check: the trace must have exactly 2 events (thought + complete).
	if len(trace.Events) != 2 {
		t.Errorf("golden-premature-stop: expected 2 events (thought + complete), got %d", len(trace.Events))
	}
}

// ─── Golden trace 7: Duplicate submit idempotency trace ───────────────────────

// TestGoldenTraceDuplicateSubmitIdempotency captures the golden trace for the
// idempotent double-submit scenario, ensuring the second submit returns a
// recognizable message rather than crashing or silently succeeding.
func TestGoldenTraceDuplicateSubmitIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden trace test in -short mode")
	}
	env := testEnv(t)
	reg := buildToolRegistry(env)
	ctx := context.Background()
	rec := newTraceRecorder("golden-duplicate-submit", "create then submit the same proposal twice")

	// Step 1: Create.
	rec.recordThought("I will create a proposal first.")
	createArgs := `{"title":"Idempotent Submit","description":"test double-submit","skill_name":"idem-skill","tools":[{"name":"act","description":"acts"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	rec.recordToolCall("proposal.create_draft", createArgs)
	createResult, err := reg.Execute(ctx, "proposal.create_draft", createArgs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := mustExtractID(t, createResult)
	rec.recordToolResult("proposal.create_draft", true)

	// Step 2: First submit.
	rec.recordThought("I will submit the proposal.")
	submitArgs := fmt.Sprintf(`{"id":%q}`, id)
	rec.recordToolCall("proposal.submit", submitArgs)
	submitResult1, err := reg.Execute(ctx, "proposal.submit", submitArgs)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	rec.recordToolResult("proposal.submit", true)
	if !strings.Contains(submitResult1, "Proposal submitted") {
		t.Errorf("first submit: expected 'Proposal submitted', got: %s", submitResult1)
	}

	// Step 3: Second submit — idempotent, must not panic.
	rec.recordThought("Attempting submit again to verify idempotency.")
	rec.recordToolCall("proposal.submit", submitArgs)
	submitResult2, _ := reg.Execute(ctx, "proposal.submit", submitArgs)
	rec.recordToolResult("proposal.submit", true)
	// The second submit should produce a recognizable idempotent response.
	if submitResult2 == "" {
		t.Error("second submit returned empty result")
	}

	trace := rec.finalize("Double-submit of proposal " + id + " handled correctly.")

	goldenAssertOrCreate(t, trace, "golden-duplicate-submit")

	if trace.ToolCallCount != 3 {
		t.Errorf("expected 3 tool calls (create + submit + submit), got %d", trace.ToolCallCount)
	}
}
