# `iteration.go` — Feedback-Driven Fix Loop

## Purpose
Implements `IterationEngine`, which orchestrates a multi-round "fix loop" that feeds Court reviewer feedback and analysis failures back into the code generator, re-commits the result, and re-runs analysis — up to `MaxFixRounds` (3) times — until all checks pass or the rounds are exhausted.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `MaxFixRounds` | Constant: 3 maximum automatic fix rounds after the initial generation. |
| `ReviewFeedback` | Structured reviewer feedback: persona, verdict, comments, questions, concerns. |
| `FixRequest` | Input to one fix round: proposal ID, skill spec, current files, feedback, analysis result, round number. |
| `FixRequest.FeedbackSummary()` | Formats all feedback and analysis failures into a human-readable string for injection into LLM prompts. |
| `FixRound` | Records a single iteration: generated files, diff, commit hash, analysis result, duration, state. |
| `IterationResult` | Aggregate outcome: all rounds, final round number, final pass/fail, final commit + diff, total duration. |
| `IterationEngine` | Wires together `Pipeline`, `BuilderRuntime`, `CodeGenerator`, `Analyzer`, `git.Manager`, and the kernel. |
| `RunFixLoop` | Main entry point: iterates rounds 2–(MaxFixRounds+1), calling `runSingleFixRound` each time; stops early on analysis pass. |
| `runSingleFixRound` | One iteration: build prompt → `CodeGenerator.Generate` → `git.CommitFiles` → `git.GenerateDiff` → `Analyzer.Analyze`. |
| `ExtractFeedback` | Converts `[]proposal.Review` into `[]ReviewFeedback` by filtering `reject`/`ask` verdicts and extracting concern keywords from evidence. |

## How It Fits Into the Broader System
`IterationEngine` is the self-healing layer between the Court review and artifact packaging. When Court rejects a proposal or analysis fails, it drives automated remediation before human re-review.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/git`, `proposal`, `kernel`.
- `go.uber.org/zap`.
