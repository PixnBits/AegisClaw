# `engine.go` — Governance Court Engine

## Purpose
Orchestrates the full multi-round court review process for proposals. For each proposal the engine: transitions its status, runs concurrent persona reviews, evaluates weighted consensus, and either approves, rejects, escalates, or starts a new round with accumulated feedback.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `EngineConfig` | `MaxRounds`, `ReviewTimeout`, `ConsensusQuorum` (default 0.8), `MaxRiskThreshold` (default 7.0) |
| `SessionState` | Enum: `pending`, `reviewing`, `consensus`, `approved`, `rejected`, `escalated` |
| `Session` | Tracks a full court review: ID, proposal ID, round number, per-persona results, feedback |
| `RoundResult` | Reviews, heatmap, avg risk, consensus flag, and feedback for one round |
| `ReviewerFunc` | Pluggable function that creates a sandbox, injects a persona prompt, and returns a `proposal.Review` |
| `RoundUpdateFunc` | Called after a non-consensus round to persist the updated proposal before the next round |
| `Engine` | Main orchestrator; holds config, proposal store, kernel, personas, reviewer func |
| `Engine.Review()` | Entry point: runs up to `MaxRounds` review rounds, persists session JSON, logs to Merkle audit chain |
| `Engine.GetSession()` | Returns a session by ID |
| `Engine.ListSessions()` | Returns all known sessions |

## Role in the System
The Governance Court Engine is the SDLC gatekeeper: every code generation proposal must pass through it before deployment. It integrates with the `proposal.Store` for proposal persistence, the `kernel` for audit logging, and the `SandboxLauncher` for sandboxed LLM inference.

## Notable Dependencies
- `internal/kernel`, `internal/proposal`
- `github.com/google/uuid`, `go.uber.org/zap`
