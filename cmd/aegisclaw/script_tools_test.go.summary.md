# script_tools_test.go — cmd/aegisclaw

## Purpose
Unit tests for `parseRunScriptParams` — the argument validation function for the `run_script` tool.

## Key Tests
- `TestParseRunScriptParams_Valid` — valid Python params parse correctly.
- `TestParseRunScriptParams_InvalidLanguage` — unsupported language returns a descriptive error.
- `TestParseRunScriptParams_EmptyCode` — empty code returns an error.
- `TestParseRunScriptParams_ScriptTooLarge` — code exceeding `maxScriptSize` returns an error.
- `TestParseRunScriptParams_TooManyArgs` — arg count exceeding `maxScriptArgs` returns an error.

## System Fit
Ensures input validation is complete before any `os/exec` invocation. Pure unit tests; no process execution.

## Notable Dependencies
- Standard library only (`encoding/json`, `testing`).
