# `iteration_test.go` — Iteration Engine Tests

## Purpose
Unit tests for `FixRequest` validation, `FeedbackSummary` formatting, `ExtractFeedback` logic, and `IterationEngine` constructor validation — all exercised without a live builder sandbox or git repository.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestFixRequestValidation` | Empty proposal ID, invalid skill spec, empty files, missing feedback+analysis, and out-of-range round numbers each fail with descriptive errors. |
| `TestFeedbackSummary` | Confirms that reviewer persona, verdict, comments, questions, concerns, and analysis failures (test/lint/security/build output and findings) all appear in the formatted string. |
| `TestExtractFeedback` | Only `reject`/`ask` verdicts produce `ReviewFeedback`; evidence strings containing concern keywords (`risk`, `vulnerable`, etc.) are surfaced as concerns; `approve` verdicts are ignored. |
| `TestNewIterationEngineValidation` | Nil pipeline, builder runtime, code generator, git manager, or kernel each return the appropriate error. |
| `TestFixRoundStateConstants` | Asserts string values of `FixRoundState` constants. |
| `TestMaxFixRoundsConstant` | Confirms `MaxFixRounds == 3`. |

## How It Fits Into the Broader System
These tests guard the fix-loop's decision logic and prompt-assembly path, ensuring that reviewer feedback is faithfully communicated to the LLM and that the engine rejects malformed inputs early.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/proposal`
- Standard library `strings`, `testing`.
