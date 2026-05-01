# lookup_handlers.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers for the semantic tool-lookup store: `lookup.search` and `lookup.list`.

## Key Types / Functions
- `lookupSearchRequest` — `{ query string, max_results int }`.
- `makeLookupSearchHandler(env)` — performs a vector-similarity search over indexed tools; returns Gemma 4 control-token blocks. Default `max_results = 6`.
- `makeLookupListHandler(env)` — returns all indexed tool names and descriptions (no embeddings).

## System Fit
Enables the agent to discover tools dynamically at runtime without hard-coding tool names. Powers the `lookup_tools` ReAct tool call.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/lookup`
