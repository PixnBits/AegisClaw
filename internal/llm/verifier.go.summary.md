# verifier.go

## Purpose
Implements cross-model verification for high-stakes LLM decisions. For `VerifyCritical` requests it fans out to multiple models in parallel, computes a field-level majority vote, measures agreement, and flags results for human escalation when consensus falls below a configurable threshold.

## Key Types / Functions
- **`VerificationLevel`** – `VerifyStandard` (single model) or `VerifyCritical` (multi-model consensus).
- **`VerificationRequest`** – input: persona, prompt, model list, temperature, schema, level, optional consensus threshold override.
- **`ModelResponse`** – captures one model's parsed output, raw text, duration, and any error.
- **`VerificationResult`** – output: `Consensus`, `Agreement` ratio, `MajorityResponse`, all `Responses`, `Discrepancies`, `EscalateToHuman`, `Duration`.
- **`Discrepancy`** / **`DiscrepantValue`** – field-level disagreement records.
- **`Verifier`** – wraps an `*Enforcer`; uses `ConsensusThreshold` (default 0.66).
- **`Verifier.Verify(ctx, req)`** – dispatches to `verifySingle` or `verifyMulti` based on level.
- **`verifyMulti`** – parallel goroutine fan-out with `sync.WaitGroup`; collects results, computes majority and agreement.
- **`findDiscrepancies`** / **`findMajority`** / **`computeAgreement`** – core consensus algorithms.
- **`allEqual`** / **`valuesMatch`** – equality helpers (float64 tolerance, JSON-serialised comparison for complex types).
- **`VerificationResult.AuditEntry(persona, level)`** – produces a JSON audit record for the tamper-evident log.

## System Role
The final safety net before CISO-level or kernel-impacting decisions are accepted. Critical verdicts that fail consensus automatically set `EscalateToHuman=true` and are logged as structured discrepancy records.

## Notable Dependencies
- `Enforcer` (enforcer.go) – per-model validated generation calls.
- `sync` – parallel model fan-out.
- `go.uber.org/zap` – consensus/escalation audit logging.
