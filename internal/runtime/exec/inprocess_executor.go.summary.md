# inprocess_executor.go

## Purpose
A test-only in-process implementation of the `TaskExecutor` interface, compiled only with the `inprocesstest` build tag. It runs LLM inference directly in the test process by communicating with a locally running Ollama instance, bypassing the Firecracker VM layer entirely. To prevent accidental use in production, the executor panics at construction time unless the environment variable `AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only` is set.

## Key Types and Functions
- `InProcessTaskExecutor`: struct implementing `TaskExecutor`; holds an `*http.Client` configured for Ollama
- Build tag: `//go:build inprocesstest` — excluded from normal and production builds
- Safety guard: constructor checks `AEGISCLAW_INPROCESS_TEST_MODE` env var and panics if not set to the expected value
- `ExecuteTurn(ctx, AgentTurnRequest) (AgentTurnResponse, error)`: sends the request directly to Ollama's HTTP API and maps the response to `AgentTurnResponse`

## Role in the System
Enables integration tests for the ReAct FSM and higher-level agent logic without requiring a running Firecracker VM. Used alongside the `OllamaRecorder` for cassette-based replay tests. This executor is never linked into the production binary.

## Dependencies
- `net/http`: Ollama HTTP client
- `encoding/json`: request/response serialisation
- `os`: environment variable check
- `internal/runtime/exec`: `TaskExecutor` interface (same package)
