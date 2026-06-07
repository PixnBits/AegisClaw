# Runtime Architecture

## Core Principle

**If a component touches untrusted data or performs complex logic, it must run inside its own isolated sandbox.**

On **Linux** this sandbox is a Firecracker microVM. On **macOS and Windows** this is a Docker Sandbox (`sbx` — not full containers).

The only code that runs directly on the host is a tiny, minimal daemon.

## Host Daemon (Trusted Computing Base)

The daemon is responsible for **only**:

- Starting, stopping, and monitoring sandboxed environments (Firecracker on Linux, Docker Sandbox on macOS/Windows)
- Managing the Unix socket for the CLI/TUI
- Signing Merkle tree roots for the audit log
- Acting as bootstrap and watchdog for AegisHub

## Sandboxed Components

All other functionality runs in isolated sandboxes:

- **AegisHub** — The only privileged router. Enforces strict ACLs between all sandboxes.
- **Store VM** — Owns all persistent storage (proposals, audit logs, composition history, *and channel state*)
- **Tool Handler VMs** — Each major tool or skill category runs in its own sandbox
- **LLM Proxy VM** — Handles secret injection and prompt sanitization
- **Court Scribe VM** — Observes conversations and produces structured summaries *(may evolve to per-channel or multi-agent support)*
- **Court Member VMs** — Each of the seven Governance Court personas runs in its own sandbox as specialised Agent instances
- **Agent Runtime VMs** — Individual agent instances, including role-specialised ones (Court personas, SDLC roles, Project Manager)

## Dynamic Agent Lifecycle & Resource Management (Multi-Agent Channels)

**Requirement**: Agent microVMs (Court Members, SDLC specialists, Project Manager, general agents) **must support fast on-demand spin-up (<1s target) and clean spin-down when idle**.

This is essential to deliver responsive UX (Court visibility <30s per personas) while keeping resource usage low on local hardware (laptops, home servers) when the system is not actively in use.

### Host Daemon Responsibilities (Expanded)
- Fast launch path for Agent Runtime VMs and Court Member VMs (minimal rootfs, optimised kernel, parallel launch where possible, potential snapshot resume or warm pools for common roles).
- Idle detection and graceful spin-down (configurable timeout or explicit release by PM/orchestrator).
- Resource accounting, limits, and observability (per channel, per user, global).
- Monitoring and health for many transient agent instances.

### Triggers for Spin-Up
- Project Manager decides a role/agent is needed for current plan or delegation.
- User @mentions a role or explicit request.
- Channel activity requiring a specialist.
- Court review initiated.

### Channel Mapping
Channels are organisational units persisted in Store VM. Agents "attach" to channels via AegisHub sessions and ACLs. Channel state (history, proposals, artifacts) lives in Store; agent-specific memory can be namespaced or in dedicated Memory VMs.

See `collaboration-model.md` for full details on channels, roles, and orchestration.

## Philosophy

This architecture ensures that a compromise in any single component cannot spread. The trusted computing base is kept to an absolute minimum.

Every major function has its own security boundary. The addition of dynamic role-based agents and channels does not weaken isolation — Court Members remain unable to see each other's state, and all changes still require formal Court approval.

## Related Documents

- **[../architecture.md](../architecture.md)** — High-level overview
- **[../index.md](./index.md)** — PRD index
- **[glossary.md](./glossary.md)** — Key terms
- **[security-model.md](./security-model.md)** — Security guarantees
- **[collaboration-model.md](./collaboration-model.md)** — Channels, roles, Project Manager, and dynamic lifecycle model
- **[governance-court.md](./governance-court.md)** — Court as Agents in channels