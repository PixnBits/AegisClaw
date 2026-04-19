package exec_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/runtime/exec"
)

// ─── State.String ─────────────────────────────────────────────────────────────

func TestState_String(t *testing.T) {
	cases := []struct {
		state exec.State
		want  string
	}{
		{exec.StateThinking, "Thinking"},
		{exec.StateActing, "Acting"},
		{exec.StateObserving, "Observing"},
		{exec.StateFinalizing, "Finalizing"},
		{exec.State(99), "State(99)"},
	}
	for _, c := range cases {
		if got := c.state.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", int(c.state), got, c.want)
		}
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// scriptedExecutor returns deterministic AgentTurnResponses from a pre-loaded
// script.  When the script is exhausted it returns a final "done" answer.
func scriptedExecutor(script []exec.AgentTurnResponse) exec.TaskExecutor {
	idx := 0
	return &funcExecutor{fn: func(_ context.Context, _ exec.AgentTurnRequest) (exec.AgentTurnResponse, error) {
		if idx < len(script) {
			r := script[idx]
			idx++
			return r, nil
		}
		return exec.AgentTurnResponse{Status: "final", Content: "done"}, nil
	}}
}

// funcExecutor wraps an AgentFunc as a TaskExecutor.
type funcExecutor struct {
	fn func(context.Context, exec.AgentTurnRequest) (exec.AgentTurnResponse, error)
}

func (f *funcExecutor) ExecuteTurn(ctx context.Context, req exec.AgentTurnRequest) (exec.AgentTurnResponse, error) {
	return f.fn(ctx, req)
}

// noopToolExec always returns "tool-result" without error.
func noopToolExec(_ context.Context, _, _ string) (string, error) {
	return "tool-result", nil
}

// erroringToolExec always returns an error.
func erroringToolExec(_ context.Context, _, _ string) (string, error) {
	return "", errors.New("tool failure")
}

// collectTransitions returns an OnTransition callback that appends each
// transition to the provided slice.
func collectTransitions(out *[]exec.StateTransition) func(exec.StateTransition) {
	return func(t exec.StateTransition) {
		*out = append(*out, t)
	}
}

// ─── NewReActRunner ───────────────────────────────────────────────────────────

func TestNewReActRunner_InitialState(t *testing.T) {
	ex := scriptedExecutor(nil)
	r := exec.NewReActRunner(ex, noopToolExec, "hello")
	if r.State() != exec.StateThinking {
		t.Errorf("initial state = %v, want Thinking", r.State())
	}
	if r.IsDone() {
		t.Error("IsDone should be false before any steps")
	}
}

// ─── Direct final answer (Thinking → Finalizing) ──────────────────────────────

func TestReActRunner_DirectFinalAnswer(t *testing.T) {
	script := []exec.AgentTurnResponse{
		{Status: "final", Content: "42"},
	}
	var got []exec.StateTransition
	r := exec.NewReActRunner(
		scriptedExecutor(script),
		noopToolExec,
		"what is the answer",
		exec.WithOnTransition(collectTransitions(&got)),
	)

	result := r.Step(context.Background())
	if result.Err != nil {
		t.Fatalf("Step: unexpected error: %v", result.Err)
	}
	if result.State != exec.StateFinalizing {
		t.Errorf("state = %v, want Finalizing", result.State)
	}
	if result.FinalAnswer != "42" {
		t.Errorf("FinalAnswer = %q, want %q", result.FinalAnswer, "42")
	}
	if !r.IsDone() {
		t.Error("IsDone should be true after Finalizing")
	}

	// One transition: Thinking → Finalizing.
	if len(got) != 1 {
		t.Fatalf("transitions = %d, want 1", len(got))
	}
	if got[0].From != exec.StateThinking || got[0].To != exec.StateFinalizing {
		t.Errorf("transition = %v→%v, want Thinking→Finalizing", got[0].From, got[0].To)
	}
}

// ─── Single tool call (Thinking → Acting → Observing → Thinking → Finalizing) ─

func TestReActRunner_SingleToolCall(t *testing.T) {
	script := []exec.AgentTurnResponse{
		{Status: "tool_call", Tool: "proposal.create_draft", Args: `{"title":"T"}`},
		{Status: "final", Content: "created"},
	}
	var got []exec.StateTransition
	r := exec.NewReActRunner(
		scriptedExecutor(script),
		noopToolExec,
		"create draft",
		exec.WithOnTransition(collectTransitions(&got)),
	)

	// Step 1: Thinking → Acting
	res1 := r.Step(context.Background())
	if res1.Err != nil {
		t.Fatalf("step1: %v", res1.Err)
	}
	if res1.State != exec.StateActing {
		t.Errorf("step1 state = %v, want Acting", res1.State)
	}
	if res1.ToolCalled != "proposal.create_draft" {
		t.Errorf("ToolCalled = %q, want proposal.create_draft", res1.ToolCalled)
	}

	// Step 2: Acting → Observing
	res2 := r.Step(context.Background())
	if res2.Err != nil {
		t.Fatalf("step2: %v", res2.Err)
	}
	if res2.State != exec.StateObserving {
		t.Errorf("step2 state = %v, want Observing", res2.State)
	}

	// Step 3: Observing → Thinking
	res3 := r.Step(context.Background())
	if res3.Err != nil {
		t.Fatalf("step3: %v", res3.Err)
	}
	if res3.State != exec.StateThinking {
		t.Errorf("step3 state = %v, want Thinking", res3.State)
	}

	// Step 4: Thinking → Finalizing
	res4 := r.Step(context.Background())
	if res4.Err != nil {
		t.Fatalf("step4: %v", res4.Err)
	}
	if res4.State != exec.StateFinalizing {
		t.Errorf("step4 state = %v, want Finalizing", res4.State)
	}
	if res4.FinalAnswer != "created" {
		t.Errorf("FinalAnswer = %q, want created", res4.FinalAnswer)
	}

	// Verify transition sequence.
	wantTransitions := []struct{ from, to exec.State }{
		{exec.StateThinking, exec.StateActing},
		{exec.StateActing, exec.StateObserving},
		{exec.StateObserving, exec.StateThinking},
		{exec.StateThinking, exec.StateFinalizing},
	}
	if len(got) != len(wantTransitions) {
		t.Fatalf("transitions = %d, want %d", len(got), len(wantTransitions))
	}
	for i, wt := range wantTransitions {
		if got[i].From != wt.from || got[i].To != wt.to {
			t.Errorf("transition[%d] = %v→%v, want %v→%v",
				i, got[i].From, got[i].To, wt.from, wt.to)
		}
	}

	if r.ToolCallCount() != 1 {
		t.Errorf("ToolCallCount = %d, want 1", r.ToolCallCount())
	}
	if r.Iterations() != 2 {
		t.Errorf("Iterations = %d, want 2", r.Iterations())
	}
}

// ─── Run (full loop) ──────────────────────────────────────────────────────────

func TestReActRunner_Run_TwoToolCalls(t *testing.T) {
	script := []exec.AgentTurnResponse{
		{Status: "tool_call", Tool: "proposal.create_draft", Args: `{"title":"A"}`},
		{Status: "tool_call", Tool: "proposal.submit", Args: `{"id":"1"}`},
		{Status: "final", Content: "done"},
	}
	r := exec.NewReActRunner(
		scriptedExecutor(script),
		noopToolExec,
		"create and submit",
	)
	result, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalAnswer != "done" {
		t.Errorf("FinalAnswer = %q, want done", result.FinalAnswer)
	}
	if result.ToolCallCount != 2 {
		t.Errorf("ToolCallCount = %d, want 2", result.ToolCallCount)
	}
	if result.Iterations != 3 {
		t.Errorf("Iterations = %d, want 3", result.Iterations)
	}
}

// ─── Max iterations guard ─────────────────────────────────────────────────────

func TestReActRunner_MaxIterationsGuard(t *testing.T) {
	// Executor always requests a tool call — loop never terminates on its own.
	neverFinalExec := &funcExecutor{fn: func(_ context.Context, _ exec.AgentTurnRequest) (exec.AgentTurnResponse, error) {
		return exec.AgentTurnResponse{Status: "tool_call", Tool: "proposal.ping", Args: `{}`}, nil
	}}

	r := exec.NewReActRunner(
		neverFinalExec,
		noopToolExec,
		"loop forever",
		exec.WithMaxIterations(3),
	)
	_, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when max iterations reached, got nil")
	}
	if r.Iterations() > 3 {
		t.Errorf("iterations = %d, exceeded max of 3", r.Iterations())
	}
}

// ─── Executor error ───────────────────────────────────────────────────────────

func TestReActRunner_ExecutorError(t *testing.T) {
	errExec := &funcExecutor{fn: func(_ context.Context, _ exec.AgentTurnRequest) (exec.AgentTurnResponse, error) {
		return exec.AgentTurnResponse{}, errors.New("vsock connection refused")
	}}
	r := exec.NewReActRunner(errExec, noopToolExec, "hello")
	_, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "vsock connection refused") {
		t.Errorf("error should mention vsock: %v", err)
	}
}

// ─── Unexpected status ────────────────────────────────────────────────────────

func TestReActRunner_UnexpectedStatus(t *testing.T) {
	badExec := &funcExecutor{fn: func(_ context.Context, _ exec.AgentTurnRequest) (exec.AgentTurnResponse, error) {
		return exec.AgentTurnResponse{Status: "unknown_status"}, nil
	}}
	r := exec.NewReActRunner(badExec, noopToolExec, "hello")
	_, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for unexpected status, got nil")
	}
}

// ─── Tool failure ─────────────────────────────────────────────────────────────

// TestReActRunner_ToolFailure verifies that a tool error is fed back to the
// LLM as an error observation rather than causing Run to return an error.
func TestReActRunner_ToolFailure(t *testing.T) {
	script := []exec.AgentTurnResponse{
		{Status: "tool_call", Tool: "bad.tool", Args: `{}`},
		{Status: "final", Content: "recovered"},
	}
	r := exec.NewReActRunner(
		scriptedExecutor(script),
		erroringToolExec, // always errors
		"trigger failure",
	)
	result, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run should succeed even when the tool errors; got: %v", err)
	}
	if result.FinalAnswer != "recovered" {
		t.Errorf("FinalAnswer = %q, want recovered", result.FinalAnswer)
	}
	// The tool was called once (even though it errored — the error is an observation).
	if result.ToolCallCount != 1 {
		t.Errorf("ToolCallCount = %d, want 1", result.ToolCallCount)
	}
}

// ─── Step on terminal state ───────────────────────────────────────────────────

func TestReActRunner_StepOnTerminalState(t *testing.T) {
	script := []exec.AgentTurnResponse{{Status: "final", Content: "done"}}
	r := exec.NewReActRunner(scriptedExecutor(script), noopToolExec, "q")
	r.Step(context.Background()) // → Finalizing

	// Stepping again on the terminal state must return an error.
	res := r.Step(context.Background())
	if res.Err == nil {
		t.Fatal("expected error when stepping terminal state, got nil")
	}
}

// ─── Thinking text propagation ────────────────────────────────────────────────

func TestReActRunner_ThinkingPropagated(t *testing.T) {
	script := []exec.AgentTurnResponse{
		{Status: "final", Content: "answer", Thinking: "internal monologue"},
	}
	r := exec.NewReActRunner(scriptedExecutor(script), noopToolExec, "q")
	result := r.Step(context.Background())
	if result.Thinking != "internal monologue" {
		t.Errorf("Thinking = %q, want 'internal monologue'", result.Thinking)
	}
}

// ─── Transitions carry tool name ──────────────────────────────────────────────

func TestReActRunner_TransitionToolName(t *testing.T) {
	script := []exec.AgentTurnResponse{
		{Status: "tool_call", Tool: "my.tool", Args: `{}`},
		{Status: "final", Content: "ok"},
	}
	var got []exec.StateTransition
	r := exec.NewReActRunner(
		scriptedExecutor(script),
		noopToolExec,
		"use my.tool",
		exec.WithOnTransition(collectTransitions(&got)),
	)
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Find the Thinking → Acting transition.
	var actingTransition *exec.StateTransition
	for i := range got {
		if got[i].To == exec.StateActing {
			actingTransition = &got[i]
			break
		}
	}
	if actingTransition == nil {
		t.Fatal("no Thinking→Acting transition found")
	}
	if actingTransition.Tool != "my.tool" {
		t.Errorf("Acting transition Tool = %q, want my.tool", actingTransition.Tool)
	}
}

// ─── Run result transitions ───────────────────────────────────────────────────

func TestReActRunner_RunResult_Transitions(t *testing.T) {
	script := []exec.AgentTurnResponse{
		{Status: "final", Content: "immediate"},
	}
	r := exec.NewReActRunner(scriptedExecutor(script), noopToolExec, "q")
	result, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Only one transition: Thinking → Finalizing.
	if len(result.Transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(result.Transitions))
	}
	if result.Transitions[0].From != exec.StateThinking {
		t.Errorf("transition.From = %v, want Thinking", result.Transitions[0].From)
	}
	if result.Transitions[0].To != exec.StateFinalizing {
		t.Errorf("transition.To = %v, want Finalizing", result.Transitions[0].To)
	}
}

// ─── WithModel / WithSeed ─────────────────────────────────────────────────────

func TestReActRunner_WithModelAndSeed(t *testing.T) {
	var gotModel string
	var gotSeed int64
	captureExec := &funcExecutor{fn: func(_ context.Context, req exec.AgentTurnRequest) (exec.AgentTurnResponse, error) {
		gotModel = req.Model
		gotSeed = req.Seed
		return exec.AgentTurnResponse{Status: "final", Content: "ok"}, nil
	}}
	r := exec.NewReActRunner(
		captureExec,
		noopToolExec,
		"hello",
		exec.WithModel("llama3.2:3b"),
		exec.WithSeed(42),
	)
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotModel != "llama3.2:3b" {
		t.Errorf("model = %q, want llama3.2:3b", gotModel)
	}
	if gotSeed != 42 {
		t.Errorf("seed = %d, want 42", gotSeed)
	}
}

// ─── Idempotency: multiple tool calls accumulate correctly ────────────────────

func TestReActRunner_MultipleToolCallsAccumulate(t *testing.T) {
	const n = 4
	script := make([]exec.AgentTurnResponse, n+1)
	for i := 0; i < n; i++ {
		script[i] = exec.AgentTurnResponse{
			Status: "tool_call",
			Tool:   fmt.Sprintf("tool.%d", i),
			Args:   `{}`,
		}
	}
	script[n] = exec.AgentTurnResponse{Status: "final", Content: "done"}

	r := exec.NewReActRunner(scriptedExecutor(script), noopToolExec, "run many tools")
	result, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolCallCount != n {
		t.Errorf("ToolCallCount = %d, want %d", result.ToolCallCount, n)
	}
	if result.Iterations != n+1 {
		t.Errorf("Iterations = %d, want %d", result.Iterations, n+1)
	}
}
