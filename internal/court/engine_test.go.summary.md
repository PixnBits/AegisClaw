# `engine_test.go` — Tests for the Court Engine

## Purpose
Integration-level tests for the `Engine` orchestrator. Uses a stub `ReviewerFunc` to drive the engine without real LLM sandboxes, verifying round progression, consensus detection, rejection, escalation on max rounds, high-risk threshold enforcement, and session persistence.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestEngine_ApprovesOnConsensus` | All-approve stub triggers `SessionApproved` in round 1 |
| `TestEngine_RejectsOnRejection` | Majority-reject stub produces `SessionRejected` |
| `TestEngine_EscalatesOnMaxRounds` | No-consensus stub runs `MaxRounds` then escalates |
| `TestEngine_HighRiskThreshold` | Average risk > `MaxRiskThreshold` → immediate rejection |
| `TestEngine_MultiRoundFeedback` | Feedback from ask verdicts is injected into subsequent rounds |
| `TestEngine_SessionPersistence` | Completed session JSON is written to `sessionDir` and loadable by a new engine instance |
| `TestNewEngine_Validation` | Constructor rejects nil store, nil kernel, empty personas, nil reviewer func, bad quorum/risk config |

## Role in the System
Ensures the court review lifecycle is reliable without requiring live Firecracker VMs or an Ollama instance, making it safe to run in CI.

## Notable Dependencies
- Package under test: `court`
- `internal/kernel`, `internal/proposal`
