# chat_integration_test.go — cmd/aegisclaw

## Purpose
Integration tests for the chat handler that wire up a real `ProposalStore` and `kernel.Kernel` backed by temp directories, without requiring KVM or Ollama.

## Key Functions / Helpers
- `testEnv(t)` — creates a `runtimeEnv` with a real kernel and proposal store; resets the kernel singleton on cleanup.
- Tests verify that the chat handler correctly routes proposal tool calls to the underlying stores and returns structured JSON responses.

## System Fit
Middle ground between pure unit tests and full live tests. Catches regressions in the chat handler's interaction with the proposal and kernel layers without requiring a running VM.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/kernel`
- `github.com/PixnBits/AegisClaw/internal/proposal`
- `go.uber.org/zap/zaptest`
