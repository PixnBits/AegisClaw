# memory_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw memory` subcommand tree: `retrieve`, `list`, `compact`, and `delete`. Routes all operations through the daemon's `chat.tool` endpoint so they run in the same security context as agent tool calls.

## Key Types / Functions
- `runMemoryRetrieve(cmd, args)` — dispatches `retrieve_memory` tool with a query string.
- `runMemoryList(cmd, args)` — dispatches `list_memories` tool.
- `runMemoryCompact(cmd, args)` — dispatches `compact_memory` tool (triggers LLM-based compaction).
- `runMemoryDelete(cmd, args)` — dispatches `delete_memory` tool with a memory ID.

## System Fit
CLI interface to the agent's persistent memory store. Using `chat.tool` ensures that all memory operations are audit-logged as tool calls.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
