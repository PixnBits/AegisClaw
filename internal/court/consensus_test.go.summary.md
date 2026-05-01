# `consensus_test.go` — Tests for Weighted Consensus

## Purpose
Tests the `EvaluateConsensus` function and `IterationFeedback.FormatFeedbackPrompt` across a variety of review configurations to verify quorum math, feedback collection, and edge cases.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestEvaluateConsensus_AllApprove` | Unanimous approval → `Reached: true` |
| `TestEvaluateConsensus_AllReject` | Unanimous rejection → `Reached: false` |
| `TestEvaluateConsensus_MixedQuorum` | Mixed reviews with weights; checks precise approval rate against threshold |
| `TestEvaluateConsensus_AskReducesEffectiveVote` | Ask verdicts contribute to `AskRate` and accumulate questions in feedback |
| `TestEvaluateConsensus_Empty` | No reviews → `Reached: false`, empty heatmap returned without panic |
| `TestEvaluateConsensus_AbstainDoesNotBlock` | Abstain counts in total weight but does not reduce approval rate below quorum |
| `TestIterationFeedback_FormatFeedbackPrompt` | Output contains round number, questions, and concerns |
| `TestIterationFeedback_EmptyFeedback` | Returns empty string when no questions or concerns are present |

## Role in the System
Verifies that the consensus gate behaves correctly under all verdict combinations so that the `Engine` can safely rely on `EvaluateConsensus` to drive multi-round iteration logic.

## Notable Dependencies
- Package under test: `court`
- `github.com/PixnBits/AegisClaw/internal/proposal`
