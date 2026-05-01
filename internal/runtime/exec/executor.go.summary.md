# executor.go

## Purpose
Defines the `TaskExecutor` interface and the shared request/response types that decouple the ReAct FSM from any specific LLM backend or execution environment. By programming to this interface, the rest of the system can swap between the production Firecracker-backed executor, an in-process test executor, or future backends without changing the FSM logic.

## Key Types and Functions
- `TaskExecutor` interface: single method `ExecuteTurn(ctx context.Context, req AgentTurnRequest) (AgentTurnResponse, error)`
- `AgentTurnRequest`: Messages (conversation history), Model (Ollama model name), StreamID, StructuredOutput schema, TraceID, Temperature, Seed
- `AgentTurnResponse`: Status (`"final"` or `"tool_call"`), Content (assistant text), Thinking (chain-of-thought), Tool (tool name if status is tool_call), Args (JSON arguments map)
- Status constants: `StatusFinal`, `StatusToolCall`

## Role in the System
Acts as the abstraction boundary between the ReAct FSM (`react_fsm.go`) and concrete LLM backends. The FSM calls `ExecuteTurn` on each iteration and interprets the response status to decide whether to finalise or continue the reasoning loop. All executor implementations — Firecracker, in-process, and recorder — satisfy this interface.

## Dependencies
- `context`: request lifecycle management
- Standard library only; no external dependencies — this is a pure interface definition file
