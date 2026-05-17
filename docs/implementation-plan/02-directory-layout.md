# 02 - Directory & Filesystem Layout Implementation

**Goal**: Implement the security-conscious layout defined in `docs/specs/directory-layout.md`, with special handling for the two highest-risk items: the privileged daemon socket and the secrets vault.

## Security Context (Why This Step Exists)
Putting everything under `~/.aegis/` is great for user experience, but creates real attack surface for root-privileged components. This task ensures we apply **extra protection** where it matters most.

## Tasks

1. **Implement path constants with security distinctions**
   - `internal/paths/` package with two categories:
     - User-writable: `ConfigDir`, `WorkspaceDir`, `CacheDir`, `LogsDir`, `GitDir`, `VMDir`, `DataDir`
     - High-sensitivity / privileged: `SecretsDir`, `SocketPath` (points to `/run/user/$UID/aegis/daemon.sock` or abstract socket)

2. **Socket placement (critical)**
   - On Linux: Create and bind socket at `/run/user/$UID/aegis/daemon.sock` (tmpfs).
   - Fall back to `~/.aegis/run/daemon.sock` only on non-Linux with strict ACLs.
   - **Never** place the privileged socket directly under `~/.aegis/`.

3. **Secrets vault protection (critical)**
   - Create `~/.aegis/secrets/` as 0700 owned by daemon/`aegis` group.
   - On **every access** (including startup):
     - Use `O_NOFOLLOW`
     - Verify ownership + permissions
     - Refuse + emit audit event if incorrect
   - (Future) Consider moving vault to `/var/lib/aegis/secrets/` for even stronger protection.

4. **Directory creation + enforcement helper**
   - `EnsureSecureDirectories()` called early in daemon startup.
   - Runtime checks on `secrets/`, `data/store/`, `data/audit/`.
   - Daemon refuses to start (or enters safe-mode) on insecure permissions.
   - Add `aegis doctor --fix-permissions` support.

5. **Tests**
   - Permission creation + enforcement tests.
   - TOCTOU / symlink attack resistance test for socket location.
   - Secrets vault access control test (wrong permissions → refusal + audit).
   - Non-root CLI still works with new socket location.

## Acceptance Criteria
- Most user data lives under single `~/.aegis/` root.
- Privileged socket is **never** under `~/.aegis/` (uses `/run/user/$UID/aegis/` or abstract socket).
- Secrets vault has **mandatory runtime ownership/permission enforcement** on every access.
- Daemon refuses to start on insecure permissions for high-sensitivity directories.
- Full test coverage including attack scenarios.

**Dependencies**: Follows 01 (CLI basics)
**Estimated effort**: 1–1.5 days.

**Owner**: TBD
**Status**: **Completed** (implementation already present in `internal/paths/`, `internal/config/`, `cmd/aegisclaw/`, and `internal/api/`; verified against spec; legacy migration code removed for pre-release simplification)

---

## Completion Notes (May 2026)

The core implementation was already in place and aligned with the paranoid security requirements:

- `internal/paths/paths.go` provides the canonical `Layout`, `DefaultLayout()`, `DefaultSocketPath()` (Linux: `/run/user/$UID/aegis/daemon.sock`), `EnsureSecureDirectories()`, `VerifySensitiveDir()` (O_NOFOLLOW + ownership + mode checks), `FixSecurePermissions()`, and attack-resistant directory creation.
- Integrated at daemon startup (`runtime.go`), in `doctor --fix-permissions`, and API server socket binding.
- Config uses the paths package for all defaults; single `~/.aegis/` root enforced.

As a final pre-release simplification step on this branch:
- Removed all legacy path migration code (`normalizeConfigPaths`, `migrateLegacyPath`, etc.) and related tests from `internal/config/`.
- This reduces code in the config loading path with no impact on new installs (all configs are now created fresh with secure defaults).

All acceptance criteria met. Task 02 is complete and unblocks `03-daemon-minimal-tcb-refactor.md`.