# Package: sandbox

## Overview
The `sandbox` package implements the full Firecracker microVM sandbox lifecycle for AegisClaw. It creates isolated execution environments for skill agents, enforcing per-sandbox network policies via nftables, vsock communication channels for LLM requests, and a tamper-evident skill registry. The package covers VM specification, runtime management, network policy compilation, snapshot operations, and crash-recovery cleanup.

## Files
- `spec.go`: `SandboxSpec` and `SandboxInfo` data types; resource validation constants
- `manager.go`: `SandboxManager` interface definition
- `firecracker.go`: `FirecrackerRuntime` — full VM lifecycle using firecracker-go-sdk
- `orchestrator.go`: `Orchestrator` interface and `firecrackerOrchestrator` implementation; `NewOrchestrator` factory
- `netpolicy.go`: `PolicyEngine` — translates `NetworkPolicy` to nftables rules
- `registry.go`: `SkillRegistry` — persistent tamper-evident skill deployment tracker
- `snapshot.go`: VM snapshot creation and restoration
- `cleanup.go`: Idempotent resource cleanup for TAP devices, nftables tables, and state directories
- `firecracker_test.go`: Runtime config validation and sandbox spec enforcement tests
- `netpolicy_test.go`: nftables rule generation tests for all policy modes
- `orchestrator_test.go`: Orchestrator lifecycle tests with mock manager
- `registry_test.go`: Registry CRUD, hash chain, and integrity repair tests
- `security_test.go`: Security boundary tests — path traversal, network wildcards, resource limits

## Key Abstractions
- `SandboxSpec`: immutable desired state; populated from approved governance proposals
- `FirecrackerRuntime`/`SandboxManager`: VM lifecycle management
- `Orchestrator`: higher-level composite operations (create+start, cleanup-on-failure)
- `PolicyEngine`: deterministic nftables compiler for network isolation
- `SkillRegistry`: Merkle-chained skill deployment tracker
- `SnapshotMeta`: VM checkpoint metadata for rapid restore

## System Role
The sandbox package is the security foundation of AegisClaw. All skill code executes inside Firecracker microVMs managed by this package. Network isolation, vsock communication, and resource limits enforced here are the primary barriers preventing skill agents from affecting the host system.

## Dependencies
- `github.com/firecracker-microvm/firecracker-go-sdk`: VM management
- `internal/kernel`: audit logging
- `crypto/sha256`, `encoding/json`: hashing and persistence
- `os/exec`: `ip`, `nft` for network and cleanup operations
