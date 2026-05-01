# audit_verify.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw audit verify` subcommand. Reads the audit log from disk and verifies the hash chain using the kernel's Ed25519 public key, printing pass/fail for each entry.

## Key Types / Functions
- `runAuditVerify(cmd, args)` — loads kernel public key from the kernel store, calls `audit.VerifyChain`, prints verification results.

## System Fit
Offline integrity check that does not require a running daemon. Detects any tampering with the audit log between the point of writing and now.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/audit` — `VerifyChain`
- `github.com/PixnBits/AegisClaw/internal/kernel` — public key retrieval
