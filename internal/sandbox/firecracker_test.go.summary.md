# firecracker_test.go

## Purpose
Tests for the `FirecrackerRuntime` VM lifecycle operations. Since tests cannot run actual Firecracker processes in most CI environments, these tests focus on configuration validation, state-machine behaviour, and error path handling. Tests that require the Firecracker binary are skipped when it is not present.

## Key Types and Functions
- `TestNewFirecrackerRuntime_InvalidConfig`: verifies that relative paths and missing required fields are rejected at construction
- `TestNewFirecrackerRuntime_ValidConfig`: verifies that a fully specified config creates a runtime without error
- `TestCreateSandbox`: verifies sandbox spec validation — invalid VCPU counts, memory ranges, and CID values below 3 are rejected
- `TestSandboxNotFound`: verifies appropriate errors when `Start`, `Stop`, `Delete`, or `Status` are called with unknown IDs
- `TestCIDIncrement`: verifies the CID counter increments correctly across multiple `Create` calls

## Role in the System
Catches regressions in config validation and sandbox spec enforcement without requiring root privileges or a Firecracker binary. Ensures the runtime rejects dangerous or malformed configurations before any VM is launched.

## Dependencies
- `testing`, `t.TempDir()`
- `internal/sandbox`: `FirecrackerRuntime`, `RuntimeConfig`, `SandboxSpec`
