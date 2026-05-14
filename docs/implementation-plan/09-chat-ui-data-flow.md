# 09 - Chat UI Data Flow Implementation

## Goal
Implement real-time streaming chat UI with thinking steps, tool calls, and incremental Markdown responses.

## Acceptance Criteria
- Web Portal shows `agent_thinking` steps
- Tool calls appear as distinct blocks with timing
- Agent responses stream incrementally (like Grok/ChatGPT)
- RAIL principle: first feedback within 800ms
- Playwright tests pass for streaming

## References
- `docs/specs/chat-ui-data-flow.md`
- `docs/specs/web-portal.md`
- `docs/specs/agent-runtime.md`

## Test Command
`make test-chat-ui`