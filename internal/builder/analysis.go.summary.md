# `analysis.go` — Static Analysis & Build Findings

## Purpose
Defines the data model for analysis results and provides the `Analyzer` type that coordinates static-analysis, test, lint, security-scan, and build steps by dispatching an `analysis.run` control message to a builder sandbox.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `AnalysisSeverity` | Enum: `info`, `warning`, `error`, `critical`. |
| `AnalysisFinding` | A single finding with `Tool`, `Severity`, `File`, `Line`, `Column`, `Message`, `Rule`. |
| `AnalysisResult` | Aggregated output: per-tool pass/fail booleans, `Findings` slice, `BinaryHash`, overall `Passed`, `FailureReason`, `Duration`. |
| `AnalysisResult.HasHighSeverity()` | Returns `true` if any finding is `error` or `critical`. |
| `AnalysisResult.SummaryByTool()` | Groups finding counts by tool name. |
| `AnalysisRequest` | Input to the sandbox: `ProposalID`, `Files`, `Diff`, `SkillName`. |
| `Analyzer` | Sends requests to the builder sandbox via `BuilderRuntime.SendBuildRequest` and parses `AnalysisResult`. |
| `ParseGolangCIOutput` | Parses golangci-lint JSON (or raw text) into `[]AnalysisFinding`. |
| `ParseGosecOutput` | Parses gosec JSON (or raw text) into `[]AnalysisFinding`, mapping HIGH/MEDIUM/LOW to severity levels. |
| `ParseTestOutput` | Extracts failure findings from `go test` output. |
| `ComputeBinaryHash` | SHA-256 hex hash of a binary blob. |

## How It Fits Into the Broader System
`Analyzer` is invoked by `Pipeline.Execute` (step 8) after code generation. Its `AnalysisResult` feeds both the security gate decision and the fix-loop in `IterationEngine`.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/kernel` — `SignAndLog` for audit.
- `go.uber.org/zap`.
