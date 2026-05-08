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

Each sandbox has a single, narrowly defined responsibility and a hard security boundary.

## Communication & Mediation

No sandbox may communicate directly with any other sandbox. All communication is strictly mediated by AegisHub.

### Communication Rules

- Every sandbox has exactly one vsock connection to AegisHub.
- The **Store VM** is the only component allowed to persist long-term data.
- The **Network Boundary VM** is the only component allowed to make outbound network requests.
- The **Memory VM** is the only component allowed to hold conversation state.
- **Agent Runtime VMs** may only communicate with their paired Memory VM, the Court Scribe, and AegisHub.
- **Court VMs** may only communicate with the Court Scribe, their paired Agent (during reviews), and AegisHub.

## Data Flow Example: Skill Creation via SDLC

1. **Proposal Creation**  
   **Owner:** User’s Agent Runtime VM  
   The agent creates a formal Change Proposal and submits it to the Store VM.

2. **Court Review (Proposal)**  
   **Owner:** Court Scribe VM (Clerk of the Court)  
   The Scribe distributes the raw proposal to the Governance Court sandboxes, collects votes and feedback, and returns the consolidated result.

3. **Feedback & Iteration**  
   **Owner:** User’s Agent Runtime VM  
   The original agent receives Court feedback, revises the proposal if needed, and resubmits it.

4. **Implementation**  
   **Owner:** Builder VM (one per proposal)  
   A dedicated Builder sandbox creates a persistent git repository for the skill, implements the code, and submits a Pull Request-style revision.

5. **Code Review**  
   **Owner:** Court Scribe VM  
   The Scribe distributes the code changes to the Court sandboxes and returns consolidated feedback to the Builder VM.

6. **Approval & Merge**  
   **Owner:** Builder VM  
   When all Court personas approve, the Builder VM merges the changes into the skill’s persistent git repository.

7. **Build & Deployment**  
   **Owner:** Builder VM  
   The Builder performs a final validation check (confirming the PR exists and all required Court approvals are present). Only then does it build and register the skill in the Store VM’s skill registry.

8. **Activation**  
   **Owner:** User’s Agent Runtime VM  
   The original agent is notified that the new skill is available.

## Security Boundaries

The architecture is designed with explicit trust boundaries:

- **Host Daemon**: Highest privilege.
- **AegisHub**: Privileged router. Can see all inter-sandbox traffic.
- **Network Boundary VM**: Only component trusted with secrets and outbound network.
- **Store VM**: Only component trusted to persist data and the audit log.
- **Memory VM**: Only component trusted with conversation state.
- **Court Scribe VM**: Read access to conversations only.
- **Governance Court VMs**: Minimal privileges. Can only read proposals and write votes.
- **Builder VMs**: Ephemeral and tightly constrained.
- **Agent Runtime VMs**: Untrusted. Run user-facing agent logic.

**Key Rule:** Compromise of any single sandbox must not allow an attacker to compromise the host or bypass the Governance Court.

## Related Documents

- **[prd/index.md](../prd/index.md)** — Full set of Product Requirements Documents
- **[prd/runtime-architecture.md](../prd/runtime-architecture.md)** — Detailed runtime requirements
- **[specs/](../specs/)** — Individual component specifications (when available)