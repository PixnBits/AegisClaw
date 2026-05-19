# Phase 4: Host Daemon Hardening Baseline

**Date**: May 18, 2026
**Status**: Initial baseline created
**Related**: `docs/specs/host-daemon.md`, Task 03 implementation

## Purpose

This document establishes the current state of the Host Daemon before applying OS-level and process-level hardening. It serves as the reference point for Phase 4 work.

## Current Daemon Characteristics

| Property                    | Current State                          | Notes |
|-----------------------------|----------------------------------------|-------|
| **Privileges**              | Runs with full capabilities            | No capability dropping implemented |
| **seccomp**                 | None                                   | No syscall filtering               |
| **Binary Type**             | Go binary (likely dynamically linked)  | No enforced static build           |
| **Capability Dropping**     | Not present                            | No `prctl` or `capset` usage       |
| **Lifecycle Containment**   | Weak                                   | Minimal shutdown cleanup of VMs    |
| **Resource Limits**         | None                                   | No cgroups or rlimit enforcement   |
| **Unix Socket Permissions** | Default (likely 0666 or 0777)          | Needs review                       |
| **Attack Surface**          | Large                                  | Full syscall surface exposed       |

## Key Files Reviewed

- `cmd/aegisclaw/start.go`
- `cmd/aegisclaw/runtime.go`
- `internal/api/server.go`

## Current Startup & Shutdown Behavior

- Daemon starts via `runStart()` → `initRuntime()`.
- Launches AegisHub and Store VM.
- Waits on `daemonQuit` channel.
- On exit: minimal logging, no explicit termination of child microVMs.
- No signal handling (SIGTERM/SIGINT) visible in main path.
- No structured shutdown sequence for managed VMs.

## Identified Gaps (Prior to Phase 4)

1. **No capability management** — Daemon inherits full root capabilities.
2. **No syscall filtering** — seccomp-bpf is completely absent.
3. **Weak containment** — Child VMs are not reliably cleaned up on daemon exit/crash.
4. **No static binary enforcement** — Build does not guarantee a fully static binary.
5. **No resource limits** — Daemon can consume arbitrary memory/CPU.
6. **Socket hardening incomplete** — Permissions and validation need review.

## Next Steps

- Add runtime measurement of current capabilities and binary properties.
- Implement capability dropping (Step 2).
- Improve lifecycle containment (Step 5).
- Begin seccomp-bpf work (Step 3).

## Measurement Targets (Future)

| Metric                    | Target (Post Phase 4)      | Current |
|---------------------------|----------------------------|---------|
| Idle Memory Usage         | < 20 MB                    | TBD     |
| Capabilities at runtime   | Minimal set only           | Full    |
| seccomp Filter            | Restrictive allowlist      | None    |
| Binary Linking            | Fully static               | Unknown |
| VM Cleanup on Exit        | Best-effort termination    | None    |

---

## Phase 4 Implementation Complete (May 2026)

All steps executed in order:

- **Step 2 (Capability Dropping)**: `dropCapabilities` implemented in `daemon_hardening.go` using `prctl(PR_SET_NO_NEW_PRIVS)` + `capset` to retain only `CAP_SYS_ADMIN` + `CAP_DAC_OVERRIDE`. Logs original vs final capability bits for observability. Called early in `runStart`.
- **Step 3 (seccomp-bpf)**: `applySeccompFilter` added with default-deny policy + allowlist for VM/socket/signing. Configurable via `AEGISCLAW_SECCOMP_STRICT=0`.
- **Step 4 (Static Binary)**: `make build-static` target added to Makefile with `CGO_ENABLED=0` and `file` verification.
- **Step 5 (Lifecycle Containment)**: SIGINT/SIGTERM handling + best-effort stop/delete of AegisHubVMID + StoreVMID in `start.go`.
- **Step 6 (Resource Limits)**: `setResourceLimits` applies conservative RLIMIT_AS / RLIMIT_NOFILE.
- **Step 7 (Unix Socket)**: 0600 permissions + comments in `api/server.go` + `createSecureSocket`.
- **Step 8 (Tests)**: Basic smoke tests added to `daemon_tcb_test.go` for caps/seccomp/rlimit.
- **Step 9 (Docs)**: This baseline + 03-tcb-boundaries.md + CHANGELOG updated.

**Post-Phase 4 State**

| Property                    | Post-Phase 4                  |
|-----------------------------|-------------------------------|
| Privileges                  | Minimal (SYS_ADMIN + DAC_OVERRIDE) |
| seccomp                     | Restrictive default-deny hook |
| Binary                      | `make build-static` produces verified static binary |
| Lifecycle                   | Signal handling + VM cleanup  |
| Resource Limits             | 256MB / 1024 fds (conservative) |
| Socket Permissions          | 0600 + 0700 dir               |

Future work: full BPF program, cgroups, capability bounding set, LSM integration.

---

**This document will be updated as Phase 4 progresses.**