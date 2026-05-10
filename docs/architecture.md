# Architecture

## Overview & Core Principles

AegisClaw is built on the explicit assumption that **all external input is hostile** — including user messages, LLM responses, generated code, network data, and newly created skills.

The system enforces one fundamental rule:

> **Any component that processes untrusted data or performs non-trivial logic must run inside its own dedicated isolated sandbox.**

On **Linux** this is a Firecracker microVM. On **macOS and Windows** this is a Docker Sandbox (`sbx` — not full containers).

This creates strong, enforceable security boundaries at every major system interface.

### Core Principles

- **Minimal Trusted Computing Base (TCB)**: Only a very small, auditable host daemon runs with host-level privileges. All other functionality lives inside isolated sandboxes.
- **Strict Mediation**: No sandbox may communicate directly with another. All communication is mediated and ACL-enforced by AegisHub.
- **Complete Secret Isolation**: Secrets are never exposed to the LLM, agents, skill code, logs, or prompts.
- **Mandatory Governance**: Every change to the system must be proposed, reviewed, and approved by the Governance Court.
- **Full Auditability**: Every action taken by any component is recorded in a tamper-evident, cryptographically signed Merkle tree audit log.
- **Defense in Depth**: Isolation, mandatory review, least privilege, and tamper-evident logging are all layered together.

**Explicit Design Trade-off**:  
We deliberately accept higher complexity and slightly reduced performance in exchange for strong, verifiable containment of compromise.

## Runtime Architecture

The system is composed of one small trusted host component and multiple isolated sandboxes.

### Host Daemon (Minimal Trusted Computing Base)

The host daemon is intentionally kept extremely small. Its only responsibilities are:

- Starting, stopping, and monitoring isolated sandboxes (Firecracker on Linux, Docker Sandbox on macOS/Windows)
- Managing the Unix socket for the CLI and TUI
- Signing Merkle tree roots for the tamper-evident audit log
- Serving as the bootstrap and watchdog for AegisHub

### Sandboxed Components

- **AegisHub** — The central privileged router. Enforces ACLs and routes all communication between sandboxes.
- **Network Boundary VM** — The only component allowed to handle secrets. Runs Envoy and strictly enforces `network-access.yaml` declarations.
- **Store VM** — Owns all persistent data (proposals, audit logs, skill registry, and composition history).
- **Memory VM** — One instance per agent. Manages both short-term conversation context and long-term memory.
- **Court Scribe VM** — Observes conversations and generates structured summaries for the Governance Court.
- **Governance Court VMs** — Seven isolated sandboxes (one for each persona).
- **Agent Runtime VMs** — Where the actual agent reasoning occurs. These are stateless.
- **Builder VMs** — Ephemeral sandboxes spun up per skill proposal to implement and build new skills.
- **Web Portal VM** — Dedicated sandbox for the rich collaborative web interface.

Each sandbox has a single, narrowly defined responsibility and a hard security boundary.

## Communication & Mediation

No sandbox may communicate directly with any other sandbox. All communication is strictly mediated by AegisHub.

### LLM Connectivity

**All Agent Runtime VMs must route LLM requests (local Ollama or remote providers) through the Network Boundary VM.**

This enforces uniform network policy, rate limiting, auditing, and domain allow-listing. Direct connections from Agent Runtime VMs to Ollama are prohibited for security reasons.

`localhost:11434` (local Ollama) is whitelisted by default in the Network Boundary for Agent Runtime VMs.

### Communication Rules

- Every sandbox has exactly one vsock connection to AegisHub.
- The **Store VM** is the only component allowed to persist long-term data.
- The **Network Boundary VM** is the only component allowed to make outbound network requests (including to Ollama).
- The **Memory VM** is the only component allowed to hold conversation state.
- **Agent Runtime VMs** may only communicate with their paired Memory VM, the Court Scribe, and AegisHub.
- **Court VMs** may only communicate with the Court Scribe, their paired Agent (during reviews), and AegisHub.

## Data Flow Example: Skill Creation via SDLC

... (rest of the file unchanged) ...