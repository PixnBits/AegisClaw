# Host Daemon Specification

**Status:** Draft  
**Last Updated:** May 2026

## Purpose

The Host Daemon is the **only** component that runs with host-level privileges. It is deliberately kept very small and contains no business logic.

Its sole purpose is to serve as the trusted bootstrap and lifecycle manager for the entire system.

## Responsibilities

The Host Daemon is responsible for exactly these functions:

- Starting, stopping, and monitoring all sandboxed environments
- Generating and distributing Ed25519 keypairs to every microVM/sandbox
- Managing the Unix socket for CLI and TUI communication
- Signing Merkle tree roots for the tamper-evident audit log
- Acting as watchdog for critical components (AegisHub, Store VM, Network Boundary VM)

## Implementation Language

**Language:** Go

The Host Daemon is implemented in Go for consistency with the rest of the codebase and because it must support multiple sandbox backends.

**Strict Requirements:**
- Keep the daemon as small as possible (target < 2000 lines of code)
- Minimize external dependencies
- Must compile to a static binary
- Target idle memory usage under 20 MB

## Sandbox Backends

The daemon must support multiple sandbox technologies:

- **Linux**: Firecracker (primary target, strongest isolation)
- **macOS**: Docker Sandbox (`sbx`)
- **Windows**: Docker Sandbox (`sbx`)

A clean `SandboxBackend` interface must be used to abstract the differences between Firecracker and Docker Sandbox.

## Explicit Non-Responsibilities

The Host Daemon must **never**:
- Process user messages or LLM output
- Handle secrets
- Make governance decisions
- Execute generated code

## Test Requirements

The following behaviors must be enforced by automated tests:

- **Minimal Privilege**: The daemon must not have any unnecessary host capabilities
- **No Secret Handling**: The daemon must never receive, store, or transmit secrets
- **Keypair Isolation**: Private keys must never leave their assigned microVM
- **Lifecycle Containment**: If the daemon crashes, all running microVMs must be terminated
- **Memory Usage**: Idle memory usage of the daemon must remain under 20 MB
- **Static Binary**: The compiled daemon must be a fully static binary with no dynamic dependencies
- **Sandbox Isolation**: A compromised sandbox must not be able to affect the Host Daemon or other sandboxes
- **Audit Root Signing**: The daemon must correctly sign Merkle tree roots at regular intervals
- **Unix Socket Hardening**: The Unix socket must enforce strict permissions and input validation

## Dynamic Lifecycle (Collaboration Model)

The daemon (via Orchestrator + Sandbox backend) is responsible for fast on-demand spin-up and clean spin-down of role-specialised Agent VMs (Project Manager, Court personas, SDLC roles, general) and their supporting Memory VMs, with a hard target of **<1s startup** for responsive UX and low idle resource use on laptops.

See:
- `../prd/collaboration-model.md` and `../prd/runtime-architecture.md` (Dynamic Agent Lifecycle section)
- `docs/implementation-plan/collaboration-model.md` (detailed <1s tactics: pre-built .img, pre-pooled rootfs claim for agent-/memory-, parallel launches, pre-gen keys, snapshot/resume for Court, tight readiness via existing sentinels, shrinkage)
- `agent-runtime.md`, `aegishub.md`, `store-vm.md` (channel/role attachment, routing, state)
- Existing instrumentation: `AEGIS_BOOT_TIMING=1`, `GetVMBootMetrics`, console logs (`fc-*-console.log`), `/tmp/aegis-component-ready`, `scripts/boot-metrics.sh`, `aegis vm` subcommands.

Key new/expanded responsibilities (non-TCB business logic stays in sandboxes):
- `EnsureRoleAgent` / `EnsureCourtPersona` (and symmetric Release/Stop) with idle detection hooks.
- Pre-warming of pooled rootfs copies and/or golden snapshots (off hot path).
- Resource accounting + observability (per-channel or global) exposed to portal/CLI.
- Parallel launch of independent instances (e.g. paired agent+memory, the 7 Court personas).
- Strict use of pre-built raw images (conversion from tarball is a perf anti-pattern in the launch path).

All launches continue to use the established paranoid key distribution (0600 ephemeral + cmdline hex for shared images, immediate zeroing), per-VM registration with AegisHub, and the critical component watchdog.

Changes must preserve the exact start/stop mechanisms documented in AGENTS.md (`make start` / `sudo ./bin/aegis start`, `./bin/aegis stop`).

## Related Updates
See the implementation plan for phased files (orchestrator.go, guest_key_inject.go, rootfs_linux.go, firecracker.go, aegishub, store, portal, ACLs, specs, tests, E2E). Legacy per-session and eager-Court paths must continue to work during transition.

