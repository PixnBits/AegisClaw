# react_fsm.go

## Purpose
Implements the ReAct (Reasoning + Acting) loop as a finite state machine that drives multi-step agent execution. The FSM cycles through three states — Thinking, Acting, and Observing — until the agent produces a final answer or hits the maximum iteration limit. Each state transition is observable via an optional callback, enabling the TUI and tests to react to agent progress in real time.

## Key Types and Functions
- `ReActRunner`: the FSM; holds the executor, conversation history, and current state
- States: `StateThinking` → `StateActing` → `StateObserving` → back to `StateThinking`, or `StateThinking` → `StateFinalizing`
- `Step(ctx) (AgentTurnResponse, bool, error)`: advances one FSM iteration; returns (response, done, error)
- `Run(ctx) (AgentTurnResponse, error)`: convenience method that calls `Step` in a loop until done or error
- `WithMaxIterations(n int)` option: caps the loop (default 10)
- `WithModel(name string)` option: overrides the LLM model used
- `WithSeed(seed int)` option: sets inference seed for determinism
- `WithOnTransition(fn)` option: registers a state-change callback

## Role in the System
The ReAct FSM is the heart of agent execution. It is instantiated by the daemon's orchestration layer for each agent task, fed an initial user message, and run until completion. The FSM delegates all LLM calls to a `TaskExecutor`, keeping its own logic model-agnostic.

## Dependencies
- `internal/runtime/exec`: `TaskExecutor`, `AgentTurnRequest`, `AgentTurnResponse`
- `context`: cancellation propagation across FSM steps
