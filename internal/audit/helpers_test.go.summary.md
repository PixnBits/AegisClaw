# `helpers_test.go` — Audit Test Helpers

## Purpose
Provides shared test utilities for the `audit` package's test suite. Currently exports a single helper that constructs a test-scoped `*zap.Logger` using `zaptest`.

## Key Functions

| Symbol | Description |
|--------|-------------|
| `testLogger(t)` | Returns a `*zap.Logger` backed by `zaptest.NewLogger`, which writes output only when the test fails and is automatically cleaned up after the test. |

## How It Fits Into the Broader System
Both `merkle_test.go` and `session_test.go` call `testLogger(t)` to avoid duplicating logger-setup boilerplate. Keeping it in a dedicated helpers file makes it easy to extend the shared test infrastructure as the package grows.

## Notable Dependencies
- `go.uber.org/zap` and `go.uber.org/zap/zaptest`
- Standard library `testing`
