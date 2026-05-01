# `gate_test.go` — Security Gate Tests

## Purpose
Comprehensive tests for all four security gates (SAST, SCA, Secrets, Policy), the `Pipeline` orchestrator, `GateResult.HasBlocking`, and `EvalRequest.Validate`. Tests use real in-memory code snippets to validate both positive (clean code passes) and negative (vulnerable code fails) paths.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestSASTGate_CleanCode` | Clean `Hello World` Go file passes all SAST rules. |
| `TestSASTGate_DetectsWeakCrypto` | `crypto/md5` import triggers G401 error finding. |
| `TestSASTGate_DetectsHardcodedSecret` | `password = "..."` assignment triggers G101 critical finding. |
| `TestSASTGate_SkipsNonGoFiles` | Patterns in `README.md` do not trigger SAST. |
| `TestSCAGate_CleanGoMod` | `go.mod` with only `github.com/google/uuid` passes SCA. |
| `TestSCAGate_DetectsBannedDep` | `github.com/dgrijalva/jwt-go` triggers `SCA-BANNED` error. |
| `TestSCAGate_DetectsUnpinnedNpmDep` | `"lodash": "*"` triggers `SCA-UNPINNED` error. |
| `TestSecretsGate_CleanCode` | Empty `main.go` passes. |
| `TestSecretsGate_DetectsAWSKey` | `AKIAIOSFODNN7EXAMPLE` pattern triggers critical finding. |
| `TestSecretsGate_DetectsPrivateKey` | PEM `BEGIN RSA PRIVATE KEY` header triggers critical finding. |
| `TestPolicyGate_CleanCode` | Simple `fmt.Println` code passes all default policies. |
| `TestPolicyGate_DetectsPrivilegedOps` | `syscall.Setuid(0)` triggers `POL-NO-PRIVILEGED-OPS` critical finding. |
| `TestPipelineDefault` | All four gates run; safe code passes; exactly 4 `GateResult`s returned. |
| `TestPipelineBlocksUnsafe` | Code with MD5 and hardcoded password has `BlockingFindings > 0` and `Passed == false`. |
| `TestPipelineValidation` | Empty `EvalRequest` returns a validation error. |
| `TestGateResultHasBlocking` | Table-driven: warning → false; error/critical → true. |
| `TestEvalRequestValidation` | Missing proposal ID, skill name, or files each fail validation. |

## Notable Dependencies
- Standard library `testing` only — no external test dependencies.
