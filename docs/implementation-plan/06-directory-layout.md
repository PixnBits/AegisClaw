# 06 - Directory & Filesystem Layout Implementation

**Goal**: Implement the single-root `~/.aegis/` layout defined in the new specification `docs/specs/directory-layout.md`. All paths must be created with correct paranoid-security permissions on first run.

## Why This Matters
Users should only ever see **one** new directory (`~/.aegis/`). Scattering files across `~/.config/`, `~/.local/share/`, `~/.cache/`, etc. creates surprise and increases the chance of misconfiguration or accidental exposure. Security requires strict permission enforcement from day one.

## Tasks

1. **Create centralized path constants**
   - Add `internal/paths/` package with constants:
     - `AegisRoot`, `ConfigDir`, `SocketDir`, `VMDir`, `GitDir`, `LogsDir`, `DataDir`, `SecretsDir`, `WorkspaceDir`, `CacheDir`, `RunDir`
   - Support XDG fallbacks where they don't conflict with security.

2. **Implement directory creation helper**
   - `EnsureDirectories()` function called early in daemon startup.
   - Create all required dirs with **exact permissions** from the spec table.
   - Use `os.MkdirAll` with explicit mode + `chmod` + ownership checks.
   - Add `aegis doctor --fix-permissions` command (from CLI step 01).

3. **Wire existing components to new paths**
   - Update Host Daemon, AegisHub, Store VM, etc. to use the new constants.
   - Migrate any hard-coded paths from v1.
   - Update `configuration-management.md` references if needed.

4. **Secrets & sensitive data protection**
   - `secrets/` must be created as 0700 and owned by the daemon process/group.
   - Daemon must refuse to start (or enter safe-mode) if `secrets/` or `data/store/` have world-readable permissions.
   - Add runtime check + audit event on startup.

5. **Tests**
   - Unit tests for path constants and permission enforcement.
   - Integration test: fresh install → verify exact directory tree + permissions.
   - Security test: attempt to start daemon with overly permissive `secrets/` → startup fails + clear error.
   - `aegis doctor` test for permission repair.

## Acceptance Criteria
- All AegisClaw data lives under a single `~/.aegis/` root (no user surprise).
- Every directory is created with the exact permissions and ownership defined in `docs/specs/directory-layout.md`.
- Daemon refuses to start on insecure permissions for `secrets/` or `data/store/`.
- `aegis doctor --fix-permissions` works reliably.
- Full test coverage for path creation and security checks.

**Dependencies**: Follows 02 (daemon refactor) and 05 (socket hardening). Can run in parallel with workspace customization (future step 07).
**Estimated effort**: 1 day.

**Owner**: TBD
**Status**: Ready to start