# Chat UI Data Flow Specification

## Overview
The Web Portal Chat UI must deliver a rich, real-time, transparent experience that shows exactly what the agent is doing — including thinking steps, tool calls, timing, and **incrementally streamed responses** — while keeping the UI responsive and secure.

All data flows through the Web Portal VM → AegisHub → Agent Runtime (and back). The browser never talks directly to any other VM.

## RAIL Principle (Fast Feedback)

All chat interactions must follow the **RAIL** model for excellent perceived performance:

- **R**esponse — Show **something** to the user within **300–800 ms** of their message
- **A**ction — Immediately display the agent's plan or first tool call
- **I**ntermediate — Show thinking steps and tool calls in real time
- **L**atency — Final answer can take longer, but the user never feels "stuck"

**Implementation Rule**: The first `agent_thinking` or `tool_call` message must be sent to the UI **as fast as possible** (ideally under 500 ms).

## Message Types (JSON Schema)

Every message sent over the real-time stream (SSE or WebSocket) has this structure:

```json
{
  "type": "user_message | agent_thinking | tool_call | tool_result | agent_response | error | status",
  "message_id": "msg_abc123",
  "session_id": "sess_xyz789",
  "timestamp": "2026-05-08T20:13:45.123Z",
  "trace_id": "trace_987def",
  "content": { ... },
  "metadata": { ... }
}
```

### Key Message Types for Streaming

**agent_response** (incrementally streamed)
- `content.text`: Partial Markdown content received so far
- `content.is_complete`: `false` while streaming, `true` on final chunk
- UI **appends** each chunk live to the message bubble (exactly like Grok / ChatGPT)

**agent_thinking**
- Used for the critical first fast feedback (RAIL)

**tool_call / tool_result**
- Shown as distinct cards with timing

## Real-Time Streaming Behavior

- Preferred: **Server-Sent Events (SSE)** on `/api/chat/stream`
- As soon as the Agent Runtime produces output, chunks are pushed immediately
- First feedback (`agent_thinking` or initial `tool_call`) must appear **very quickly**

## UI Rendering Rules

- Typing indicator shown during initial `agent_thinking`
- Replaced by growing message as soon as first `agent_response` chunk arrives
- Tool calls appear as separate expandable blocks with timing
- Markdown rendered incrementally and safely on every chunk

## Security & Sanitization

- All content sanitized before rendering
- Secrets redacted by backend components

## Testability Requirements

- Playwright tests must verify:
  - First feedback appears within 800 ms
  - Incremental streaming works correctly
  - RAIL flow (thinking → tool calls → partial response)

## Related Documents
- `./implementation-current.md` (implementation-current features, look & feel, API, chat streaming details)
- `./web-portal.md` (target-state portal spec)
- `./web-portal-vm.md`
- `../agent-runtime.md`
- `../observability.md`

## Traceability
**Driven by:**
- Modern LLM UX expectations
- RAIL principle for low perceived latency
- User journeys #2, #3, and #5