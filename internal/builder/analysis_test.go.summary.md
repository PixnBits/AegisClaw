# `analysis_test.go` — Analysis Tests

## Purpose
Unit tests for the `AnalysisResult`, `AnalysisRequest`, `Analyzer` constructor, output parsers, and utility functions in `analysis.go`. All tests run without a real builder sandbox by exercising pure data-manipulation logic.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestAnalysisResultHasHighSeverity` | Table-driven: confirms `HasHighSeverity` returns false for info/warning, true for error/critical. |
| `TestAnalysisResultSummaryByTool` | Checks finding counts grouped by tool name. |
| `TestAnalysisRequestValidation` | Missing `ProposalID`, `Files`, or `SkillName` each return the expected error. |
| `TestNewAnalyzerValidation` | Confirms `NewAnalyzer(nil, nil, nil)` returns an error. |
| `TestParseGolangCIOutputJSON` | Parses a 2-issue golangci-lint JSON blob; checks tool name, severity, file, line. |
| `TestParseGolangCIOutputRawText` | Raw non-JSON text yields one `warning` finding. |
| `TestParseGosecOutputJSON` | 3-issue gosec JSON: HIGH→critical, MEDIUM→error, LOW→warning. |
| `TestParseTestOutput*` | Passed output → no findings; failed output → `SeverityError` findings for `FAIL` lines. |
| `TestComputeBinaryHash` | Known SHA-256 for `"hello world"`, determinism, uniqueness. |
| JSON roundtrip tests | `AnalysisFinding` and `AnalysisResult` serialise and deserialise correctly. |

## Notable Dependencies
- Standard library `encoding/json`, `testing`, `time`.
