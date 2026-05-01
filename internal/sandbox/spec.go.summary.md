# spec.go

## Purpose
Defines the core data types that describe a sandbox specification and its runtime state. These types are the shared vocabulary used across all other sandbox files — they represent what a sandbox should look like (`SandboxSpec`) and what it currently looks like (`SandboxInfo`). Separating spec from implementation ensures that policy, runtime, and manager code can all reference the same canonical types without import cycles.

## Key Types and Functions
- `SandboxSpec`: full sandbox descriptor — ID, Name, Resources (VCPUs 1–32, MemoryMB 128–32768), NetworkPolicy, SecretsRefs, VsockCID (≥3), RootfsPath, KernelPath, WorkspaceMB
- `SandboxInfo`: runtime state — PID, StartedAt, StoppedAt, TapDevice, HostIP, GuestIP, SocketPath
- `NetworkPolicy`: NoNetwork (bool), DefaultDeny (must be true), AllowedHosts, AllowedPorts, AllowedProtocols, EgressMode (`"proxy"` or `"direct"`)
- `Resources`: VCPUs and MemoryMB with validation bounds
- Validation constants: `MinVCPUs = 1`, `MaxVCPUs = 32`, `MinMemoryMB = 128`, `MaxMemoryMB = 32768`

## Role in the System
`SandboxSpec` is created by the orchestrator when launching a new skill sandbox, populated from the approved governance proposal. `SandboxInfo` is populated by `FirecrackerRuntime` when a VM is started and used by the TUI status dashboard to display live sandbox state.

## Dependencies
- Standard library only: `time` for timestamps in `SandboxInfo`
