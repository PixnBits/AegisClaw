# 04 - Audit Merkle Root Signing & Static Verification Hardening

**Goal**: Isolate and harden the Merkle tree root signing process in the Host Daemon. Add static-binary verification, capability dropping, and tamper-evidence guarantees. Ensure signing never processes untrusted data.

## Paranoid Security Context
From `docs/specs/host-daemon.md` and `docs/architecture.md`:
- Only the daemon may sign Merkle roots (for tamper-evident audit log).
- Signing must be minimal, auditable, and protected against compromise.
- Part of the "explicit non-responsibilities" enforcement.

## Tasks

1. **Isolate signing logic**:
   - Move any signing code into a tiny dedicated package (`internal/audit/signer.go` or similar) with zero dependencies on business logic.
   - Signing must use only the daemon's Ed25519 key (never shared).
2. **Add static-binary & integrity checks**:
   - On startup: verify the daemon binary itself against a known good hash (or embedded signature).
   - Reject startup if binary has been modified.
3. **Capability & privilege hardening**:
   - Drop all Linux capabilities except those strictly needed for signing + socket + Firecracker control.
   - Use `prctl` + seccomp to prevent privilege escalation.
4. **Signing cadence & audit**:
   - Sign roots at regular intervals (e.g., every 30s or on critical events).
   - Log every signature operation with correlation ID.
   - Expose `aegis audit verify` (from CLI step 01) that checks the chain.
5. **Tests**:
   - Signing correctness + tamper detection test.
   - Static binary verification test (modify binary → daemon refuses to start).
   - Capability drop test (verify effective capabilities at runtime).

## Acceptance Criteria
- Signing logic is < 300 LOC and fully isolated.
- Daemon refuses to start on binary tampering.
- All signing operations are Merkle-audited.
- Passes `docs/specs/host-daemon.md` test requirements for Audit Root Signing.

**Dependencies**: Follows 02 (daemon refactor).
**Estimated effort**: 1 day.

**Owner**: TBD
**Status**: Ready after 02