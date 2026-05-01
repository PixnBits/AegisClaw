# Package: runtime/exec

## Overview
The `exec` package defines the agent task execution abstraction and provides multiple implementations for different environments. At its core is the `TaskExecutor` interface and the `ReActRunner` FSM that implements the Reasoning + Acting loop. Production execution routes through a Firecracker microVM via vsock; test execution uses an in-process Ollama client with optional cassette replay.

## Files
| File | Description |
|------|-------------|
| `executor.go` | `TaskExecutor` interface and shared `AgentTurnRequest`/`AgentTurnResponse` wire types |
| `react_fsm.go` | ReAct FSM — Thinking/Acting/Observing states, `Step`/`Run` methods, option functions |
| `firecracker_executor.go` | Production executor; delegates to guest VM via `VMRuntime.SendToVM` |
| `inprocess_executor.go` | Test-only in-process executor (build tag `inprocesstest`); requires safety env var |
| `ollama_recorder.go` | Test-only VCR cassette record/replay for Ollama (build tag `inprocesstest`) |
| `executor_test.go` | Unit tests for wire types and `FirecrackerTaskExecutor`; uses `stubVMRuntime` (no KVM required) |
| `ollama_recorder_test.go` | Tests for cassette record/replay and end-to-end `OllamaRecorder`+`ReActRunner` integration (build tag `inprocesstest`) |
| `react_fsm_test.go` | Comprehensive FSM tests covering all states, transitions, option functions, and error paths |

## Key Abstractions
- `TaskExecutor`: single-method interface decoupling the FSM from all LLM backends
- `ReActRunner`: configurable FSM with max-iterations cap, model/seed overrides, and transition callbacks
- `AgentTurnRequest`/`AgentTurnResponse`: wire types shared across all executor implementations
- Status values: `"final"` terminates the loop; `"tool_call"` triggers the Acting state

## System Role
This package is the execution engine of AegisClaw. Every agent task — whether launched by the TUI chat, a CLI command, or an automated orchestrator — is driven through a `ReActRunner` backed by a `TaskExecutor`. The clean interface boundary ensures that tests can exercise the full agent logic without a running VM.

## Dependencies
- `internal/sandbox`: `VMRuntime` for vsock communication (production only)
- `context`: lifecycle management across FSM steps
- `net/http`: Ollama client (test builds only)
