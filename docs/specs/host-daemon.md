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

**See also:** [docs/implementation-plan/03-daemon-minimal-tcb-refactor.md](../implementation-plan/03-daemon-minimal-tcb-refactor.md) — requirements traceability matrix (maps each bullet above to tests, CI tier, and gap backlog).

