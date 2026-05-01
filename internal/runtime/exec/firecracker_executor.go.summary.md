# firecracker_executor.go

## Purpose
Production implementation of the `TaskExecutor` interface that delegates LLM inference to the guest agent running inside a Firecracker microVM. Requests are serialised and sent over a vsock connection managed by `VMRuntime.SendToVM`. This ensures that all LLM execution happens inside the isolated sandbox, never on the host.

## Key Types and Functions
- `FirecrackerTaskExecutor`: struct holding a reference to a `*sandbox.VMRuntime`
- `NewFirecrackerTaskExecutor(runtime *sandbox.VMRuntime) *FirecrackerTaskExecutor`: constructor
- `ExecuteTurn(ctx, AgentTurnRequest) (AgentTurnResponse, error)`: wraps the request as an `agentVMRequest` with type `"chat.message"` and sends it to the VM via `VMRuntime.SendToVM`; deserialises the response
- `agentVMRequest`: internal wire type with `Type` and `Payload` fields for the vsock protocol

## Role in the System
This is the default `TaskExecutor` used in production. When the orchestrator launches a skill task, it creates a `FirecrackerTaskExecutor` backed by the running Firecracker microVM and passes it to the `ReActRunner`. All LLM inference, tool calls, and agent reasoning therefore happen inside the microVM boundary.

## Dependencies
- `internal/sandbox`: `VMRuntime` and vsock communication primitives
- `encoding/json`: request/response serialisation
- `context`: request lifecycle
