# memory_handlers.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers for the agent memory store: `memory.list`, `memory.search`, `memory.compact`, and `memory.delete`.

## Key Types / Functions
- `memoryListRequest` — `{ tier, limit, count_only }`.
- `memorySearchRequest` — `{ query, k, task_id }`.
- `makeMemoryListHandler(env)` — lists memories with optional tier filter; `count_only` mode returns just a count.
- `makeMemorySearchHandler(env)` — performs a similarity search with top-k results.
- `makeMemoryCompactHandler(env)` — triggers LLM-based compaction of old entries.
- `makeMemoryDeleteHandler(env)` — deletes a memory by ID.

## System Fit
Backs the `retrieve_memory`, `list_memories`, `compact_memory`, and `delete_memory` ReAct tools. Also used by `memory_cmd.go` CLI subcommands.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/memory`
