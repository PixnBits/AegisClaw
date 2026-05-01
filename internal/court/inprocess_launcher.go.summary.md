# `inprocess_launcher.go` — Test-Only In-Process Sandbox Launcher

## Purpose
Provides a `SandboxLauncher` implementation that runs LLM inference directly inside the test process — no Firecracker VM, no jailer, and no vsock. Compiled **only** when the `inprocesstest` build tag is set.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `InProcessSandboxLauncher` | Zero-isolation launcher backed by an `llm.OllamaProxy` |
| `NewInProcessSandboxLauncher()` | Panics unless `AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only` is set in the environment |
| `LaunchReviewer()` | Returns a synthetic UUID-based sandbox ID; no real VM is created |
| `SendReviewRequest()` | Calls the OllamaProxy directly in-process; parses the LLM response as a `ReviewResponse` |
| `StopReviewer()` | No-op; nothing to tear down |

## Security Invariants
- Build tag `inprocesstest` prevents this file from appearing in production binaries.
- An explicit env-var safety guard and a loud `zap.Warn` log are emitted on every instantiation.
- File header includes a multi-line warning banner.

## Role in the System
Enables fast, CI-friendly court engine integration tests that exercise the real reviewer prompt/parsing path without requiring a KVM-capable host or running Ollama locally.

## Notable Dependencies
- `internal/llm` (OllamaProxy)
- `go.uber.org/zap`, `github.com/google/uuid`
