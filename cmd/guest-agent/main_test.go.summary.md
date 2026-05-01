# cmd/guest-agent/main_test.go

## Purpose
Unit tests for `decodeStructuredChatResponse` — the function that parses the guest agent's chat response from various LLM output formats without requiring a live model.

## Key Tests
- `TestDecodeStructuredChatResponseNativeToolCall` — native tool-call format (`proxyToolCall`) → `"tool_call"` status with correct tool name and args.
- `TestDecodeStructuredChatResponseStructuredJSON` — bare JSON object → `"final"` status.
- `TestDecodeStructuredChatResponseFencedJSON` — `\`\`\`json\n{...}\n\`\`\`` fence → parsed correctly.
- `TestDecodeStructuredChatResponseToolCallMarkdownFallback` — `\`\`\`tool-call\n{...}\n\`\`\`` fence → `"tool_call"` status.
- `TestDecodeStructuredChatResponsePlainFinalOnlyOnLastAttempt` — plain text is rejected when `isFinal=false`; accepted as `"final"` when `isFinal=true`.

## System Fit
Guards the response-parsing logic that determines whether the LLM's output is a tool call, a final answer, or a structured JSON response. The `isFinal` parameter prevents plain-text hallucinations from being accepted prematurely.

## Notable Dependencies
- Standard library only (`encoding/json`, `testing`).
