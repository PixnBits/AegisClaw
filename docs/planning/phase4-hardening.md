# Phase 4.2 Capability Dropping - Done

- Added `dropCapabilities()` using `prctl` + `capset`.
- Keeps minimal set: `CAP_SYS_ADMIN`, `CAP_NET_ADMIN`, `CAP_SYS_CHROOT`, `CAP_DAC_OVERRIDE`.
- Called very early in `initRuntime()`.
- Non-fatal on failure (logs warning).

Capabilities are now dropped to a much smaller set.