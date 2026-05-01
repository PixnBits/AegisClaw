# Package: sessions

## Overview
The `sessions` package provides an in-memory ephemeral session registry for tracking agent chat sessions during a daemon run. Sessions are never written to disk; they exist only for the lifetime of the process. Each session accumulates a conversation history (capped at 200 messages) and carries a lifecycle status (active/idle/closed). The store enforces a maximum of 100 concurrent sessions, evicting the oldest idle or closed session when capacity is exceeded.

## Files
- `store.go`: `Store`, `Record`, `Message`, and `Status` types with full CRUD and history API
- `store_test.go`: Lifecycle, history capping, eviction, and concurrency tests

## Key Abstractions
- `Store`: bounded, thread-safe session registry; all public methods are safe for concurrent use
- `Record`: session state holder with conversation history; `Get` and `List` return shallow copies to prevent external mutation
- `Message`: a single conversation turn with role, content, and timestamp
- `Status`: typed lifecycle state (`active`/`idle`/`closed`)
- Caps: `maxMessagesPerSession = 200`, `maxSessions = 100`; eviction targets oldest idle/closed session

## System Role
The session store is used by the TUI `ChatModel` (`internal/tui/chat.go`) to persist conversation history within a session and by the agent orchestration layer to correlate tool calls, messages, and sandbox assignments. Its in-memory-only design means no persistent chat logs are kept, which is an intentional privacy choice.

## Dependencies
- `github.com/google/uuid`: session ID generation
- `sync`: `RWMutex` for concurrent access
- `time`: message and session timestamps
