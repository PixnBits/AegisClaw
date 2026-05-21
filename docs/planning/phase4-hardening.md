# Phase 4: Host Daemon Hardening

## Summary of Changes

Phase 4 focused on reducing the attack surface and improving containment of the Host Daemon.

### Completed Items

| #  | Area                        | Key Changes                                                          | Status      |
|----|-----------------------------|----------------------------------------------------------------------|-------------|
| 1  | Lifecycle Containment       | Signal handling + aggressive VM termination on exit + stale VM cleanup on startup | Complete |
| 2  | Capability Dropping         | Early drop to minimal set (`CAP_SYS_ADMIN`, `CAP_NET_ADMIN`, etc.)  | Complete |
| 3  | seccomp-bpf                 | Placeholder filter scaffolded; a production-grade strict default-deny allowlist is deferred to a follow-up PR | **Partial / Placeholder** |
| 4  | Static Binary               | `make build-static` target with `CGO_ENABLED=0`                     | Complete |
| 5  | Unix Socket Hardening       | Strict `0700` directory + `0600` socket permissions + secure creation helper | Complete |

## Overall Impact

The Host Daemon is now meaningfully hardened:
- Much smaller capability set
- Syscall filtering scaffolded (strict production filter is a planned follow-up)
- Better process lifecycle containment
- Static binaries encouraged
- Tighter Unix socket defaults

Phase 4 delivers meaningful hardening while remaining practical to build and run. The seccomp filter is currently a placeholder and should be replaced with a strict default-deny allowlist before production deployment.