# Runtime Architecture

## Core Principle

**If a component touches untrusted data or performs complex logic, it must run inside its own Firecracker microVM.**

The only code that runs directly on the host is a tiny, minimal daemon.

## Host Daemon (Trusted Computing Base)

The daemon is responsible for **only**:

- Starting, stopping, and monitoring Firecracker microVMs
- Managing the Unix socket for the CLI/TUI
- Signing Merkle tree roots for the audit log
- Acting as bootstrap and watchdog for AegisHub

## MicroVM Components

All other functionality runs in isolated microVMs:

- **AegisHub** — The only privileged router. Enforces strict ACLs between all microVMs.
- **Store VM** — Owns all persistent storage (proposals, audit logs, composition history)
- **Tool Handler VMs** — Each major tool or skill category runs in its own VM
- **LLM Proxy VM** — Handles secret injection and prompt sanitization
- **Court Scribe VM** — Observes conversations and produces structured summaries
- **Court Member VMs** — Each of the five Governance Court personas runs in its own VM
- **Agent Runtime VMs** — Individual agent instances

## Philosophy

This architecture ensures that a compromise in any single component cannot spread. The trusted computing base is kept to an absolute minimum.

Every major function has its own security boundary.
