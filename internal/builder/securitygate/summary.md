# `internal/builder/securitygate/` — Package Summary

## Overview
Package `securitygate` implements the mandatory security gates mandated by PRD §11.2 (deviation D8). Every skill code submission must pass all four gates before the builder pipeline can proceed to SBOM generation and artifact packaging. The package is intentionally dependency-free (only standard library) to maximise auditability.

## Architecture
A `Pipeline` runs registered `Gate` implementations in sequence. A single blocking (`error` or `critical`) finding in any gate causes `PipelineResult.Passed = false`. Non-blocking (`warning`, `info`) findings are recorded but do not fail the pipeline.

## Gates

| Gate | Type | What It Checks |
|------|------|----------------|
| `SASTGate` | `sast` | Regex-based Go source analysis (8 rules: G101–G401) |
| `SCAGate` | `sca` | `go.mod` banned packages; `package.json` wildcard/git deps |
| `SecretsGate` | `secrets_scanning` | AWS keys, GitHub tokens, private key PEM headers, generic API keys |
| `PolicyGate` | `policy` | PRD isolation invariants (no unsafe exec, no host FS, no undeclared network, no privileged syscalls) |

## File Table

| File | Role |
|------|------|
| `gate.go` | All gate implementations, `Pipeline`, `EvalRequest`, `GateResult`, `PipelineResult`, `DefaultPolicies` |
| `gate_test.go` | Comprehensive positive/negative tests for every gate and the pipeline orchestrator |

## Notable Dependencies
- Standard library only: `encoding/json`, `regexp`, `strings`, `time`
- No external imports — deliberate for security auditability
