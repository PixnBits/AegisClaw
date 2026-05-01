# `kernel_test.go` — Tests for the Kernel

## Purpose
Tests the singleton kernel lifecycle, Ed25519 signing/verification, `SignAndLog` audit chain behavior, and action validation — all without Firecracker or a live daemon.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestKernel_GetInstance` | Singleton returns the same pointer on repeated calls |
| `TestKernel_SignAndVerify` | `Sign` + `Verify` round-trip succeeds; tampered data fails verification |
| `TestKernel_SignAndLog` | Valid action appended to Merkle log; returned `SignedAction` has non-nil signature |
| `TestKernel_SignAndLog_InvalidAction` | `Validate()` failures are propagated from `SignAndLog` |
| `TestKernel_KeyPersistence` | Re-opening the kernel from the same key directory loads the same public key |
| `TestKernel_ResetInstance` | After `ResetInstance()`, `GetInstance` creates a fresh kernel |
| `TestAction_NewAction` | UUID and timestamp are auto-populated |
| `TestAction_Validate` | Table-driven: missing ID, unknown type, missing source, zero timestamp |
| `TestAction_Marshal` | Canonical JSON is deterministic and round-trips |
| `TestControlPlane_RegisterHandler` | Handler registered; nil handler and empty type return errors |

## Role in the System
Ensures the cryptographic core and audit chain are correct before any subsystem depends on them.

## Notable Dependencies
- Package under test: `kernel`
- `internal/audit`
- Standard library (`crypto/ed25519`, `testing`)
