# `pipeline.go` — End-to-End Build Pipeline

## Purpose
Orchestrates the complete flow from an approved proposal to a committed, analysed, security-gated, and SBOM-stamped code diff. `Pipeline` coordinates `BuilderRuntime`, `CodeGenerator`, `git.Manager`, `Analyzer`, the `securitygate.Pipeline`, and the `sbom` package in a single `Execute` call.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `PipelineState` | Enum: `pending`, `building`, `complete`, `failed`, `cancelled`. |
| `PipelineResult` | Full run record: proposal ID, state, builder ID, commit hash, branch, diff, generated files + hashes, reasoning, `AnalysisResult`, `SecurityGateResult`, `SBOMPath`, error, timing. |
| `Pipeline` | Central coordinator; holds references to all subsystems. |
| `NewPipeline` | Validates all required dependencies and initialises the run registry. |
| `Pipeline.SetSBOMDir` | Configures the directory for SBOM JSON output (empty = disabled). |
| `Pipeline.SetWorkspaceSkillContext` | Injects `SKILL.md` content into the code-generation system prompt. |
| `Pipeline.Execute` | 9-step flow: (1) choose repo kind → (2) launch builder VM → (3) collect existing code (edit mode) → (4) generate code → (5) create branch + commit → (6) generate diff → (7) compute file hashes → (8) run static analysis → (8.5) run security gates → (9) optionally write SBOM → mark complete + audit log. |
| `Pipeline.GetResult` / `ListResults` | Retrieve run history. |
| `computeFileHashes` | SHA-256 hex hashes for all generated files. |

## How It Fits Into the Broader System
`Pipeline` is the primary entry point for the daemon's build subsystem. It is called when a proposal transitions to `implementing` status and its result (diff + analysis) is handed back to the Court for review.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/builder/securitygate`
- `github.com/PixnBits/AegisClaw/internal/sbom`, `git`, `proposal`, `kernel`
- `go.uber.org/zap`
