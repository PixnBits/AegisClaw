# react_fsm_test.go

## Purpose
Comprehensive unit tests for the ReAct finite-state machine (`ReActRunner`) defined in `react_fsm.go`. Runs without a build-tag guard, covering all FSM transitions, option functions, and error paths.

## Key Types / Functions
- `scriptedExecutor` — returns a pre-loaded sequence of `AgentTurnResponse` values; falls back to a final "done" answer when the script is exhausted.
- `funcExecutor` — thin wrapper converting a closure into a `TaskExecutor`.
- `noopToolExec` / `erroringToolExec` — canned `ToolExecFunc` implementations for happy-path and error-path tests.
- `collectTransitions` — captures `StateTransition` events into a slice for assertion.
- **FSM tests**: `TestNewReActRunner_InitialState`, `TestReActRunner_DirectFinalAnswer`, `TestReActRunner_SingleToolCall`, `TestReActRunner_Run_TwoToolCalls`, `TestReActRunner_MaxIterationsGuard`, `TestReActRunner_ExecutorError`, `TestReActRunner_UnexpectedStatus`, `TestReActRunner_ToolFailure`, `TestReActRunner_StepOnTerminalState`, `TestReActRunner_ThinkingPropagated`, `TestReActRunner_TransitionToolName`, `TestReActRunner_RunResult_Transitions`, `TestReActRunner_WithModelAndSeed`, `TestReActRunner_MultipleToolCallsAccumulate`.

## System Fit
Acts as the specification test suite for the ReAct loop. Each test scenario maps to a documented behavioural contract (e.g. tool failures become error observations rather than fatal errors; max-iterations cap is enforced; step on terminal state returns an error).

## Notable Dependencies
- Standard library only (`context`, `errors`, `fmt`, `testing`)
- `github.com/PixnBits/AegisClaw/internal/runtime/exec` — package under test
