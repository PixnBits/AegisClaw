# executor_test.go

## Purpose
Unit tests for the `AgentTurnRequest`/`AgentTurnResponse` wire types and the `FirecrackerTaskExecutor`. Runs with the default build tag (no guard) so it runs in every `go test` invocation.

## Key Types / Functions
- `stubVMRuntime` — in-test `VMRuntime` implementation; captures the last request and returns a scripted response or error.
- `buildVMResponse` — helper that wraps a chat-response payload inside the `agentVMResponse` JSON envelope.
- `TestAgentTurnRequest_JSONRoundTrip` — verifies all fields survive a marshal/unmarshal cycle.
- `TestAgentTurnRequest_ZeroTemperaturePreserved` — confirms `temperature=0` is omitted from the public JSON (omitempty) but the caller behaviour is documented.
- `TestAgentTurnResponse_JSONRoundTrip` — covers `"final"`, `"tool_call"`, and thinking-field variants.
- `TestFirecrackerTaskExecutor_*` — five tests covering: final response, tool-call response, VM transport error, agent-level error (`success=false`), malformed JSON, temperature+seed propagation, zero-temperature forwarded when seed is set, and concurrent safety.
- `TestVMRuntimeInterface` — compile-time check that `stubVMRuntime` satisfies `VMRuntime`.

## System Fit
Guards the public API surface of `executor.go` and `firecracker_executor.go`. The `stubVMRuntime` pattern lets all tests run without KVM or a real Firecracker binary.

## Notable Dependencies
- `encoding/json`, `net/http/httptest` — standard library
- `github.com/PixnBits/AegisClaw/internal/runtime/exec` — package under test
