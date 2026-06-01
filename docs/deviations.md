# Known Deviations and Technical Compromises

This document records intentional deviations from the specifications and design documents. The goal is transparency so these can be revisited and improved later as the system matures.

Each entry includes:
- What the spec/design says
- What the current implementation does
- Rationale for the deviation
- Risks / downsides
- Planned or desired future resolution

---

## 1. Control Socket Location and Permissions

**Status:** Active deviation (as of May 2026)

### Specification

- `docs/specs/cli.md` (Connection Model):
  > The CLI connects **exclusively** to the Host Daemon via a **Unix domain socket** (`~/.aegis/daemon.sock` on Linux/macOS...).

- `docs/specs/cli.md` (Persistent Data Storage):
  - `~/.aegis/` should be `chmod 0700` and user-owned.
  - The socket lives inside this directory.

- `docs/specs/host-daemon.md` (Test Requirements):
  > **Unix Socket Hardening**: The Unix socket must enforce **strict permissions** and input validation.

- Multiple implementation plans (phase-0-foundations, 02-host-daemon-skeleton, etc.) reference the socket at `~/.aegis/daemon.sock`.

### Current Implementation

- The control socket is created at `/tmp/aegis/daemon.sock`.
- The directory is created with `0755` (or `0777` for the PID file subdir).
- The socket file is set to mode `0666` (world-writable) and chowned to the original invoking user (via `SUDO_USER`).
- The PID file is also placed in `/tmp/aegis/`.

See:
- `cmd/aegis/main.go` (`init()`, `ensureStateDir()`, `writePIDFile()`, `startSocketServer()`)
- The explicit comment: *"Use /tmp for the PID file so it's accessible to both root and non-root users"*

### Rationale

The Host Daemon must run as root on Linux when using real Firecracker microVMs (for KVM access, creating tap devices, etc.).

Per the CLI spec, only `aegis start` (and initial setup) should require elevated privileges. Normal users must be able to run `status`, `vm list`, `stop`, `chat`, etc.

A filesystem socket created by a root process inside `~/.aegis/daemon.sock` ends up owned by root with restrictive permissions. The normal user cannot connect without additional mechanisms (dedicated group + setgid helper, capabilities, etc.).

Placing the socket in `/tmp` with relaxed permissions was the simplest pragmatic solution to keep the CLI usable during development while the real Firecracker bring-up was in progress.

### Risks / Downsides

- Violates the explicit socket location in `cli.md`.
- Weaker security posture than a user-owned socket inside a `0700 ~/.aegis/` directory (`/tmp` is a shared, attacker-influencable namespace).
- Goes against the "strict permissions" hardening requirement in `host-daemon.md`.
- Creates a larger attack surface for the control interface.
- The current `0666` socket means any local user can send basic commands (including `stop`).

### Future Resolution Path

**Preferred direction (Linux):** Use **abstract Unix sockets** (names prefixed with `\0`).

- Abstract sockets have no filesystem representation, so there are no permission or ownership problems.
- A root daemon can listen on an abstract name that any user on the machine can connect to.
- This cleanly satisfies both the "daemon runs as root" requirement and the "normal user can use the CLI" requirement without relaxing filesystem permissions or using `/tmp`.

Non-Linux platforms (macOS/Windows with Docker sandboxes) would continue to use a conventional filesystem socket at `~/.aegis/daemon.sock`.

This deviation is being actively addressed. As of this commit we have introduced OS-specific build handling (`socket_linux.go`, `socket_default.go`, and readiness helpers) that defaults to abstract Unix sockets on Linux. The filesystem `/tmp` path and relaxed permissions logic are now conditional. Full migration (including updating the CLI connection paths and removing the old /tmp fallback) is tracked here for future cleanup.

**Tracking:** This is the highest-priority socket-related deviation.

---

## 2. Other Noted Drifts (to be expanded)

- State directory for control artifacts vs. user data (`/tmp/aegis` vs `~/.aegis`).
- Effective home directory logic under sudo (see `getEffectiveHomeDir()` in `internal/config/config.go`).

---

**How to use this document**

When a new compromise is accepted, add an entry here with the same structure. When a deviation is resolved, mark it as **Resolved** with the commit or PR that addressed it and move it to a "Resolved" section (or delete it after updating the relevant specs).

This document exists to prevent "we'll fix it later" from becoming permanent technical debt.