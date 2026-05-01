# verifier_test.go

## Purpose
Tests for the `Verifier` type, covering standard and critical verification modes, consensus/disagreement scenarios, edge-case guards, and individual helper functions.

## Key Test Cases
- **`TestVerificationLevelString`** – table-driven `VerificationLevel.String()` coverage.
- **`newTestVerifier(handler)`** – helper wiring a mock HTTP server → `Client` → `Enforcer` → `Verifier`.
- **`TestVerifySingleModel`** – `VerifyStandard` with one model returns `Consensus=true`, `Agreement=1.0`.
- **`TestVerifyCriticalConsensus`** – two models return identical JSON; `EscalateToHuman=false`.
- **`TestVerifyCriticalDisagreement`** – models return conflicting verdicts; `EscalateToHuman=true`, `Discrepancies` non-empty.
- **`TestVerifyNoModels`** / **`TestVerifyNoSchema`** – guards return errors immediately without calling the enforcer.
- **`TestVerifyCriticalOneModel`** – critical verification with only 1 model returns an error.
- **`TestValuesMatch`** – table-driven equality checks for strings, float64s (with tolerance), bools, and arrays.
- **`TestAllEqual`** – edge cases: nil, single value, equal pair, and differing pair.
- **`TestAuditEntry`** – verifies the JSON audit record contains expected keys (`type`, `persona`, `level`, `consensus`).
- **`TestFindDiscrepancies`** – two models with different verdicts produce a discrepancy entry with two values.
- **`TestFindMajority`** – 2-vs-1 majority vote selects `"approve"`.
- **`TestComputeAgreement`** – full-agreement, half-agreement, and single-response cases.
- **`TestVerifyAllModelsFail`** – all models return 500; expects a non-nil error.

## Notable Dependencies
- `net/http/httptest` – mock Ollama endpoint.
- `sync/atomic` – ordered call counting for disagreement test.
