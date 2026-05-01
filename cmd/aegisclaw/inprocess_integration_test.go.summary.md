# inprocess_integration_test.go — cmd/aegisclaw

## Purpose
Integration tests that drive the full agent ReAct loop using `InProcessTaskExecutor` — no Firecracker microVM required. Gated by the `inprocesstest` build tag AND the env var `AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only`.

## Security Warning
`InProcessTaskExecutor` has **zero sandbox isolation**. These tests must **never** run in production or standard CI. They are compiled and run only when both the build tag and the env var are explicitly set.

## Key Tests
- End-to-end proposal lifecycle (create → review → submit) driven by scripted LLM responses.
- Tests for tool-call chaining, max-iteration guard, and error recovery in an in-process context.

## System Fit
Faster than live tests (no KVM boot time) but with the same logical coverage. Useful for rapid iteration during development.

## Notable Dependencies
- Build tag: `inprocesstest`
- `AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only` env var
- `github.com/PixnBits/AegisClaw/internal/runtime/exec` — `InProcessTaskExecutor`
