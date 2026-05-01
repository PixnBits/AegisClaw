# store_test.go

## Purpose
Tests for the session `Store` covering all public operations, capacity eviction behaviour, and concurrent access safety. Tests verify the full session lifecycle from open to close, message history capping, and the LRU-style eviction of idle sessions when the store reaches maximum capacity.

## Key Types and Functions
- `TestOpen_NewSession`: verifies a fresh session is created with `StatusActive` and a non-empty ID
- `TestOpen_Idempotent`: calls `Open` twice with the same ID and verifies the same record is returned
- `TestOpen_ReactivatesClosed`: closes a session then re-opens it; verifies status becomes active again
- `TestGet`: verifies `Get` returns a copy with correct fields
- `TestAppendMessage`: appends messages and verifies `History` returns them in order
- `TestClose`: closes a session and verifies status is updated
- `TestHistory_Cap`: appends more than 200 messages and verifies `History` returns only the most recent ones
- `TestList`: verifies all open sessions appear in `List` output
- `TestEviction`: opens 100 sessions, then opens one more; verifies the oldest idle/closed session was evicted
- `TestConcurrentAccess`: spawns multiple goroutines calling `Open`, `AppendMessage`, and `Close` concurrently; verifies no race conditions (run with `-race`)

## Role in the System
Ensures the session store is safe for concurrent use by the TUI and agent goroutines and correctly manages the bounded session capacity.

## Dependencies
- `testing`, `sync`: standard concurrency test utilities
- `internal/sessions`: package under test
