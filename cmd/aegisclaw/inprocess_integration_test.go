//go:build inprocesstest
// +build inprocesstest

// inprocess_integration_test.go — Integration tests that use InProcessTaskExecutor
// to drive the agent ReAct loop without a Firecracker microVM.
//
// # SECURITY WARNING
//
// These tests use InProcessTaskExecutor which has ZERO sandbox isolation.
// They MUST ONLY be run with the "inprocesstest" build tag AND the environment
// variable AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only set.
//
// Normal "go test ./..." does NOT compile or run these tests.
//
// To run:
//
//	AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only \
//	  go test ./cmd/aegisclaw -tags=inprocesstest -run 'Integration|InProcess' -v
//
// Or via Makefile:
//
//	make test-inprocess
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/proposal"
	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/testutil"
)

// skipUnlessInProcessMode skips the test unless the safety env var is set.
// This provides a second layer of protection beyond the build tag.
func skipUnlessInProcessMode(t *testing.T) {
	t.Helper()
	if os.Getenv(rtexec.InProcessEnvVar) != rtexec.InProcessEnvValue {
		t.Skipf("skipping in-process test: set %s=%s to enable",
			rtexec.InProcessEnvVar, rtexec.InProcessEnvValue)
	}
}

// stubAgentFn returns a deterministic AgentFunc that drives a scripted ReAct
// sequence.  Each call pops one response from the script; when the script is
// exhausted it returns a final "no further action" answer.
func stubAgentFn(script []rtexec.AgentTurnResponse) rtexec.AgentFunc {
	idx := 0
	return func(_ context.Context, _ rtexec.AgentTurnRequest) (rtexec.AgentTurnResponse, error) {
		if idx < len(script) {
			resp := script[idx]
			idx++
			return resp, nil
		}
		return rtexec.AgentTurnResponse{
			Status:  "final",
			Content: "All tasks completed.",
		}, nil
	}
}

// driveReActLoop drives the ReAct loop using the given TaskExecutor and tool
// registry, starting with the user's input.  It returns the final answer
// string, or an error if the loop hits the iteration limit without one.
//
// Internally this delegates to ReActRunner — the explicit finite-state machine
// in internal/runtime/exec — while feeding the traceRecorder so that existing
// test assertions on trace events continue to work unchanged.
//
// This helper is compiled only under the inprocesstest build tag.
func driveReActLoop(
	ctx context.Context,
	executor rtexec.TaskExecutor,
	toolRegistry *ToolRegistry,
	input string,
	maxIterations int,
	rec *traceRecorder,
) (string, error) {
	if maxIterations <= 0 {
		maxIterations = 10
	}

	// Wrap the registry's Execute method as the ToolExecutorFunc expected by
	// ReActRunner.
	toolExec := func(c context.Context, tool, argsJSON string) (string, error) {
		return toolRegistry.Execute(c, tool, argsJSON)
	}

	// pendingThinking holds the Thinking text from the Thinking→Acting
	// transition so it can be recorded before the tool-call event.
	var pendingThinking string

	runner := rtexec.NewReActRunner(
		executor,
		toolExec,
		input,
		rtexec.WithMaxIterations(maxIterations),
		rtexec.WithSeed(testutil.TestOllamaSeed),
		rtexec.WithOnTransition(func(tr rtexec.StateTransition) {
			switch tr.To {
			case rtexec.StateActing:
				// pendingThinking was set in the Thinking step; flush it now.
				if t := strings.TrimSpace(pendingThinking); t != "" {
					rec.recordThought(t)
					pendingThinking = ""
				}
			}
		}),
	)

	for {
		result := runner.Step(ctx)
		if result.Err != nil {
			return "", result.Err
		}

		switch result.State {
		case rtexec.StateThinking:
			// Thinking text is produced by the Thinking step but we delay
			// recording it until we know whether the LLM will call a tool
			// (handled above in OnTransition) or emit a final answer.
			if t := strings.TrimSpace(result.Thinking); t != "" {
				pendingThinking = t
			}

		case rtexec.StateActing:
			// Tool call is about to be executed; record the call intent.
			rec.recordToolCall(result.ToolCalled, result.ToolArgs)
			// Carry Thinking text from this step (produced by the prior
			// Thinking step which may have been stored in pendingThinking).
			if t := strings.TrimSpace(result.Thinking); t != "" {
				pendingThinking = t
			}

		case rtexec.StateObserving:
			// Tool has executed; record the result.
			rec.recordToolResult(result.ToolCalled, result.ToolErr == nil)

		case rtexec.StateFinalizing:
			// Flush any remaining thinking text before recording the answer.
			if t := strings.TrimSpace(pendingThinking); t != "" {
				rec.recordThought(t)
				pendingThinking = ""
			}
			if t := strings.TrimSpace(result.Thinking); t != "" {
				rec.recordThought(t)
			}
			return result.FinalAnswer, nil
		}
	}
}

// ─── In-Process Integration Test 1: Simple tool call ─────────────────────────

func TestInProcessIntegration_SimpleToolCall(t *testing.T) {
	skipUnlessInProcessMode(t)
	if testing.Short() {
		t.Skip("skipping in-process test in -short mode")
	}

	env := testEnv(t)
	ctx := context.Background()

	// Script: one tool call (create_draft), then a final answer.
	createArgs := `{"title":"InProcess Skill","description":"Created in-process","skill_name":"inprocess-skill","tools":[{"name":"run","description":"runs the skill"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	script := []rtexec.AgentTurnResponse{
		{
			Status: "tool_call",
			Tool:   "proposal.create_draft",
			Args:   createArgs,
		},
		{
			Status:  "final",
			Content: "I have created the proposal for the in-process skill.",
		},
	}

	executor := rtexec.NewInProcessExecutor(stubAgentFn(script))

	reg := buildMinimalToolRegistry(env)
	rec := newTraceRecorder("inprocess-simple-tool-call", "create inprocess-skill proposal")

	answer, err := driveReActLoop(ctx, executor, reg, "create inprocess-skill proposal", 10, rec)
	if err != nil {
		t.Fatalf("driveReActLoop: %v", err)
	}

	// Assertions on final answer.
	if !strings.Contains(answer, "in-process skill") && !strings.Contains(answer, "created") {
		t.Errorf("unexpected final answer: %q", answer)
	}

	// Assertions on trace.
	trace := rec.finalize(answer)
	if trace.ToolCallCount != 1 {
		t.Errorf("expected 1 tool call, got %d", trace.ToolCallCount)
	}

	// Assertions on proposal store.
	proposals, err := env.ProposalStore.List()
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	found := false
	for _, p := range proposals {
		if p.TargetSkill == "inprocess-skill" {
			found = true
			if p.Status != proposal.StatusDraft {
				t.Errorf("expected draft, got %s", p.Status)
			}
		}
	}
	if !found {
		t.Error("proposal for 'inprocess-skill' not found in store")
	}
}

// ─── In-Process Integration Test 2: Multi-turn with tool failure/recovery ─────

func TestInProcessIntegration_ToolFailureRecovery(t *testing.T) {
	skipUnlessInProcessMode(t)
	if testing.Short() {
		t.Skip("skipping in-process test in -short mode")
	}

	env := testEnv(t)
	ctx := context.Background()

	// Script:
	//   1. Call an unknown tool (will fail) → agent should see the error
	//   2. Recover and call the correct tool
	//   3. Final answer
	goodArgs := `{"title":"Recovered","description":"recovered after failure","skill_name":"recovered","tools":[{"name":"t","description":"d"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`
	script := []rtexec.AgentTurnResponse{
		{
			Status: "tool_call",
			Tool:   "proposal.bad_tool", // unknown → will error
			Args:   `{}`,
		},
		{
			Status: "tool_call",
			Tool:   "proposal.create_draft", // correct tool after recovery
			Args:   goodArgs,
		},
		{
			Status:  "final",
			Content: "Recovery successful. The proposal has been created.",
		},
	}

	executor := rtexec.NewInProcessExecutor(stubAgentFn(script))
	reg := buildMinimalToolRegistry(env)
	rec := newTraceRecorder("inprocess-tool-failure-recovery", "attempt recovery after bad tool")

	answer, err := driveReActLoop(ctx, executor, reg, "create recovered proposal", 10, rec)
	if err != nil {
		t.Fatalf("driveReActLoop: %v", err)
	}

	if !strings.Contains(strings.ToLower(answer), "recovery") && !strings.Contains(strings.ToLower(answer), "created") {
		t.Errorf("unexpected final answer: %q", answer)
	}

	trace := rec.finalize(answer)

	// 2 tool calls were made (one failed, one succeeded).
	if trace.ToolCallCount != 2 {
		t.Errorf("expected 2 tool calls (1 fail + 1 success), got %d", trace.ToolCallCount)
	}

	// Verify trace has a failed tool result followed by a successful one.
	toolResults := filterEventsByType(trace.Events, TraceEventToolResult)
	if len(toolResults) < 2 {
		t.Fatalf("expected at least 2 tool_result events, got %d", len(toolResults))
	}
	if toolResults[0].Success == nil || *toolResults[0].Success {
		t.Errorf("first tool result should have failed")
	}
	if toolResults[1].Success == nil || !*toolResults[1].Success {
		t.Errorf("second tool result should have succeeded")
	}
}

// ─── In-Process Integration Test 3: Full create → submit journey ──────────────

func TestInProcessIntegration_FullCreateSubmitJourney(t *testing.T) {
	skipUnlessInProcessMode(t)
	if testing.Short() {
		t.Skip("skipping in-process test in -short mode")
	}

	env := testEnv(t)
	ctx := context.Background()
	rec := newTraceRecorder("inprocess-full-journey", "create and submit journey-skill")

	createArgs := `{"title":"Journey Skill","description":"Full journey test","skill_name":"journey-skill","tools":[{"name":"go","description":"runs the journey"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`

	// First pass: create_draft.  The actual proposal ID isn't known at script
	// creation time, so the second call (submit) uses a sentinel that we
	// replace with the real ID once the store contains the proposal.
	callCount := 0
	var createdID string

	agentFn := func(_ context.Context, req rtexec.AgentTurnRequest) (rtexec.AgentTurnResponse, error) {
		callCount++
		switch callCount {
		case 1:
			return rtexec.AgentTurnResponse{
				Status:   "tool_call",
				Tool:     "proposal.create_draft",
				Args:     createArgs,
				Thinking: "I will create the proposal first.",
			}, nil
		case 2:
			// By now the proposal has been created; find its ID.
			proposals, _ := env.ProposalStore.List()
			for _, p := range proposals {
				if p.TargetSkill == "journey-skill" {
					createdID = p.ID
					break
				}
			}
			if createdID == "" {
				return rtexec.AgentTurnResponse{}, fmt.Errorf("journey-skill proposal not found in store")
			}
			return rtexec.AgentTurnResponse{
				Status:   "tool_call",
				Tool:     "proposal.submit",
				Args:     fmt.Sprintf(`{"id":%q}`, createdID),
				Thinking: "Now I will submit the proposal.",
			}, nil
		default:
			return rtexec.AgentTurnResponse{
				Status:  "final",
				Content: fmt.Sprintf("The journey-skill proposal %s has been created and submitted for Court review.", createdID),
			}, nil
		}
	}

	executor := rtexec.NewInProcessExecutor(agentFn)
	reg := buildMinimalToolRegistry(env)

	answer, err := driveReActLoop(ctx, executor, reg, "create and submit journey-skill proposal", 10, rec)
	if err != nil {
		t.Fatalf("driveReActLoop: %v", err)
	}

	// Assertions on final answer.
	if !strings.Contains(answer, "journey-skill") {
		t.Errorf("answer should mention 'journey-skill', got: %q", answer)
	}

	trace := rec.finalize(answer)
	if trace.ToolCallCount != 2 {
		t.Errorf("expected 2 tool calls (create+submit), got %d", trace.ToolCallCount)
	}

	// Assertions on proposal store.
	if createdID == "" {
		t.Fatal("createdID was not set")
	}
	p, err := env.ProposalStore.Get(createdID)
	if err != nil {
		t.Fatalf("get proposal %s: %v", createdID, err)
	}
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("expected submitted, got %s", p.Status)
	}

	// Assertions on thought events.
	if trace.Iterations < 2 {
		t.Errorf("expected at least 2 iterations, got %d", trace.Iterations)
	}

	// Verify tool call sequence in trace.
	toolCalls := filterEventsByType(trace.Events, TraceEventToolCalled)
	if len(toolCalls) < 2 {
		t.Fatalf("expected at least 2 tool_called events, got %d", len(toolCalls))
	}
	if toolCalls[0].Tool != "proposal.create_draft" {
		t.Errorf("first tool call should be create_draft, got %q", toolCalls[0].Tool)
	}
	if toolCalls[1].Tool != "proposal.submit" {
		t.Errorf("second tool call should be submit, got %q", toolCalls[1].Tool)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// buildMinimalToolRegistry creates a ToolRegistry with the proposal tools
// wired to the test env.  This mirrors how the daemon registers tools at
// startup, but only includes what's needed for in-process tests.
func buildMinimalToolRegistry(env *runtimeEnv) *ToolRegistry {
	reg := &ToolRegistry{env: env}
	reg.Register("proposal.create_draft", "Create a new skill proposal draft", func(ctx context.Context, args string) (string, error) {
		return handleProposalCreateDraft(env, ctx, args)
	})
	reg.Register("proposal.list_drafts", "List all draft proposals", func(ctx context.Context, args string) (string, error) {
		return handleProposalListDrafts(env, ctx)
	})
	reg.Register("proposal.get_draft", "Get details of a proposal draft", func(ctx context.Context, args string) (string, error) {
		return handleProposalGetDraft(env, ctx, args)
	})
	reg.Register("proposal.submit", "Submit a proposal for Court review", func(ctx context.Context, args string) (string, error) {
		return handleProposalSubmit(env, nil, ctx, args)
	})
	reg.Register("proposal.status", "Get the current status of a proposal", func(ctx context.Context, args string) (string, error) {
		return handleProposalStatus(env, ctx, args)
	})
	return reg
}

// inprocessCassetteDir is the directory where Ollama cassettes for in-process
// integration tests are stored.
func inprocessCassetteDir(name string) string {
	return filepath.Join("testdata", "ollama-cassettes", name)
}

// writeInprocessCassette writes a cassette file for one agent turn.
// Used to set up fixtures for OllamaRecorder-based tests that run in replay
// mode (RECORD_OLLAMA is not set).
func writeInprocessCassette(t *testing.T, dir, name string, idx int, resp rtexec.AgentTurnResponse) {
	t.Helper()
	type cassetteTurn struct {
		Request  rtexec.AgentTurnRequest  `json:"request"`
		Response rtexec.AgentTurnResponse `json:"response"`
	}
	turn := cassetteTurn{
		Request: rtexec.AgentTurnRequest{
			Messages: []rtexec.AgentMessage{{Role: "user", Content: "test"}},
		},
		Response: resp,
	}
	data, err := json.MarshalIndent(turn, "", "  ")
	if err != nil {
		t.Fatalf("writeInprocessCassette marshal: %v", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%03d.json", name, idx))
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("writeInprocessCassette mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("writeInprocessCassette write: %v", err)
	}
}

// ─── In-Process Test 4: ReActRunner state transition assertions ───────────────

// TestInProcessIntegration_ReActRunnerStateTransitions drives the ReActRunner
// step-by-step and asserts on every state transition, demonstrating that the
// FSM is correct for a create-draft → submit sequence.
func TestInProcessIntegration_ReActRunnerStateTransitions(t *testing.T) {
	skipUnlessInProcessMode(t)
	if testing.Short() {
		t.Skip("skipping in-process test in -short mode")
	}

	env := testEnv(t)
	ctx := context.Background()

	createArgs := `{"title":"FSM Skill","description":"Tests the state machine","skill_name":"fsm-skill","tools":[{"name":"run","description":"runs it"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`

	callCount := 0
	var createdID string
	agentFn := func(_ context.Context, _ rtexec.AgentTurnRequest) (rtexec.AgentTurnResponse, error) {
		callCount++
		switch callCount {
		case 1:
			return rtexec.AgentTurnResponse{
				Status:   "tool_call",
				Tool:     "proposal.create_draft",
				Args:     createArgs,
				Thinking: "I will create the FSM skill proposal.",
			}, nil
		case 2:
			proposals, _ := env.ProposalStore.List()
			for _, p := range proposals {
				if p.TargetSkill == "fsm-skill" {
					createdID = p.ID
					break
				}
			}
			return rtexec.AgentTurnResponse{
				Status:   "tool_call",
				Tool:     "proposal.submit",
				Args:     fmt.Sprintf(`{"id":%q}`, createdID),
				Thinking: "Proposal created. Now submitting.",
			}, nil
		default:
			return rtexec.AgentTurnResponse{
				Status:  "final",
				Content: "fsm-skill proposal created and submitted.",
			}, nil
		}
	}

	executor := rtexec.NewInProcessExecutor(agentFn)
	reg := buildMinimalToolRegistry(env)

	var seenTransitions []rtexec.StateTransition

	runner := rtexec.NewReActRunner(
		executor,
		func(c context.Context, tool, argsJSON string) (string, error) {
			return reg.Execute(c, tool, argsJSON)
		},
		"create and submit fsm-skill",
		rtexec.WithOnTransition(func(tr rtexec.StateTransition) {
			seenTransitions = append(seenTransitions, tr)
		}),
	)

	// ── Step-by-step: Thinking → Acting ──────────────────────────────────────
	res1 := runner.Step(ctx)
	if res1.Err != nil {
		t.Fatalf("step1 (Thinking→Acting): %v", res1.Err)
	}
	if res1.State != rtexec.StateActing {
		t.Errorf("step1 state = %v, want Acting", res1.State)
	}
	if res1.ToolCalled != "proposal.create_draft" {
		t.Errorf("step1 ToolCalled = %q, want proposal.create_draft", res1.ToolCalled)
	}

	// ── Acting → Observing ────────────────────────────────────────────────────
	res2 := runner.Step(ctx)
	if res2.Err != nil {
		t.Fatalf("step2 (Acting→Observing): %v", res2.Err)
	}
	if res2.State != rtexec.StateObserving {
		t.Errorf("step2 state = %v, want Observing", res2.State)
	}
	if res2.ToolErr != nil {
		t.Errorf("step2 ToolErr should be nil (tool succeeded), got: %v", res2.ToolErr)
	}

	// ── Observing → Thinking ──────────────────────────────────────────────────
	res3 := runner.Step(ctx)
	if res3.Err != nil {
		t.Fatalf("step3 (Observing→Thinking): %v", res3.Err)
	}
	if res3.State != rtexec.StateThinking {
		t.Errorf("step3 state = %v, want Thinking", res3.State)
	}

	// ── Thinking → Acting (submit) ────────────────────────────────────────────
	res4 := runner.Step(ctx)
	if res4.Err != nil {
		t.Fatalf("step4 (Thinking→Acting/submit): %v", res4.Err)
	}
	if res4.State != rtexec.StateActing {
		t.Errorf("step4 state = %v, want Acting", res4.State)
	}
	if res4.ToolCalled != "proposal.submit" {
		t.Errorf("step4 ToolCalled = %q, want proposal.submit", res4.ToolCalled)
	}

	// ── Acting → Observing ────────────────────────────────────────────────────
	res5 := runner.Step(ctx)
	if res5.Err != nil {
		t.Fatalf("step5 (Acting→Observing/submit): %v", res5.Err)
	}
	if res5.ToolErr != nil {
		t.Errorf("step5 ToolErr should be nil (tool succeeded), got: %v", res5.ToolErr)
	}

	// ── Observing → Thinking ──────────────────────────────────────────────────
	res6 := runner.Step(ctx) // → Thinking
	if res6.Err != nil {
		t.Fatalf("step6 (Observing→Thinking): %v", res6.Err)
	}
	if res6.State != rtexec.StateThinking {
		t.Errorf("step6 state = %v, want Thinking", res6.State)
	}

	// ── Thinking → Finalizing ─────────────────────────────────────────────────
	res7 := runner.Step(ctx)
	if res7.Err != nil {
		t.Fatalf("step7 (Thinking→Finalizing): %v", res7.Err)
	}
	if res7.State != rtexec.StateFinalizing {
		t.Errorf("step7 state = %v, want Finalizing", res7.State)
	}
	if !strings.Contains(res7.FinalAnswer, "fsm-skill") {
		t.Errorf("FinalAnswer should mention fsm-skill, got: %q", res7.FinalAnswer)
	}

	// ── Assert complete transition sequence ───────────────────────────────────
	wantTransitions := []struct{ from, to rtexec.State }{
		{rtexec.StateThinking, rtexec.StateActing},
		{rtexec.StateActing, rtexec.StateObserving},
		{rtexec.StateObserving, rtexec.StateThinking},
		{rtexec.StateThinking, rtexec.StateActing},
		{rtexec.StateActing, rtexec.StateObserving},
		{rtexec.StateObserving, rtexec.StateThinking},
		{rtexec.StateThinking, rtexec.StateFinalizing},
	}
	if len(seenTransitions) != len(wantTransitions) {
		t.Fatalf("transitions = %d, want %d: %v", len(seenTransitions), len(wantTransitions), seenTransitions)
	}
	for i, wt := range wantTransitions {
		if seenTransitions[i].From != wt.from || seenTransitions[i].To != wt.to {
			t.Errorf("transition[%d] = %v→%v, want %v→%v",
				i, seenTransitions[i].From, seenTransitions[i].To, wt.from, wt.to)
		}
	}

	// ── Verify proposal store state ───────────────────────────────────────────
	if createdID == "" {
		t.Fatal("createdID was never set")
	}
	p, err := env.ProposalStore.Get(createdID)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if p.Status != proposal.StatusSubmitted {
		t.Errorf("expected submitted, got %s", p.Status)
	}
	if runner.ToolCallCount() != 2 {
		t.Errorf("ToolCallCount = %d, want 2", runner.ToolCallCount())
	}
}

// ─── In-Process Test 5: OllamaRecorder replay ────────────────────────────────

// TestInProcessIntegration_OllamaRecorder_Replay demonstrates using
// OllamaRecorder to replay pre-recorded Ollama responses for deterministic
// testing.  Cassette fixtures are written into a temp dir so the test can run
// without any real LLM or network access.
//
// To record new cassettes against a live Ollama server, set RECORD_OLLAMA=true
// and provide a real AgentFunc.
func TestInProcessIntegration_OllamaRecorder_Replay(t *testing.T) {
	skipUnlessInProcessMode(t)
	if testing.Short() {
		t.Skip("skipping in-process test in -short mode")
	}

	env := testEnv(t)
	ctx := context.Background()

	cassetteDir := t.TempDir()
	cassetteDir = filepath.Join(cassetteDir, "ollama-cassettes", "recorder-replay")
	cassetteName := "recorder-replay"

	createArgs := `{"title":"Recorder Skill","description":"Recorded responses","skill_name":"recorder-skill","tools":[{"name":"act","description":"acts"}],"data_sensitivity":1,"network_exposure":1,"privilege_level":1}`

	// Pre-seed the cassette fixtures so the test runs fully offline.
	writeInprocessCassette(t, cassetteDir, cassetteName, 0, rtexec.AgentTurnResponse{
		Status:   "tool_call",
		Tool:     "proposal.create_draft",
		Args:     createArgs,
		Thinking: "I will create the recorder-skill proposal from the cassette.",
	})
	writeInprocessCassette(t, cassetteDir, cassetteName, 1, rtexec.AgentTurnResponse{
		Status:  "final",
		Content: "recorder-skill proposal has been created from the cassette.",
	})

	// RECORD_OLLAMA is not set → recorder replays cassette fixtures.
	t.Setenv("RECORD_OLLAMA", "")

	recorder := rtexec.NewOllamaRecorder(cassetteDir, cassetteName, nil)
	executor := rtexec.NewInProcessExecutor(recorder.AgentFunc())
	reg := buildMinimalToolRegistry(env)

	var transitions []rtexec.StateTransition
	runner := rtexec.NewReActRunner(
		executor,
		func(c context.Context, tool, argsJSON string) (string, error) {
			return reg.Execute(c, tool, argsJSON)
		},
		"create recorder-skill via cassette",
		rtexec.WithOnTransition(func(tr rtexec.StateTransition) {
			transitions = append(transitions, tr)
		}),
	)

	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(result.FinalAnswer, "recorder-skill") {
		t.Errorf("FinalAnswer should mention recorder-skill, got: %q", result.FinalAnswer)
	}
	if result.ToolCallCount != 1 {
		t.Errorf("ToolCallCount = %d, want 1", result.ToolCallCount)
	}
	if result.Iterations != 2 {
		t.Errorf("Iterations = %d, want 2", result.Iterations)
	}

	// Verify proposal was created in the store.
	proposals, err := env.ProposalStore.List()
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	found := false
	for _, p := range proposals {
		if p.TargetSkill == "recorder-skill" {
			found = true
			if p.Status != proposal.StatusDraft {
				t.Errorf("expected draft, got %s", p.Status)
			}
		}
	}
	if !found {
		t.Error("recorder-skill proposal not found in store")
	}

	// Assert the full Thinking→Acting→Observing→Thinking→Finalizing sequence.
	wantTransitions := []struct{ from, to rtexec.State }{
		{rtexec.StateThinking, rtexec.StateActing},
		{rtexec.StateActing, rtexec.StateObserving},
		{rtexec.StateObserving, rtexec.StateThinking},
		{rtexec.StateThinking, rtexec.StateFinalizing},
	}
	if len(transitions) != len(wantTransitions) {
		t.Fatalf("transitions = %d, want %d: %v", len(transitions), len(wantTransitions), transitions)
	}
	for i, wt := range wantTransitions {
		if transitions[i].From != wt.from || transitions[i].To != wt.to {
			t.Errorf("transition[%d] = %v→%v, want %v→%v",
				i, transitions[i].From, transitions[i].To, wt.from, wt.to)
		}
	}
}
