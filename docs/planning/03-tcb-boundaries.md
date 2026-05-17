# Task 03: Host Daemon TCB Boundaries

**Status**: Draft (Phase 0)
**Last Updated**: May 2026
**Related**: `docs/specs/host-daemon.md`, `docs/architecture.md`, `docs/lessons-learned` branch

## Purpose

This document defines the **target responsibilities** for the Host Daemon and the major sandboxed components. It serves as the north star for the aggressive Minimal TCB refactor in Task 03.

We are intentionally moving toward the multi-VM architecture described in the `docs/lessons-learned` branch.

## Final Host Daemon Responsibilities (Strict)

The Host Daemon must be reduced to **only** the following:

| Responsibility                    | Description                                                                 | Why it must stay in TCB          | Notes |
|-----------------------------------|-----------------------------------------------------------------------------|----------------------------------|-------|
| Sandbox Lifecycle                 | Create, start, stop, monitor, and destroy Firecracker microVMs             | Requires host privileges         | Core |
| Unix Socket Server                | Accept connections from CLI/TUI and dispatch privileged operations         | Needs to run on host             | Core |
| Ed25519 Key Distribution          | Generate and securely deliver private keys to microVMs                     | Trust anchor                     | Core |
| Merkle Root Signing               | Periodically sign audit log Merkle roots                                   | Tamper-evidence root of trust    | Core |
| AegisHub Watchdog                 | Launch, monitor, and restart the AegisHub microVM if needed                | Bootstrap + containment          | Core |
| Basic Capability Dropping         | Drop unnecessary capabilities as early as possible                         | Least privilege                  | Hardening |
| Lifecycle Containment             | Ensure that if the daemon dies, VMs are terminated                         | Containment                      | Hardening |

**Explicit Non-Responsibilities** (must be removed):

- Processing user messages or LLM output
- Building or serving Tool Registries
- Running the Governance Court or making governance decisions
- Managing persistent stores (proposals, audit, composition, etc.)
- Handling secrets or vault operations
- Running ReAct loops, build pipelines, or event-driven orchestrators
- Starting the web dashboard
- Maintaining long-lived business state machines

## Target Component Responsibilities

### AegisHub (Strengthen)

- Central communication router and ACL enforcer between all sandboxes.
- Hosts many control-plane API handlers (or proxies them).
- May host Tool Registry serving.
- Coordinates with Store VM and other components.
- Runs inside its own microVM (already partially implemented).

### Store VM (New / Expand in this task)

- **Sole owner** of all persistent state.
- Runs: ProposalStore, PRStore, CompositionStore, Audit-related storage, Skill Registry, etc.
- Exposes a narrow, well-defined interface (primarily to AegisHub).
- One of the most important new boundaries to establish.

### Network Boundary VM

- The **only** component allowed to handle secrets.
- The **only** component allowed to make outbound network requests (including to Ollama).
- Enforces network policy from skill declarations.
- Future home of Envoy or similar proxy.

### Court Components (Future)

- Court Scribe VM
- Individual Governance Court persona VMs (7 personas)
- These should eventually be fully isolated from the Host Daemon.

### Builder VMs

- Ephemeral VMs used for skill building and code generation.
- Should be orchestrated via AegisHub / builder coordination, not directly from the Host Daemon.

## Current Daemon Inventory & Migration Plan

High-level mapping of major pieces currently living in `cmd/aegisclaw/`:

| Component                        | Current Location          | Fate                          | Target Component     | Priority | Notes |
|----------------------------------|---------------------------|-------------------------------|----------------------|----------|-------|
| Court Engine                     | `cmd/aegisclaw/`          | Move                          | Court VMs + Scribe   | High     | Governance must leave TCB |
| BuildOrchestrator + Pipeline     | `cmd/aegisclaw/`          | Move                          | AegisHub / Builder VM| High     | Event-driven builder logic |
| ProposalStore                    | Initialized in daemon     | Move                          | **Store VM**         | High     | Persistent state ownership |
| PRStore                          | Initialized in daemon     | Move                          | **Store VM**         | High     | - |
| CompositionStore                 | Initialized in daemon     | Move                          | **Store VM**         | High     | - |
| MemoryStore                      | Initialized in daemon     | Move                          | Store VM / Memory VM | Medium   | - |
| EventBus                         | Initialized in daemon     | Move                          | AegisHub / Store VM  | Medium   | - |
| WorkerStore                      | Initialized in daemon     | Move                          | Store VM             | Medium   | - |
| Vault                            | Initialized in daemon     | Remove from daemon            | Network Boundary VM  | High     | Never touch secrets in TCB |
| Tool Registry construction       | `cmd/aegisclaw/`          | Move                          | AegisHub             | Medium   | - |
| Most API handlers                | `cmd/aegisclaw/`          | Move or proxy                 | AegisHub             | High     | Reduce daemon surface |
| Dashboard startup                | `cmd/aegisclaw/`          | Move                          | Web Portal VM        | Low      | - |
| Team / Autonomy registries       | `cmd/aegisclaw/`          | Move                          | Store VM / AegisHub  | Medium   | - |
| `launchAegisHub` + MessageHub    | `cmd/aegisclaw/`          | Keep (but slim down)          | Host Daemon          | -        | Core responsibility |
| Sandbox / Firecracker lifecycle  | `internal/sandbox`        | Keep                          | Host Daemon          | -        | Core responsibility |
| Merkle / Kernel signing          | `internal/kernel`         | Keep                          | Host Daemon          | -        | Core responsibility |

## Initial MicroVM Scope for Task 03

We will actively work on the following in this task:

1. **Host Daemon** — Aggressive stripping
2. **AegisHub** — Strengthen as control plane
3. **Store VM** — Introduce / realize as persistent state owner
4. **Network Boundary VM** — At least define role and remove secret handling from daemon

Court VMs and Builder VMs will be addressed at a high level but may be completed in later tasks.

## Measurement Baseline (to be updated)

- Current daemon LOC (excluding tests): TBD
- Current idle memory: TBD

## Open Questions

- How much of the current in-process `MessageHub` should move into the AegisHub microVM vs stay as a bridge?
- Should we create a thin `ControlPlane` interface that the daemon talks to, rather than directly to individual stores?

---

**This document will be updated as we make decisions during Task 03.**