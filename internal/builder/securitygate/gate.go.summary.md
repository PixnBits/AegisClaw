# `gate.go` — Security Gate Pipeline

## Purpose
Implements the mandatory security gates described in PRD §11.2 (deviation D8). Before any skill artifact can be deployed, all generated code must pass four independent gates: SAST, SCA, secrets scanning, and policy-as-code enforcement. The gates run in sequence and any blocking (`error` or `critical`) finding fails the whole pipeline.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `GateType` | Enum: `sast`, `sca`, `secrets_scanning`, `policy`. |
| `Severity` | Enum: `info`, `warning`, `error`, `critical`. |
| `Finding` | Gate, rule ID, severity, file, line, message. |
| `GateResult` | Result for one gate: passed flag, findings, duration, error string. `HasBlocking()` returns true if any finding is error/critical. |
| `PipelineResult` | Aggregated result: all `GateResult`s, `Passed`, `TotalFindings`, `BlockingFindings`, `Duration`. |
| `Gate` (interface) | `Type() GateType` + `Evaluate(*EvalRequest) (*GateResult, error)`. |
| `EvalRequest` | `ProposalID`, `SkillName`, `Files` map, optional `Diff`. |
| `Pipeline` | Runs all registered gates; a blocking finding in any gate sets `Passed = false`. |
| `DefaultPipeline(policies)` | Creates a pipeline with `SASTGate`, `SCAGate`, `SecretsGate`, `PolicyGate`. |
| `SASTGate` | Regex-based Go source analysis: command injection (G204), hardcoded creds (G101), path traversal (G304), weak crypto (G401), unencrypted HTTP (G104), SSRF (G107), directory/file permissions (G301, G306). |
| `SCAGate` | Checks `go.mod` for banned packages (JWT-go, satori/uuid) and `package.json` for wildcard/git-URL dependencies. |
| `SecretsGate` | Regex patterns for AWS keys, GitHub tokens, generic API keys, and PEM private key headers. |
| `PolicyGate` | Evaluates `[]Policy` check functions: no unsafe exec, no host-FS paths, no undeclared network, no privileged syscalls. |
| `DefaultPolicies()` | Returns the four default policy rules (`POL-NO-UNSAFE-EXEC`, `POL-NO-HOST-FS`, `POL-NO-NETWORK-UNLESS-DECLARED`, `POL-NO-PRIVILEGED-OPS`). |

## How It Fits Into the Broader System
`securitygate.Pipeline` is called by `builder.Pipeline.Execute` at step 8.5, after static analysis and before SBOM generation. It cannot be skipped and its failures are reflected in `PipelineResult.SecurityGateResult`.

## Notable Dependencies
- Standard library: `encoding/json`, `regexp`, `strings`, `time`.
- No external dependencies — intentionally self-contained for auditability.
