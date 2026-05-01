# memory_daemon.go — cmd/aegisclaw

## Purpose
Runs a background goroutine that triggers automatic memory compaction on a 24-hour interval.

## Key Types / Functions
- `startMemoryDaemon(ctx, env)` — ticker-based loop; calls `env.memoryStore.Compact()` once per day.
- `buildMemoryAutoSummaryMsg(ctx, env)` — constructs the system-prompt injection that provides the agent's summarised memory context at the start of each chat session.

## System Fit
Keeps the memory store from growing unbounded. `buildMemoryAutoSummaryMsg` is called by `chat_handlers.go` to inject memory context into every agent turn.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/memory` — `Store.Compact`
