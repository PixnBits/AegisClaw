# Phase 3.5: AegisHub Launch & Lifecycle Hardening

**Status**: Started

## Changes Made
- Introduced `launchAegisHub()` with basic health monitoring goroutine.
- Added `shutdownAegisHub()` helper for graceful shutdown.
- Integrated into `initRuntime()`.

## Remaining Work
- Full VM launch via `sandbox.FirecrackerRuntime` for AegisHub.
- Real health check implementation (vsock ping).
- Restart-on-failure logic.
- Metrics / observability hooks.

This completes the main Phase 3 handler extraction + AegisHub lifecycle work.