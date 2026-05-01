# `scripts/gen_sandbox.py` — Summary

## Purpose

A Python code-generation utility that programmatically writes two Go source files into `internal/sandbox/`: `spec.go` and `manager.go`. It uses Python string concatenation to produce correctly tab-indented Go code, working around any tooling or formatting constraints that might arise from generating Go files in a non-Go context.

## What It Generates

### `internal/sandbox/spec.go`

Defines the core data types and validation logic for Firecracker sandbox specifications:

| Type | Description |
|---|---|
| `SandboxState` | Lifecycle state enum: `created`, `running`, `stopped`, `error` |
| `Resources` | vCPU count and memory limits for a microVM |
| `NetworkPolicy` | Default-deny network rules with optional allowed hosts (IP/CIDR), ports, and protocols |
| `SandboxSpec` | Full desired-state specification for a Firecracker sandbox (ID, name, resources, network policy, vsock CID, rootfs path, kernel path, workspace size) |
| `SandboxInfo` | Runtime state snapshot (spec, state, PID, timestamps, socket path, TAP device, host/guest IPs) |
| `RuntimeConfig` | Paths to `firecracker`, `jailer`, kernel image, rootfs template, chroot base, and state directory |

Both `SandboxSpec.Validate()` and `RuntimeConfig.Validate()` enforce: non-empty IDs, name regex (`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`), vCPU range 1–32, memory range 128–32768 MB, vsock CID ≥ 3, absolute paths, and `NetworkPolicy.DefaultDeny == true`.

### `internal/sandbox/manager.go`

Defines the `SandboxManager` interface with six lifecycle methods: `Create`, `Start`, `Stop`, `Delete`, `List`, `Status`.

## Fit in the Broader System

Running `python scripts/gen_sandbox.py` regenerates the foundational sandbox package files. The generated `SandboxManager` interface is implemented by the Firecracker sandbox manager in `internal/sandbox`. This script is a one-time or occasional developer utility — not part of the normal build pipeline.

## Notable Dependencies

- Python 3 standard library only (`os`)
- Output path: `internal/sandbox/` relative to the repository root
