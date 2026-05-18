# Phase 3.5 Update - Actual Firecracker Launch

**Major Progress:**

- Added `internal/sandbox/aegishub_vm_spec.go` with `DefaultAegisHubVMSpec()`.
- `launchAegisHub()` now performs real Firecracker VM creation and start using `sandbox.FirecrackerRuntime`.
- `AegisHubMonitor` stores VM reference for proper shutdown.
- Client connection happens after successful VM launch.

This brings AegisHub launch in line with the Store VM pattern.