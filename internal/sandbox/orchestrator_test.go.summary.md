# orchestrator_test.go

## Purpose
Tests for the `Orchestrator` interface and its `firecrackerOrchestrator` implementation. Uses a mock `SandboxManager` to test orchestrator logic in isolation from the actual Firecracker runtime, enabling tests to run without root privileges or a Firecracker binary.

## Key Types and Functions
- `mockSandboxManager`: test double implementing `SandboxManager`; records calls and returns configurable errors
- `TestNewOrchestrator_UnknownMode`: verifies that an unknown mode (e.g., `"kata"`) returns an error
- `TestLaunchSandbox_Success`: verifies `LaunchSandbox` calls `Create` then `Start` in order and returns `SandboxInfo`
- `TestLaunchSandbox_StartFails`: verifies that when `Start` fails, `Delete` is called for cleanup and the error is propagated
- `TestStopSandbox`, `TestDeleteSandbox`: verify delegation to the underlying manager
- `TestListSandboxes`, `TestSandboxStatus`: verify correct pass-through of manager responses
- `TestSendToSandbox`: verifies vsock message forwarding to the runtime

## Role in the System
Ensures the orchestrator's composite lifecycle logic (create+start, cleanup-on-failure) is correct without requiring a real VM environment. Guards against regressions in the daemon's primary sandbox management path.

## Dependencies
- `testing`
- `internal/sandbox`: `Orchestrator`, `NewOrchestrator`, `SandboxManager`, `SandboxSpec`, `SandboxInfo`
