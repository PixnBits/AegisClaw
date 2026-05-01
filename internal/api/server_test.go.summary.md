# `server_test.go` — API Server Tests

## Purpose
Unit tests for the `Server` type in `server.go`, focused on verifying that `CallDirect` correctly recovers from handler panics and returns a structured error response rather than crashing.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestCallDirectRecoversFromHandlerPanic` | Registers a handler that calls `panic("boom")`, invokes it via `CallDirect`, and asserts the response is non-nil, `Success == false`, and `Error == "internal handler panic"`. |

## How It Fits Into the Broader System
This test guards the panic-recovery path in `CallDirect`, which is used by the in-process dashboard to invoke daemon handlers without a round-trip through the Unix socket. Ensuring panic safety is important because any registered handler (skill, vault, chat) could misbehave.

## Notable Dependencies
- `go.uber.org/zap` (`zap.NewNop()` for a silent logger in tests).
- Standard library `context`, `encoding/json`, `testing`.
