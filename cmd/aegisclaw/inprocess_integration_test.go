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
	"fmt"
	"os"
	"strings"
	"testing"

	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/proposal"
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
// registry, starting with the user's input.  It returns a ReActTrace and the
// final answer string, or an error if the loop hits the iteration limit without
// a final answer.
//
// This is a lightweight in-process analogue of makeChatMessageHandler — it
// exists only for tests and is compiled only under the inprocesstest build tag.
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

	msgs := []rtexec.AgentMessage{
		{Role: "user", Content: input},
	}

	for i := 0; i < maxIterations; i++ {
		resp, err := executor.ExecuteTurn(ctx, rtexec.AgentTurnRequest{
			Messages: msgs,
		})
		if err != nil {
			return "", fmt.Errorf("executor error at iteration %d: %w", i+1, err)
		}

		if thought := strings.TrimSpace(resp.Thinking); thought != "" {
			rec.recordThought(thought)
		}

		switch resp.Status {
		case "final":
			return resp.Content, nil

		case "tool_call":
			rec.recordToolCall(resp.Tool, resp.Args)

			toolResult, toolErr := toolRegistry.Execute(ctx, resp.Tool, resp.Args)
			success := toolErr == nil
			rec.recordToolResult(resp.Tool, success)

			if toolErr != nil {
				toolResult = fmt.Sprintf("Error executing %s: %v", resp.Tool, toolErr)
			}

			toolCallContent := formatToolCallBlock(resp.Tool, resp.Args)
			msgs = append(msgs,
				rtexec.AgentMessage{Role: "assistant", Content: toolCallContent},
				rtexec.AgentMessage{Role: "tool", Name: resp.Tool, Content: toolResult},
			)

		default:
			return "", fmt.Errorf("unexpected agent status: %q", resp.Status)
		}
	}

	return "", fmt.Errorf("reached iteration limit (%d) without a final answer", maxIterations)
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
		return handleProposalCreateDraft(env, args)
	})
	reg.Register("proposal.list_drafts", "List all draft proposals", func(ctx context.Context, args string) (string, error) {
		return handleProposalListDrafts(env)
	})
	reg.Register("proposal.get_draft", "Get details of a proposal draft", func(ctx context.Context, args string) (string, error) {
		return handleProposalGetDraft(env, args)
	})
	reg.Register("proposal.submit", "Submit a proposal for Court review", func(ctx context.Context, args string) (string, error) {
		return handleProposalSubmit(env, nil, ctx, args)
	})
	reg.Register("proposal.status", "Get the current status of a proposal", func(ctx context.Context, args string) (string, error) {
		return handleProposalStatus(env, args)
	})
	return reg
}

// filterEventsByType returns all trace events of the given type.
func filterEventsByType(events []TraceEvent, typ TraceEventType) []TraceEvent {
	var out []TraceEvent
	for _, e := range events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}
