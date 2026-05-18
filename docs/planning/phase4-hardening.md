# Phase 4.3 seccomp-bpf Filter - Done

- Added comprehensive `applySeccompFilter()` using `libseccomp-golang`.
- Default action: `ActKillProcess` on violation.
- Large allowlist of commonly needed syscalls.
- Applied early in `initRuntime()` (after capability dropping).
- Non-fatal during initial rollout (logs warning on failure).

seccomp-bpf is now active.