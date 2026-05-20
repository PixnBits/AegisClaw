# 04 - Unix Socket Hardening (Avoid Docker Single-Socket Anti-Pattern)

**Goal**: Replace any single privileged Unix socket pattern (the classic Docker `/var/run/docker.sock` risk) with a hardened, least-privilege design. Every connection must be strongly authenticated and authorized; the CLI must **never** require root.

## Why This Is Critical (Paranoid Security)
Docker's single socket is a well-known attack vector: any process that can write to it gains full host control. Per `docs/specs/host-daemon.md` (Unix Socket Hardening test requirement) and `docs/architecture.md` (strict mediation), we must prevent this entirely.

## Traceability

Socket-related **test requirement rows** and their CI status live in [03-daemon-minimal-tcb-refactor.md](03-daemon-minimal-tcb-refactor.md) (Section 2.1, Unix socket hardening). Prioritized gaps for peer credentials, rate limits, and audit-on-deny are tracked as **DB-05** and **DB-06** in [docs/planning/daemon-test-backlog.md](../planning/daemon-test-backlog.md).

## Tasks

1. **Design & implement hardened socket model**:
   - Use a **dedicated non-root group** (e.g., `aegis`) for socket ownership.
   - Socket permissions: `0700` or `0750` (owner/group only).
   - Prefer **abstract Unix sockets** (e.g., `@aegis-daemon`) or a path under `/run/aegis/` with tight mount namespace isolation where possible.
   - **Never** bind as world-writable or allow arbitrary processes to connect.

2. **Per-connection authentication & authorization**:
   - Use `SO_PEERCRED` (or `SCM_CREDENTIALS`) on every connection to verify the client's UID/GID/PID in real time.
   - Maintain an allow-list of permitted UIDs (non-root CLI users + specific service accounts).
   - Reject connections from root or unexpected processes with a clear audit event.
   - Add simple capability tokens or signed requests for sensitive operations (e.g., `start`, `safe-mode`).

3. **Input validation & rate limiting**:
   - Strict protobuf/JSON schema validation on every message.
   - Rate limit per UID/PID (e.g., 10 req/sec) with back-pressure.
   - Maximum message size enforcement to prevent DoS.

4. **Non-root CLI support**:
   - CLI binary (`aegisclaw`) must run as normal user.
   - Only the daemon binds the socket (requires root or `CAP_NET_ADMIN` + `CAP_SYS_ADMIN` for Firecracker, but drops them immediately after).
   - Provide clear error messages and `aegis doctor` guidance if permissions are wrong.

5. **Auditing & monitoring**:
   - Log every connection attempt (success/failure) with UID, PID, command, and correlation ID (Merkle-signed).
   - Expose socket stats via `aegis status --socket`.

6. **Tests** (from `docs/specs/host-daemon.md` + new requirements):
   - Unauthorized access test (non-allowed UID → immediate reject + audit log).
   - Permission test (verify socket mode 0700/0750 and group ownership).
   - `SO_PEERCRED` verification test (spoofed credentials rejected).
   - Rate-limit & size-limit enforcement tests.
   - Non-root CLI end-to-end test (`aegis status` as normal user succeeds).
   - No world-writable socket regression test.

## Acceptance Criteria
- No single privileged socket that any process can abuse (Docker anti-pattern eliminated).
- Every connection is authenticated via `SO_PEERCRED` + allow-list.
- CLI runs 100% as non-root user.
- All new behaviors pass the socket hardening tests in `docs/specs/host-daemon.md`.
- Clear audit trail for every connection attempt.

**Dependencies**: Follows 02 (directory layout) and 03 (daemon TCB)
**Estimated effort**: 1.5–2 days (high security ROI).

**Owner**: TBD
**Status**: Ready to start (directly addresses user concern about Docker socket risks)