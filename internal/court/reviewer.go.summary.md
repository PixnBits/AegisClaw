# `reviewer.go` — Reviewer Sandbox Launcher & Request/Response

## Purpose
Defines the wire protocol for reviewer sandboxes (`ReviewRequest`/`ReviewResponse`), the `SandboxLauncher` interface, and two implementations: the production `FirecrackerLauncher` and the out-of-package-visible struct definition for the test launcher.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `ReviewRequest` | Sent to a reviewer sandbox: proposal metadata, persona prompt, model, round number, optional temperature/seed |
| `ReviewResponse` | Expected JSON from the LLM: `verdict`, `risk_score`, `evidence []string`, `questions []string`, `comments` |
| `ReviewResponse.UnmarshalJSON()` | Tolerates LLMs returning `evidence` as a string instead of a JSON array |
| `ReviewResponse.Validate()` | Schema gate: checks verdict is valid, risk score in [0, 10], evidence present for non-abstain verdicts |
| `SandboxLauncher` | Interface: `LaunchReviewer`, `SendReviewRequest`, `StopReviewer` |
| `FirecrackerLauncher` | Production launcher: creates an airgapped Firecracker microVM (no network, vsock-only), injects persona prompt, calls Ollama via `OllamaProxy` |
| `NewFirecrackerLauncher()` | Constructor; requires `FirecrackerRuntime`, `RuntimeConfig`, and an `OllamaProxy` |

## Reviewer VM Security Model
Each reviewer VM is launched with `NetworkPolicy{NoNetwork: true, DefaultDeny: true}`. LLM inference is routed through the host-side `OllamaProxy` over vsock — no IP stack in the VM.

## Role in the System
Bridges the `Engine` orchestrator to actual LLM sandboxes. The `SandboxLauncher` abstraction allows tests to inject a stub without Firecracker.

## Notable Dependencies
- `internal/kernel`, `internal/llm`, `internal/proposal`, `internal/sandbox`
- `go.uber.org/zap`, `github.com/google/uuid`
