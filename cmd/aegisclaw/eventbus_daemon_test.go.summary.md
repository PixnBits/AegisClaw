# eventbus_daemon_test.go — cmd/aegisclaw

## Purpose
Unit tests for the event-bus polling daemon and memory store integration. Uses a real in-process `memory.Store` backed by a temp directory and an ephemeral age identity.

## Key Functions / Helpers
- `newTestMemoryStore(t)` — creates a real `memory.Store` with a fresh age X25519 identity in a temp dir.
- Tests cover: timer fires spawning a worker, `buildMemoryAutoSummaryMsg` output for non-empty and empty stores, at-least-once timer delivery semantics.

## System Fit
Verifies the event-bus daemon behaves correctly without KVM. The `memory.Store` integration proves the background compaction path works end-to-end.

## Notable Dependencies
- `filippo.io/age`
- `github.com/PixnBits/AegisClaw/internal/eventbus`
- `github.com/PixnBits/AegisClaw/internal/memory`
