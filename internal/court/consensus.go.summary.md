# `consensus.go` — Weighted Consensus Evaluation

## Purpose
Computes a weighted consensus verdict from a set of persona reviews. Implements the quorum model described in the PRD: approvals, rejections, and "ask" verdicts are weighted by persona importance, and the result determines whether the court has reached consensus.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `ConsensusResult` | Outcome struct: `Reached`, weighted score, approval/reject/ask rates, average risk, per-persona heatmap, and iteration feedback |
| `IterationFeedback` | Collected questions and concerns from `ask` verdicts for the next review round |
| `IterationFeedback.FormatFeedbackPrompt()` | Formats collected questions/concerns as a prompt supplement for the next LLM round |
| `EvaluateConsensus()` | Core function: computes weighted approval rate and compares it against the configured quorum threshold |

## Logic Summary
- Each review is weighted by the persona's configured `Weight` (fallback: equal weighting).
- `approve` and `reject` verdicts contribute directly to weighted totals.
- `ask` verdicts contribute to `askWeight` and their `Questions` are collected as `IterationFeedback`.
- `abstain` verdicts count toward `totalWeight` but contribute nothing to approval/rejection.
- Consensus is reached when `approvalRate >= quorum`.

## Role in the System
Called by `Engine` at the end of each review round to decide whether to approve, reject, escalate, or enter a new round with feedback injected into the reviewer prompts.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/proposal` — `Review`, `ReviewVerdict` types
