# `builder_test.go` — Builder Runtime Tests

## Purpose
Unit tests for `BuilderSpec`, `BuilderConfig`, `BuilderInfo`, and `BuilderRuntime` configuration logic. Tests exercise validation rules and constructor behaviour without requiring a running Firecracker hypervisor.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestDefaultBuilderSpec` | Checks all field defaults (name format, vCPUs, memory, workspace, allowed hosts/ports, proposal ID). |
| `TestBuilderSpecValidation` | Table-driven: empty ID, empty name, out-of-range vCPUs, out-of-range memory, out-of-range workspace, empty proposal ID each return the expected error. |
| `TestBuilderConfigDefaults` | Validates the default config passes its own `Validate()` check. |
| `TestBuilderConfigValidation` | Empty rootfs, empty workspace dir, out-of-range concurrent builds, and out-of-range timeout each fail validation. |
| `TestNewBuilderRuntimeValidation` | Nil `FirecrackerRuntime` or nil kernel returns an error. |
| `TestBuilderStateConstants` | Asserts string values of `BuilderState` constants. |

## How It Fits Into the Broader System
Guards the configuration layer so that misconfigured builder runtimes fail fast at construction time rather than at the point of a VM launch.

## Notable Dependencies
- Standard library `testing`, `time`.
