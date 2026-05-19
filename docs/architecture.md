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

### Component Data Ownership (Post-Phase 5)

| Data / Responsibility          | Long-term Owner          | Access Path                  |
|--------------------------------|--------------------------|------------------------------|
| Proposals, PRs, Events, Workers| Store VM                 | AegisHub → Store VM          |
| Chat sessions & context        | Agent VMs + Memory VM    | AegisHub                     |
| Dashboard / Web UI             | Web Portal VM            | AegisHub                     |
| Composition (VM registry)      | Host Daemon (temporary)  | Direct (lightweight publish) |

## Runtime Architecture

The system is composed of one small trusted host component and multiple isolated sandboxes.

### Host Daemon (Minimal Trusted Computing Base)

The host daemon is intentionally kept extremely small. After Phase 5, it no longer depends on a general `store.Store` interface or `remoteStore`.

Its only responsibilities are:

- Starting, stopping, and monitoring isolated sandboxes (Firecracker on Linux, Docker Sandbox on macOS/Windows)
- Managing the Unix socket for the CLI and TUI
- Signing Merkle tree roots for the tamper-evident audit log
- Serving as the bootstrap and watchdog for AegisHub
- Lightweight Composition Manifest publishing for critical launched VMs (AegisHub, Store VM) — temporary

All persistent state access (proposals, workers, events, etc.) has been removed from the daemon.

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

#### Composition Manifest Ownership (Temporary)

The daemon temporarily retains lightweight logic to publish launched critical VMs (AegisHub, Store VM) into the Composition Manifest. This is a transitional responsibility.

Future model: Component and VM registry data should be queried through AegisHub → daemon (mediated, ACL-enforced). The daemon acts only as a thin publisher for its own launched VMs.

### ControlPlaneProxy & Mediated Request Flow (Phase 6)

The Host Daemon now includes a `ControlPlaneProxy` that acts as a thin mediation layer. CLI and TUI operations are forwarded through this proxy to AegisHub rather than being handled directly inside the daemon.

**Request Flow**:
CLI / TUI → Unix socket → api.Handler → ControlPlaneProxy.Forward → AegisHub (MessageHub) → Target component (Store VM, Agent VM, Web Portal VM, etc.)

This keeps the daemon's trusted surface minimal while enabling AegisHub-mediated access to data and operations. Requests are intentionally styled similarly to skill/tool invocations.

**Current Socket Model**:
A single Unix socket (`Daemon.SocketPath`) is used for all communication.

**Future Work**:
Split into two sockets for attack-surface reduction:
- Privileged socket: VM lifecycle, control-plane, and shutdown operations.
- Standard socket: Skill/tool calls and read-only data queries.

This separation would allow stricter permission models and further reduce the attack surface of the Host Daemon.

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

**Future Data Access Routing**: All reads of proposals, workers, events, etc. from CLI, dashboard, or other components will be routed through AegisHub (via the daemon's ControlPlaneProxy) to the appropriate owner (primarily Store VM). The Host Daemon no longer provides direct Store access.

**Phase 7 Wiring**: Key CLI handlers (worker.list/status, skill.list/status, chat.message, etc.) have been refactored to delegate to ControlPlaneProxy.

**Phase 8 Implementation**: AegisHub (MessageHub) receives `ControlPlaneRequest` messages, performs ACL checks (RoleCLI permitted), and dispatches on the `Action` field. The handler in `internal/ipc/hub.go:handleControlPlaneRequest` first attempts delegation to a registered backend (e.g., "store-vm") when available, falling back to realistic sample data otherwise. `ControlPlaneProxy.Forward` respects context cancellation and properly surfaces backend errors. Dead Phase 3 dashboard stubs were removed. 

Additional actions wired: `chat.message`, `proposal.list`, `proposal.status` (handlers now registered on API socket and use ControlPlaneProxy). Sessions.send threaded through proxy. Cleanup pass: nil proxy fallbacks explicitly marked with TODO(Phase 9) where intentional (tool registry internal path); delegation fallback now logs; added coverage tests for proposal/sessions paths.

**Phase 9**: Integration tests added using in-process MessageHubNoKernel + RegisterSkill for realistic Store VM / chat-router delegation. Proposal actions now have adapter pattern for ProposalStore. Chat responses include timestamp. Real backend implementations (full Store VM vsock, chat router) remain future work.

## Data Flow Example: Skill Creation via SDLC

... (rest of the file unchanged) ...