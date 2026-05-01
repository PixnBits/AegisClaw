# chat_handlers.go — cmd/aegisclaw

## Purpose
Implements the daemon-side HTTP handlers for `POST /chat/message` and related chat endpoints. Runs the ReAct agent loop (up to `reactMaxIterations = 10` steps) and streams structured responses.

## Key Types / Functions
- `agentVMRequest` / `agentVMResponse` — JSON envelope types for communication with the agent guest VM.
- `agentChatPayload` — the request body for `/chat/message`.
- `agentChatMsg` / `agentChatResponse` — internal types for agent turn responses.
- `handleChatMessage(env, w, r)` — entry point; validates request, builds system prompt with workspace + memory context, calls `runReActLoop`.
- `runReActLoop(ctx, env, req)` — drives `ReActRunner.Run()`; dispatches tool calls via `env.toolRegistry.Execute`.
- `reactMaxIterations = 10` — guards against infinite loops.
- Embeds `schemas/proposal-create-draft.schema.json` for proposal tool validation.

## System Fit
Core of the daemon's intelligence layer. All chat interactions (CLI, dashboard, gateway webhooks) ultimately flow through these handlers.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/runtime/exec` — `ReActRunner`, `AgentTurnRequest/Response`
- `github.com/PixnBits/AegisClaw/internal/audit` — turn recording
- `github.com/PixnBits/AegisClaw/internal/tracing` — span propagation
