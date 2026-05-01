# orchestrator.go

## Purpose
Implements the `Orchestrator` interface and its factory function. The orchestrator provides a higher-level API for sandbox lifecycle management, combining the low-level `SandboxManager` operations (create + start) into single composite actions and adding best-effort cleanup on failure. It is the primary entry point for the daemon and CLI to launch and manage skill sandboxes.

## Key Types and Functions
- `Orchestrator` interface: `Mode() string`, `LaunchSandbox(ctx, SandboxSpec) (*SandboxInfo, error)`, `StopSandbox(ctx, id) error`, `DeleteSandbox(ctx, id) error`, `SandboxStatus(ctx, id) (*SandboxInfo, error)`, `ListSandboxes(ctx) ([]SandboxInfo, error)`, `SendToSandbox(ctx, id, payload) ([]byte, error)`
- `NewOrchestrator(mode string, cfg RuntimeConfig, logger) (Orchestrator, error)`: factory; currently only `"firecracker"` mode is supported; returns error for unknown modes
- `firecrackerOrchestrator`: concrete implementation wrapping `FirecrackerRuntime`
- `LaunchSandbox`: calls `Create` then `Start`; on `Start` failure, attempts `Delete` as cleanup

## Role in the System
The orchestrator is the single interface used by the daemon control plane (`internal/kernel`), the TUI status dashboard, and the CLI commands to manage skill sandbox VMs. By abstracting over `FirecrackerRuntime`, it enables future support for alternative isolation technologies.

## Dependencies
- `internal/sandbox`: `FirecrackerRuntime`, `SandboxSpec`, `SandboxInfo`
- `internal/kernel`: for audit logging of launch/stop events
- `context`, `fmt`: standard orchestration utilities
