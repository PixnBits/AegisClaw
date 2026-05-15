# 05 - Runtime Permission Enforcement

**Goal**: Add mandatory runtime `O_NOFOLLOW` + ownership/permission verification on every access to high-sensitivity directories (`secrets/`, `data/store/`, `data/audit/`). The daemon must refuse to operate if permissions are incorrect.

## Why This Matters (Paranoid Security)
Even if we create directories with correct permissions at startup, a user or attacker can later change them. We must enforce security **on every access**, not just at creation time.

## Tasks

1. **Create secure path access helper**
   - `internal/paths/secure.go` with functions like:
     - `SecureOpen(path string, flags int) (*os.File, error)`
     - `VerifyDirectory(path string, expectedMode os.FileMode, expectedOwner int) error`
   - Always use `O_NOFOLLOW` for sensitive paths.

2. **Integrate checks into critical paths**
   - On every access to `secrets/`, `data/store/`, `data/audit/`
   - On startup (fail fast if insecure)
   - Before any secret read/write or audit log append

3. **Daemon refusal behavior**
   - If permissions are wrong: log critical error + enter safe-mode or refuse to start
   - Emit Merkle-signed audit event

4. **Update `aegis doctor --fix-permissions`**
   - Make it actually repair common issues (chmod, chown)

5. **Tests**
   - Attack simulation: change permissions on `secrets/` → daemon refuses + audits
   - Symlink attack test on sensitive paths
   - Normal operation still works with correct permissions

## Acceptance Criteria
- Every access to `secrets/`, `data/store/`, `data/audit/` performs ownership + permission verification.
- Daemon refuses to start or enters safe-mode on insecure permissions.
- Full test coverage including attack scenarios.

**Dependencies**: Follows 02 (directory layout)
**Estimated effort**: 1 day.

**Owner**: TBD
**Status**: Ready to start