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

**This document will be updated as Phase 4 progresses.**