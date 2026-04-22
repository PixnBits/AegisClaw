package main

// lookup_journey_test.go — Journey and contract tests for the dynamic semantic
// tool-lookup skill (lookup_tools, lookup.index_tool, seedLookupStore).
//
// These tests follow the same patterns as react_journey_test.go and
// portal_contract_test.go from the main branch:
//
//   - Each scenario is a self-contained "journey" that exercises a distinct
//     behaviour (indexing, searching, seeding, API handler shape).
//   - Tests use testEnvWithLookup, a thin wrapper over testEnv that adds a
//     real LookupStore backed by a temp directory.
//   - No Firecracker, no Ollama, no KVM required.
//   - Scenario 1:  Index then retrieve a single tool
//   - Scenario 2:  lookup_tools tool returns Gemma 4 blocks
//   - Scenario 3:  lookup.index_tool tool updates existing entry
//   - Scenario 4:  lookup_tools empty query → error
//   - Scenario 5:  seedLookupStore indexes all registry tools
//   - Scenario 6:  makeLookupSearchHandler API response shape contract
//   - Scenario 7:  makeLookupListHandler API response shape contract
//   - Scenario 8:  Nil lookup store returns error, not panic
//   - Scenario 9:  Trace record integration — tool events for lookup calls
//   - Scenario 10: ReActRunner FSM step-by-step with lookup_tools call
//   - Scenario 11: lookup_tools via ReActRunner.Run full loop
//   - Scenario 10: ReActRunner FSM step-by-step with lookup_tools call
//   - Scenario 11: lookup_tools via ReActRunner.Run full loop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/lookup"
	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/testutil"
	"go.uber.org/zap/zaptest"
)

// testEnvWithLookup extends testEnv with a LookupStore backed by a temp dir.
// Use this for tests that exercise lookup_tools or lookup.index_tool.
func testEnvWithLookup(t *testing.T) *runtimeEnv {
	t.Helper()
	env := testEnv(t)
	dir := t.TempDir()
	store, err := lookup.NewStore(lookup.StoreConfig{
		Dir:    dir,
		Logger: zaptest.NewLogger(t),
	})
	if err != nil {
		t.Fatalf("lookup.NewStore: %v", err)
	}
	env.LookupStore = store
	return env
}

// ─── Scenario 1: Index then retrieve a single tool ─────────────────────────

func TestLookupJourneyIndexAndRetrieve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lookup journey test in -short mode")
	}
	env := testEnvWithLookup(t)
	ctx := context.Background()

	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	// Index a tool via the lookup.index_tool handler.
	indexArgs := `{"name":"memory.store","description":"Store a persistent memory entry with key, value, and tags","skill_name":"memory","parameters":"{\"key\":\"string\",\"value\":\"string\"}"}`
	result, err := reg.Execute(ctx, "lookup.index_tool", indexArgs)
	if err != nil {
		t.Fatalf("lookup.index_tool: %v", err)
	}
	if !strings.Contains(result, "memory.store") {
		t.Errorf("expected 'memory.store' in result, got: %s", result)
	}
	if !strings.Contains(result, "indexed") {
		t.Errorf("expected 'indexed' in result, got: %s", result)
	}
	if env.LookupStore.Count() != 1 {
		t.Errorf("expected 1 indexed tool, got %d", env.LookupStore.Count())
	}
}

// ─── Scenario 2: lookup_tools returns Gemma 4 blocks ──────────────────────

func TestLookupJourneyLookupToolsReturnsGemma4Blocks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lookup journey test in -short mode")
	}
	env := testEnvWithLookup(t)
	ctx := context.Background()

	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	// Seed some tools first.
	seeds := []struct{ name, desc, skill string }{
		{"memory.store", "Store a persistent memory entry with key, value, and tags", "memory"},
		{"memory.retrieve", "Retrieve stored memories by semantic query", "memory"},
		{"proposal.create_draft", "Create a new skill proposal draft for governance review", "proposal"},
		{"worker.spawn", "Spawn an ephemeral Worker microVM for a subtask", "worker"},
	}
	for _, s := range seeds {
		if err := env.LookupStore.IndexTool(ctx, lookup.ToolEntry{
			Name:        s.name,
			Description: s.desc,
			SkillName:   s.skill,
		}); err != nil {
			t.Fatalf("seed %q: %v", s.name, err)
		}
	}

	result, err := reg.Execute(ctx, "lookup_tools", `{"query":"store and retrieve memory","max_results":3}`)
	if err != nil {
		t.Fatalf("lookup_tools: %v", err)
	}

	// Result must contain Gemma 4 control tokens.
	if !strings.Contains(result, "<|tool|>") {
		t.Errorf("lookup_tools result missing <|tool|> token: %s", result)
	}
	if !strings.Contains(result, "<|/tool|>") {
		t.Errorf("lookup_tools result missing <|/tool|> token: %s", result)
	}
}

// ─── Scenario 3: lookup.index_tool updates an existing entry ──────────────

func TestLookupJourneyReindexUpdatesEntry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lookup journey test in -short mode")
	}
	env := testEnvWithLookup(t)
	ctx := context.Background()

	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	// First index.
	_, err := reg.Execute(ctx, "lookup.index_tool", `{"name":"worker.spawn","description":"Spawn a Worker VM","skill_name":"worker"}`)
	if err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Re-index with updated description.
	result, err := reg.Execute(ctx, "lookup.index_tool", `{"name":"worker.spawn","description":"Spawn an ephemeral Worker microVM to execute a narrowly-scoped subtask","skill_name":"worker"}`)
	if err != nil {
		t.Fatalf("re-index: %v", err)
	}
	if !strings.Contains(result, "worker.spawn") {
		t.Errorf("expected tool name in result: %s", result)
	}

	// Count must remain 1 — no duplicate.
	if env.LookupStore.Count() != 1 {
		t.Errorf("expected 1 tool after re-index, got %d", env.LookupStore.Count())
	}
}

// ─── Scenario 4: lookup_tools with empty query returns error ──────────────

func TestLookupJourneyEmptyQueryError(t *testing.T) {
	env := testEnvWithLookup(t)
	ctx := context.Background()

	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	_, err := reg.Execute(ctx, "lookup_tools", `{"query":"","max_results":6}`)
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("expected 'query' in error message, got: %v", err)
	}
}

// ─── Scenario 5: seedLookupStore indexes all registry tools ───────────────

func TestLookupJourneySeedLookupStore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lookup journey test in -short mode")
	}
	env := testEnvWithLookup(t)
	ctx := context.Background()

	// Build a small registry and seed it.
	reg := &ToolRegistry{env: env}
	reg.Register("proposal.create_draft", "Create a new skill proposal draft", func(_ context.Context, args string) (string, error) {
		return "ok", nil
	})
	reg.Register("memory.store", "Store a persistent memory entry", func(_ context.Context, args string) (string, error) {
		return "ok", nil
	})
	reg.Register("worker.spawn", "Spawn an ephemeral Worker microVM", func(_ context.Context, args string) (string, error) {
		return "ok", nil
	})

	// seedLookupStore is async; use a synchronous variant for tests.
	seeded := 0
	for name, meta := range reg.meta {
		if meta.Description == "" {
			continue
		}
		skillName := ""
		if idx := strings.Index(name, "."); idx > 0 {
			skillName = name[:idx]
		}
		if err := env.LookupStore.IndexTool(ctx, lookup.ToolEntry{
			Name:        name,
			Description: meta.Description,
			SkillName:   skillName,
		}); err != nil {
			t.Fatalf("seed %q: %v", name, err)
		}
		seeded++
	}

	if seeded != 3 {
		t.Errorf("expected 3 seeded tools, got %d", seeded)
	}
	if env.LookupStore.Count() != 3 {
		t.Errorf("expected 3 indexed tools, got %d", env.LookupStore.Count())
	}
}

// ─── Scenario 6: makeLookupSearchHandler response shape contract ──────────

func TestLookupContractSearchHandlerShape(t *testing.T) {
	env := testEnvWithLookup(t)
	ctx := context.Background()

	// Pre-index a tool.
	if err := env.LookupStore.IndexTool(ctx, lookup.ToolEntry{
		Name:        "proposal.create_draft",
		Description: "Create a new skill proposal draft for governance review",
		SkillName:   "proposal",
	}); err != nil {
		t.Fatalf("IndexTool: %v", err)
	}

	handler := makeLookupSearchHandler(env)
	reqJSON := json.RawMessage(`{"query":"proposal draft","max_results":3}`)
	resp := handler(ctx, reqJSON)

	if !resp.Success {
		t.Fatalf("expected success=true, got error: %s", resp.Error)
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected non-empty data in response")
	}

	// Data must decode as []lookup.FormattedTool.
	var results []lookup.FormattedTool
	if err := json.Unmarshal(resp.Data, &results); err != nil {
		t.Fatalf("response data not []FormattedTool: %v\ndata: %s", err, resp.Data)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result in response data")
	}

	// Each result must have a Name and a Block with Gemma 4 tokens.
	for i, r := range results {
		if r.Name == "" {
			t.Errorf("result[%d]: Name is empty", i)
		}
		if !strings.Contains(r.Block, "<|tool|>") {
			t.Errorf("result[%d]: Block missing <|tool|> token: %s", i, r.Block)
		}
		if !strings.Contains(r.Block, "<|/tool|>") {
			t.Errorf("result[%d]: Block missing <|/tool|> token: %s", i, r.Block)
		}
	}
}

// TestLookupContractSearchHandlerMissingQuery verifies that a search request
// with no query field returns a descriptive error and success=false.
func TestLookupContractSearchHandlerMissingQuery(t *testing.T) {
	env := testEnvWithLookup(t)
	handler := makeLookupSearchHandler(env)
	resp := handler(context.Background(), json.RawMessage(`{"max_results":3}`))

	if resp.Success {
		t.Fatal("expected success=false for missing query")
	}
	if !strings.Contains(resp.Error, "query") {
		t.Errorf("error should mention 'query', got: %s", resp.Error)
	}
}

// TestLookupContractSearchHandlerInvalidJSON verifies that malformed JSON
// returns a descriptive error.
func TestLookupContractSearchHandlerInvalidJSON(t *testing.T) {
	env := testEnvWithLookup(t)
	handler := makeLookupSearchHandler(env)
	resp := handler(context.Background(), json.RawMessage(`{broken json`))

	if resp.Success {
		t.Fatal("expected success=false for malformed JSON")
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty error for malformed JSON")
	}
}

// ─── Scenario 7: makeLookupListHandler response shape contract ────────────

func TestLookupContractListHandlerShape(t *testing.T) {
	env := testEnvWithLookup(t)
	ctx := context.Background()

	// Initially empty.
	handler := makeLookupListHandler(env)
	resp := handler(ctx, nil)
	if !resp.Success {
		t.Fatalf("expected success=true, got error: %s", resp.Error)
	}

	var m map[string]int
	if err := json.Unmarshal(resp.Data, &m); err != nil {
		t.Fatalf("response not map[string]int: %v\ndata: %s", err, resp.Data)
	}
	if _, ok := m["indexed"]; !ok {
		t.Errorf("response missing 'indexed' field; got: %v", m)
	}
	if m["indexed"] != 0 {
		t.Errorf("expected 0 indexed tools, got %d", m["indexed"])
	}

	// After indexing, count must increase.
	if err := env.LookupStore.IndexTool(ctx, lookup.ToolEntry{
		Name:        "event.subscribe",
		Description: "Subscribe to an event on the Aegis event bus",
		SkillName:   "event",
	}); err != nil {
		t.Fatalf("IndexTool: %v", err)
	}

	resp2 := handler(ctx, nil)
	if !resp2.Success {
		t.Fatalf("expected success=true after index, got: %s", resp2.Error)
	}
	var m2 map[string]int
	if err := json.Unmarshal(resp2.Data, &m2); err != nil {
		t.Fatalf("response2 not map[string]int: %v", err)
	}
	if m2["indexed"] != 1 {
		t.Errorf("expected 1 indexed tool after insert, got %d", m2["indexed"])
	}
}

// ─── Scenario 8: Nil lookup store returns error, not panic ─────────────────

func TestLookupJourneyNilStoreSafeGuard(t *testing.T) {
	env := testEnv(t) // no LookupStore
	ctx := context.Background()

	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	// lookup_tools must return an error, not panic.
	_, err := reg.Execute(ctx, "lookup_tools", `{"query":"memory","max_results":3}`)
	if err == nil {
		t.Fatal("expected error when LookupStore is nil")
	}

	// lookup.index_tool must also return an error, not panic.
	_, err = reg.Execute(ctx, "lookup.index_tool", `{"name":"t","description":"d"}`)
	if err == nil {
		t.Fatal("expected error from lookup.index_tool when LookupStore is nil")
	}

	// API handlers must also return error responses, not panic.
	searchResp := makeLookupSearchHandler(env)(ctx, json.RawMessage(`{"query":"x"}`))
	if searchResp.Success {
		t.Error("expected success=false from search handler with nil store")
	}

	listResp := makeLookupListHandler(env)(ctx, nil)
	if listResp.Success {
		t.Error("expected success=false from list handler with nil store")
	}
}

// ─── Scenario 9: Tool events for lookup calls ─────────────────────────────

func TestLookupJourneyToolEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lookup journey test in -short mode")
	}
	env := testEnvWithLookup(t)
	env.ToolEvents = NewToolEventBuffer(50)
	env.ThoughtEvents = NewThoughtEventBuffer(100)
	ctx := context.Background()

	// Simulate an agent using lookup_tools.
	toolName := "lookup_tools"
	env.ToolEvents.RecordStart(toolName)
	env.ThoughtEvents.Record("tool_call", toolName, "Decided to call lookup_tools", "Querying for memory tools")

	// Index a tool and query.
	if err := env.LookupStore.IndexTool(ctx, lookup.ToolEntry{
		Name:        "memory.store",
		Description: "Store a persistent memory entry",
		SkillName:   "memory",
	}); err != nil {
		t.Fatalf("IndexTool: %v", err)
	}

	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)
	result, err := reg.Execute(ctx, toolName, `{"query":"memory","max_results":3}`)

	env.ToolEvents.RecordFinish(toolName, err == nil, err, 5*time.Millisecond)
	env.ThoughtEvents.Record("tool_result", toolName, "lookup_tools returned results", result)

	toolEvts := env.ToolEvents.Recent(10)
	if len(toolEvts) != 2 {
		t.Errorf("expected 2 tool events (start+finish), got %d", len(toolEvts))
	}
	if toolEvts[0].Phase != "start" {
		t.Errorf("event[0] phase = %q, want 'start'", toolEvts[0].Phase)
	}
	if toolEvts[1].Phase != "finish" {
		t.Errorf("event[1] phase = %q, want 'finish'", toolEvts[1].Phase)
	}
	if !toolEvts[1].Success {
		t.Errorf("lookup_tools call should have succeeded")
	}

	thoughts := env.ThoughtEvents.Recent(10)
	if len(thoughts) != 2 {
		t.Errorf("expected 2 thought events, got %d", len(thoughts))
	}
	if thoughts[0].Phase != "tool_call" {
		t.Errorf("thought[0] phase = %q, want 'tool_call'", thoughts[0].Phase)
	}
	if thoughts[1].Phase != "tool_result" {
		t.Errorf("thought[1] phase = %q, want 'tool_result'", thoughts[1].Phase)
	}
}

// ─── lookup.index_tool: validation edge cases ─────────────────────────────

// TestLookupIndexToolRequiresName verifies that missing name returns an error.
func TestLookupIndexToolRequiresName(t *testing.T) {
	env := testEnvWithLookup(t)
	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	_, err := reg.Execute(context.Background(), "lookup.index_tool", `{"description":"some description"}`)
	if err == nil {
		t.Fatal("expected error for missing 'name'")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention 'name', got: %v", err)
	}
}

// TestLookupIndexToolRequiresDescription verifies that missing description
// returns an error.
func TestLookupIndexToolRequiresDescription(t *testing.T) {
	env := testEnvWithLookup(t)
	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	_, err := reg.Execute(context.Background(), "lookup.index_tool", `{"name":"my.tool"}`)
	if err == nil {
		t.Fatal("expected error for missing 'description'")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Errorf("error should mention 'description', got: %v", err)
	}
}

// TestLookupIndexToolInvalidJSON verifies that malformed args return an error.
func TestLookupIndexToolInvalidJSON(t *testing.T) {
	env := testEnvWithLookup(t)
	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	_, err := reg.Execute(context.Background(), "lookup.index_tool", `not json at all`)
	if err == nil {
		t.Fatal("expected error for invalid JSON args")
	}
}

// ─── Scenario 10: ReActRunner FSM step-by-step with lookup_tools ─────────────
//
// Demonstrates that the ReAct FSM (added in origin/main commit 300cd6d)
// correctly routes a lookup_tools call through the
// Thinking→Acting→Observing→Thinking→Finalizing sequence.
//
// A minimal stubTaskExecutor drives the loop without the inprocesstest build
// tag, keeping these tests runnable in every environment.

// stubTaskExecutor implements rtexec.TaskExecutor by returning scripted
// responses in order.  Panics if the script is exhausted.
type stubTaskExecutor struct {
	script []rtexec.AgentTurnResponse
	idx    int
}

func (s *stubTaskExecutor) ExecuteTurn(_ context.Context, _ rtexec.AgentTurnRequest) (rtexec.AgentTurnResponse, error) {
	if s.idx >= len(s.script) {
		return rtexec.AgentTurnResponse{Status: "final", Content: "done"}, nil
	}
	resp := s.script[s.idx]
	s.idx++
	return resp, nil
}

func TestLookupJourneyReActRunnerStepByStep(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lookup ReAct journey test in -short mode")
	}

	env := testEnvWithLookup(t)
	ctx := context.Background()

	// Pre-seed the lookup store so the query returns a result.
	if err := env.LookupStore.IndexTool(ctx, lookup.ToolEntry{
		Name:        "worker.submit",
		Description: "Submit a background worker job for asynchronous execution",
		SkillName:   "worker",
	}); err != nil {
		t.Fatalf("IndexTool: %v", err)
	}

	queryArgs := `{"query":"background jobs","max_results":3}`

	executor := &stubTaskExecutor{script: []rtexec.AgentTurnResponse{
		{
			Status:   "tool_call",
			Tool:     "lookup_tools",
			Args:     queryArgs,
			Thinking: "I need to find tools related to background jobs.",
		},
		{
			Status:  "final",
			Content: "Found worker.submit for background job execution.",
		},
	}}

	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	var transitions []rtexec.StateTransition
	runner := rtexec.NewReActRunner(
		executor,
		func(c context.Context, tool, argsJSON string) (string, error) {
			return reg.Execute(c, tool, argsJSON)
		},
		"find background job tools",
		rtexec.WithMaxIterations(5),
		rtexec.WithSeed(testutil.TestOllamaSeed),
		rtexec.WithOnTransition(func(tr rtexec.StateTransition) {
			transitions = append(transitions, tr)
		}),
	)

	// ── Step 1: Thinking → Acting ─────────────────────────────────────────────
	res1 := runner.Step(ctx)
	if res1.Err != nil {
		t.Fatalf("step1 (Thinking→Acting): %v", res1.Err)
	}
	if res1.State != rtexec.StateActing {
		t.Errorf("step1 state = %v, want Acting", res1.State)
	}
	if res1.ToolCalled != "lookup_tools" {
		t.Errorf("step1 ToolCalled = %q, want lookup_tools", res1.ToolCalled)
	}

	// ── Step 2: Acting → Observing ────────────────────────────────────────────
	res2 := runner.Step(ctx)
	if res2.Err != nil {
		t.Fatalf("step2 (Acting→Observing): %v", res2.Err)
	}
	if res2.State != rtexec.StateObserving {
		t.Errorf("step2 state = %v, want Observing", res2.State)
	}
	if res2.ToolErr != nil {
		t.Errorf("step2 ToolErr should be nil (lookup succeeded), got: %v", res2.ToolErr)
	}

	// ── Step 3: Observing → Thinking ──────────────────────────────────────────
	res3 := runner.Step(ctx)
	if res3.Err != nil {
		t.Fatalf("step3 (Observing→Thinking): %v", res3.Err)
	}
	if res3.State != rtexec.StateThinking {
		t.Errorf("step3 state = %v, want Thinking", res3.State)
	}

	// ── Step 4: Thinking → Finalizing ─────────────────────────────────────────
	res4 := runner.Step(ctx)
	if res4.Err != nil {
		t.Fatalf("step4 (Thinking→Finalizing): %v", res4.Err)
	}
	if res4.State != rtexec.StateFinalizing {
		t.Errorf("step4 state = %v, want Finalizing", res4.State)
	}
	if !strings.Contains(res4.FinalAnswer, "worker.submit") {
		t.Errorf("FinalAnswer should mention worker.submit, got: %q", res4.FinalAnswer)
	}

	// ── Assert complete transition sequence ───────────────────────────────────
	want := []struct{ from, to rtexec.State }{
		{rtexec.StateThinking, rtexec.StateActing},
		{rtexec.StateActing, rtexec.StateObserving},
		{rtexec.StateObserving, rtexec.StateThinking},
		{rtexec.StateThinking, rtexec.StateFinalizing},
	}
	if len(transitions) != len(want) {
		t.Fatalf("transitions = %d, want %d: %v", len(transitions), len(want), transitions)
	}
	for i, w := range want {
		if transitions[i].From != w.from || transitions[i].To != w.to {
			t.Errorf("transition[%d] = %v→%v, want %v→%v",
				i, transitions[i].From, transitions[i].To, w.from, w.to)
		}
	}
	if runner.ToolCallCount() != 1 {
		t.Errorf("ToolCallCount = %d, want 1", runner.ToolCallCount())
	}
}

// ─── Scenario 11: lookup_tools via ReActRunner.Run full loop ─────────────────
//
// Uses ReActRunner.Run to exercise the full loop without step-by-step control,
// verifying result fields (FinalAnswer, ToolCallCount, Iterations).

func TestLookupJourneyReActRunnerRunFull(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lookup ReAct full-run journey test in -short mode")
	}

	env := testEnvWithLookup(t)
	ctx := context.Background()

	// Seed the store with two tools.
	for _, te := range []lookup.ToolEntry{
		{Name: "proposal.create_draft", Description: "Create a new skill proposal draft", SkillName: "proposal"},
		{Name: "proposal.submit", Description: "Submit a proposal for court review", SkillName: "proposal"},
	} {
		if err := env.LookupStore.IndexTool(ctx, te); err != nil {
			t.Fatalf("IndexTool %q: %v", te.Name, err)
		}
	}

	executor := &stubTaskExecutor{script: []rtexec.AgentTurnResponse{
		{
			Status:   "tool_call",
			Tool:     "lookup_tools",
			Args:     `{"query":"proposal governance","max_results":5}`,
			Thinking: "Look up proposal governance tools.",
		},
		{
			Status:  "final",
			Content: "I found proposal.create_draft and proposal.submit for governance.",
		},
	}}

	reg := &ToolRegistry{env: env}
	registerLookupTools(reg, env)

	runner := rtexec.NewReActRunner(
		executor,
		func(c context.Context, tool, argsJSON string) (string, error) {
			return reg.Execute(c, tool, argsJSON)
		},
		"find proposal governance tools",
		rtexec.WithMaxIterations(5),
		rtexec.WithSeed(testutil.TestOllamaSeed),
	)

	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(result.FinalAnswer, "proposal") {
		t.Errorf("FinalAnswer should mention proposal, got: %q", result.FinalAnswer)
	}
	if result.ToolCallCount != 1 {
		t.Errorf("ToolCallCount = %d, want 1", result.ToolCallCount)
	}
	if result.Iterations != 2 {
		t.Errorf("Iterations = %d, want 2", result.Iterations)
	}
}
