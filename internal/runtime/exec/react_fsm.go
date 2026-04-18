// Package exec — ReAct finite-state machine
//
// ReActRunner encapsulates the ReAct (Reason + Act) loop as an explicit
// finite-state machine.  It replaces the ad-hoc for-loop pattern used in
// driveReActLoop (tests) and makeChatMessageHandler (production) with a
// clearly-named State type and a Step-by-step API that makes it easy for
// tests to advance exactly one phase at a time, inject mocks, and assert
// on state transitions.
//
// Production code continues to use makeChatMessageHandler unchanged.
// Tests compiled with the "inprocesstest" tag may use ReActRunner directly.
//
// State machine:
//
//	 ┌─────────────────────────────────────────────────────┐
//	 │                                                     ▼
//	Start → StateThinking → StateActing → StateObserving ──┘
//	                    └───────────────────────────────────→ StateFinalizing
//	                                                           (terminal)
//
// StateThinking  — an LLM call (ExecuteTurn) is in progress.
// StateActing    — a tool has been identified and is about to be executed.
// StateObserving — the tool result has been received and is being appended
//
//	to the conversation history.
//
// StateFinalizing — the LLM returned status=="final"; the loop terminates.
//
// The runner also terminates with an error when the iteration cap is reached
// (termination state remains the last valid state, err is non-nil).
package exec

import (
	"context"
	"errors"
	"fmt"
)

// State represents a single phase in the ReAct loop.
type State int

const (
	// StateThinking is the initial state and the state entered at the start
	// of each new LLM call.  The runner calls TaskExecutor.ExecuteTurn here.
	StateThinking State = iota

	// StateActing is entered when the LLM requests a tool call.  The runner
	// invokes the registered ToolExecutorFunc here.
	StateActing

	// StateObserving is entered after a tool returns its result.  The runner
	// appends the tool-call and tool-result messages to the history and
	// transitions back to StateThinking for the next iteration.
	StateObserving

	// StateFinalizing is the terminal success state.  The LLM returned
	// status=="final" and the loop ends.
	StateFinalizing
)

// String returns a human-readable name for the state.
func (s State) String() string {
	switch s {
	case StateThinking:
		return "Thinking"
	case StateActing:
		return "Acting"
	case StateObserving:
		return "Observing"
	case StateFinalizing:
		return "Finalizing"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// ToolExecutorFunc is a function that executes a named tool and returns its
// result string.  An error is returned on failure; the runner converts it to
// an error message that is fed back to the LLM.
type ToolExecutorFunc func(ctx context.Context, tool, argsJSON string) (string, error)

// StateTransition records a single state change for observability and testing.
type StateTransition struct {
	// From is the state being left.
	From State
	// To is the state being entered.
	To State
	// Tool is non-empty when transitioning into StateActing or StateObserving.
	Tool string
}

// StepResult is the outcome of one call to ReActRunner.Step.
type StepResult struct {
	// State is the state the runner is now in after the step.
	State State

	// FinalAnswer is populated when State == StateFinalizing.
	FinalAnswer string

	// ToolCalled is the qualified tool name when State is StateActing or
	// StateObserving.
	ToolCalled string

	// ToolArgs is the raw JSON arguments when State == StateActing.
	ToolArgs string

	// Thinking is the LLM's internal reasoning text for the current step, if any.
	Thinking string

	// ToolErr is non-nil when State == StateObserving and the tool execution
	// failed.  The error has already been converted into an observation appended
	// to the conversation history so the LLM can recover; it is exposed here for
	// test assertions.
	ToolErr error

	// Err is non-nil when the step encountered an unrecoverable error
	// (executor failure, unexpected status, or iteration cap exceeded).
	Err error
}

// RunResult is the outcome of a complete ReActRunner.Run call.
type RunResult struct {
	// FinalAnswer is the LLM's final content.
	FinalAnswer string

	// Transitions is the ordered list of all state changes that occurred.
	Transitions []StateTransition

	// Iterations is the number of LLM calls made before the final answer.
	Iterations int

	// ToolCallCount is the number of tool calls executed.
	ToolCallCount int
}

// ReActRunner drives the ReAct loop as an explicit finite-state machine.
//
// Create one with NewReActRunner for each task.  Call Step() to advance one
// phase at a time, or Run() to drive the loop to completion automatically.
//
// ReActRunner is NOT safe for concurrent use — callers must serialise access.
type ReActRunner struct {
	executor      TaskExecutor
	toolExec      ToolExecutorFunc
	maxIterations int
	onTransition  func(StateTransition) // optional; nil is fine
	model         string
	seed          int64

	// mutable state
	state       State
	messages    []AgentMessage
	iterations  int
	toolCalls   int
	transitions []StateTransition

	// pending tool call between StateActing → StateObserving
	pendingTool string
	pendingArgs string
}

// ReActRunnerOption is a functional option for NewReActRunner.
type ReActRunnerOption func(*ReActRunner)

// WithMaxIterations sets the maximum number of LLM calls (default: 10).
func WithMaxIterations(n int) ReActRunnerOption {
	return func(r *ReActRunner) { r.maxIterations = n }
}

// WithModel overrides the model name forwarded to the executor.
func WithModel(model string) ReActRunnerOption {
	return func(r *ReActRunner) { r.model = model }
}

// WithSeed sets the determinism seed forwarded to the executor.
func WithSeed(seed int64) ReActRunnerOption {
	return func(r *ReActRunner) { r.seed = seed }
}

// WithOnTransition registers a callback that is called on every state
// transition.  Useful for test assertions; nil is a no-op.
func WithOnTransition(fn func(StateTransition)) ReActRunnerOption {
	return func(r *ReActRunner) { r.onTransition = fn }
}

// NewReActRunner creates a ReActRunner for a single agent task.
//
//   - executor: the TaskExecutor to call for each LLM inference step.
//   - toolExec: the function to call when the LLM requests a tool.
//   - userInput: the initial user message.
//   - opts: optional configuration.
func NewReActRunner(
	executor TaskExecutor,
	toolExec ToolExecutorFunc,
	userInput string,
	opts ...ReActRunnerOption,
) *ReActRunner {
	r := &ReActRunner{
		executor:      executor,
		toolExec:      toolExec,
		maxIterations: 10,
		state:         StateThinking,
		messages: []AgentMessage{
			{Role: "user", Content: userInput},
		},
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// State returns the current state of the runner.
func (r *ReActRunner) State() State { return r.state }

// Iterations returns the number of completed LLM calls so far.
func (r *ReActRunner) Iterations() int { return r.iterations }

// ToolCallCount returns the number of tool calls executed so far.
func (r *ReActRunner) ToolCallCount() int { return r.toolCalls }

// IsDone reports whether the runner has reached a terminal state
// (StateFinalizing) or encountered an error.
func (r *ReActRunner) IsDone() bool { return r.state == StateFinalizing }

// Step advances the state machine by one phase and returns the result.
//
// Call Step() in a loop until StepResult.State == StateFinalizing or
// StepResult.Err != nil.
func (r *ReActRunner) Step(ctx context.Context) StepResult {
	switch r.state {
	case StateThinking:
		return r.stepThinking(ctx)
	case StateActing:
		return r.stepActing(ctx)
	case StateObserving:
		return r.stepObserving()
	case StateFinalizing:
		return StepResult{State: StateFinalizing, Err: errors.New("react runner: Step called on terminal state")}
	default:
		return StepResult{Err: fmt.Errorf("react runner: unknown state %v", r.state)}
	}
}

// stepThinking calls the executor for one LLM inference step.
func (r *ReActRunner) stepThinking(ctx context.Context) StepResult {
	if r.iterations >= r.maxIterations {
		return StepResult{
			State: r.state,
			Err:   fmt.Errorf("react runner: reached iteration limit (%d) without a final answer", r.maxIterations),
		}
	}

	resp, err := r.executor.ExecuteTurn(ctx, AgentTurnRequest{
		Messages:    r.messages,
		Model:       r.model,
		Temperature: 0,
		Seed:        r.seed,
	})
	r.iterations++

	if err != nil {
		return StepResult{State: r.state, Err: fmt.Errorf("react runner: executor error: %w", err)}
	}

	thinking := resp.Thinking

	switch resp.Status {
	case "final":
		r.transition(StateThinking, StateFinalizing, "")
		return StepResult{State: StateFinalizing, FinalAnswer: resp.Content, Thinking: thinking}

	case "tool_call":
		r.pendingTool = resp.Tool
		r.pendingArgs = resp.Args
		r.transition(StateThinking, StateActing, resp.Tool)
		return StepResult{
			State:      StateActing,
			ToolCalled: resp.Tool,
			ToolArgs:   resp.Args,
			Thinking:   thinking,
		}

	default:
		return StepResult{
			State: r.state,
			Err:   fmt.Errorf("react runner: unexpected agent status %q", resp.Status),
		}
	}
}

// stepActing executes the pending tool call.
func (r *ReActRunner) stepActing(ctx context.Context) StepResult {
	tool := r.pendingTool
	args := r.pendingArgs

	toolResult, toolErr := r.toolExec(ctx, tool, args)
	r.toolCalls++

	if toolErr != nil {
		toolResult = fmt.Sprintf("Error executing %s: %v", tool, toolErr)
	}

	// Append the tool-call turn and the observation to the history.
	toolCallContent := fmt.Sprintf("```tool-call\n{\"name\": %q, \"args\": %s}\n```", tool, args)
	r.messages = append(r.messages,
		AgentMessage{Role: "assistant", Content: toolCallContent},
		AgentMessage{Role: "tool", Name: tool, Content: toolResult},
	)

	r.transition(StateActing, StateObserving, tool)
	return StepResult{State: StateObserving, ToolCalled: tool, ToolErr: toolErr}
}

// stepObserving transitions back to Thinking so the LLM can reason about the
// tool result.
func (r *ReActRunner) stepObserving() StepResult {
	r.transition(StateObserving, StateThinking, "")
	return StepResult{State: StateThinking}
}

// transition records and fires the optional callback for a state change.
func (r *ReActRunner) transition(from, to State, tool string) {
	r.state = to
	t := StateTransition{From: from, To: to, Tool: tool}
	r.transitions = append(r.transitions, t)
	if r.onTransition != nil {
		r.onTransition(t)
	}
}

// Run drives the ReAct loop to completion and returns a RunResult.
//
// It calls Step() in a loop until StateFinalizing is reached or an error
// occurs.  This is the convenience method for callers that do not need
// per-phase control.
func (r *ReActRunner) Run(ctx context.Context) (RunResult, error) {
	for {
		result := r.Step(ctx)
		if result.Err != nil {
			return RunResult{}, result.Err
		}
		if result.State == StateFinalizing {
			return RunResult{
				FinalAnswer:   result.FinalAnswer,
				Transitions:   r.transitions,
				Iterations:    r.iterations,
				ToolCallCount: r.toolCalls,
			}, nil
		}
	}
}
