# lookup_journey_test.go — cmd/aegisclaw

## Purpose
Journey and contract tests for the dynamic semantic tool-lookup skill. Uses `testEnvWithLookup` — a real `LookupStore` backed by a temp directory. No Firecracker, Ollama, or KVM required.

## Key Scenarios
1. Index then retrieve a single tool.
2. `lookup_tools` tool returns Gemma 4 control-token blocks.
3. `lookup.index_tool` updates an existing entry.
4. `lookup_tools` with empty query → error.
5. `seedLookupStore` indexes all registry tools.
6. `makeLookupSearchHandler` API response shape contract.
7. `makeLookupListHandler` API response shape contract.
8. Nil lookup store → error, not panic.
9. Tool events recorded for lookup calls.
10. ReActRunner FSM step-by-step with `lookup_tools` call.
11. ReActRunner `Run` full loop with `lookup_tools`.

## System Fit
Specification tests for the lookup feature. Ensures API response shapes are stable for the dashboard and portal consumers.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/lookup`
