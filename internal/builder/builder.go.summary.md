# `builder.go` — Builder Runtime & Sandbox Management

## Purpose
Defines `BuilderRuntime`, which manages a pool of dedicated Firecracker MicroVM sandboxes used exclusively for building and analyzing skill code. It extends the base `sandbox.FirecrackerRuntime` with builder-specific configuration, a concurrency semaphore, and kernel audit logging.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `BuilderState` | Enum: `idle`, `building`, `stopped`, `error`. |
| `BuilderSpec` | Per-build sandbox parameters: ID, name, vCPUs (1–8), memory (256–8192 MB), workspace size, rootfs path, allowed hosts/ports, proposal ID. |
| `DefaultBuilderSpec(proposalID)` | Returns sensible production defaults (2 vCPUs, 1 GB RAM, 512 MB workspace, only localhost:11434 outbound). |
| `BuilderConfig` | Runtime-level config: rootfs template path, workspace base dir, max concurrent builds, build timeout. |
| `DefaultBuilderConfig()` | Production defaults: builder rootfs at `/var/lib/aegisclaw/rootfs-templates/builder.ext4`. |
| `BuilderRuntime` | Owns a `map[string]*BuilderInfo`, a semaphore channel, and delegates to `sandbox.FirecrackerRuntime`. |
| `LaunchBuilder` | Acquires a semaphore slot, creates and starts the sandbox, records `BuilderInfo`, and emits an `ActionBuilderStart` audit event. |
| `SendBuildRequest` | Forwards a `kernel.ControlMessage` to the specified builder via the kernel control plane. |
| `StopBuilder` | Stops and deletes the sandbox, releases the semaphore slot, and emits `ActionBuilderStop`. |
| `Status`, `ListBuilders`, `ActiveBuilders`, `Cleanup` | Introspection and graceful-shutdown helpers. |

## How It Fits Into the Broader System
`BuilderRuntime` is instantiated by the daemon and injected into `Pipeline`, `CodeGenerator`, and `Analyzer`. All code generation and analysis traffic passes through it.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/sandbox` — `FirecrackerRuntime`, `SandboxSpec`.
- `github.com/PixnBits/AegisClaw/internal/kernel` — audit logging and control plane.
- `go.uber.org/zap`, `github.com/google/uuid`.
