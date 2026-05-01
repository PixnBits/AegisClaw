# proxy_test.go

## Purpose
Tests for the streaming NDJSON decoder in `proxy.go` (`decodeOllamaChatBody`), with a focus on the tool-call accumulation logic and edge cases introduced by Ollama's multi-chunk streaming format.

## Key Test Cases
- **`TestDecodeOllamaChatBodyToolCallsNotOverwritten`** – regression test for the bug where `tool_calls` captured in the first chunk were discarded when a later chunk arrived with an empty `tool_calls` array. Verifies that only the first non-empty batch is kept.
- **`TestDecodeOllamaChatBodyContentWithToolCalls`** – confirms content text and tool calls coexist correctly when spread across multiple chunks.
- **`TestDecodeOllamaChatBodyMultipleToolCalls`** – asserts that two tool calls in a single chunk are both captured and in order.
- **`TestDecodeOllamaChatBodyMalformedJSON`** – expects an error when a chunk contains invalid JSON.
- **`TestDecodeOllamaChatBodyEmptyResponse`** – expects an error for a completely empty response body.

## System Role
Regression suite for the streaming decoder that is central to skill VM tool-calling support. The "not overwritten" test documents a specific Ollama streaming behaviour that was previously a source of silent data loss.

## Notable Dependencies
- `strings.NewReader` – feeds synthesized NDJSON response bodies without HTTP overhead.
