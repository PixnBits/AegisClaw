# Package `internal/court` — Governance Court

## Purpose
Implements the Governance Court SDLC gatekeeper. Every code-generation proposal must pass a multi-round, multi-persona LLM review before deployment. The court engine orchestrates sandboxed reviewer VMs, evaluates weighted consensus, and produces an approved or rejected verdict with a full audit trail.

## Files

| File | Description |
|---|---|
| `consensus.go` | Weighted consensus evaluation; `IterationFeedback` for multi-round prompting |
| `consensus_test.go` | Table-driven tests for quorum math and feedback collection |
| `engine.go` | `Engine` — orchestrates review rounds; `Session` / `RoundResult` state tracking |
| `engine_test.go` | Engine lifecycle tests using stub reviewers |
| `inprocess_launcher.go` | Test-only `SandboxLauncher` (build tag `inprocesstest`); zero-isolation, safety-gated |
| `loader.go` | `LoadPersonas` — reads YAML persona files; `EnsureDefaultPersonas` |
| `loader_test.go` | Edge cases for persona directory loading |
| `persona.go` | `Persona` struct: name, role, system prompt, models, weight, output schema |
| `reviewer.go` | `ReviewRequest`/`ReviewResponse` wire protocol; `SandboxLauncher` interface; `FirecrackerLauncher` |
| `reviewer_test.go` | Schema validation tests for `ReviewResponse` |

## Key Abstractions

- **`Persona`** — reviewer identity; loaded from YAML; carries LLM models and voting weight
- **`SandboxLauncher`** — interface decoupling the engine from Firecracker; enables test stubs
- **`Engine`** — runs up to `MaxRounds` of parallel persona reviews per proposal, applying iteration feedback on non-consensus rounds
- **`EvaluateConsensus`** — weighted quorum gate; collects `IterationFeedback` for `ask` verdicts
- **`FirecrackerLauncher`** — production: airgapped reviewer microVM, vsock-only LLM access via `OllamaProxy`

## How It Fits Into the Broader System
The court engine is invoked by the daemon's `proposal.review` API action. It reads proposals from `internal/proposal`, logs every action to the kernel's Merkle audit chain, and persists session JSON for restart recovery. All code changes to AegisHub must flow through this engine.

## Notable Dependencies
- `internal/kernel`, `internal/proposal`, `internal/sandbox`, `internal/llm`
- `go.uber.org/zap`, `gopkg.in/yaml.v3`, `github.com/google/uuid`
