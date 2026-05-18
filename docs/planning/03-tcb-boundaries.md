# Task 03: Host Daemon TCB Boundaries

**Status**: Phase 3 complete. **Phase 2.6 (Store VM Contract & Launch) started**.
**Last Updated**: May 17, 2026

## Final Host Daemon Responsibilities (Strict)

The Host Daemon must be reduced to **only** the following (updated with Store VM):

| Responsibility                    | Description                                                                 | Why it must stay in TCB          | Notes |
|-----------------------------------|-----------------------------------------------------------------------------|----------------------------------|-------|
| Sandbox Lifecycle                 | Create, start, stop, monitor, and destroy Firecracker microVMs             | Requires host privileges         | Core |
| Unix Socket Server                | Accept connections from CLI/TUI and dispatch privileged operations         | Needs to run on host             | Core |
| Ed25519 Key Distribution          | Generate and securely deliver private keys to microVMs                     | Trust anchor                     | Core |
| Merkle Root Signing               | Periodically sign audit log Merkle roots                                   | Tamper-evidence root of trust    | Core |
| AegisHub Watchdog                 | Launch, monitor, and restart the AegisHub microVM                          | Bootstrap + containment          | Core |
| **Store VM Watchdog** (future)    | Launch, monitor, and restart the Store VM                                  | Bootstrap + containment          | Phase 2 |
| Basic Capability Dropping         | Drop unnecessary capabilities as early as possible                         | Least privilege                  | Hardening |
| Lifecycle Containment             | Ensure VMs are terminated if daemon dies                                   | Containment                      | Hardening |

**Explicit Non-Responsibilities** (must be removed):

- Managing persistent stores (proposals, audit, composition, etc.)
- Handling secrets or vault operations

## Target Component Responsibilities

### Store VM (Phase 2 – Realization)

**Core Principle**: The Store VM is the **sole owner** of all persistent state. The Host Daemon must **never** directly create, hold, or operate on ProposalStore, PRStore, MemoryStore, etc.

#### Store VM Responsibilities
- Sole owner of ProposalStore, PullRequestStore, CompositionStore, MemoryStore, WorkerStore, EventStore.
- Exposes narrow `Store` interface.
- Runs inside its own Firecracker microVM.
- Supports in-process and remote (vsock) backends.

#### Host Daemon vs Store VM Boundary

| Action                        | Owner       |
|-------------------------------|-------------|
| Create/initialize stores      | Store VM    |
| Launch & monitor Store VM     | Host Daemon |
| Access stores                 | Via `StoreVM` interface |

#### Launch & Lifecycle (Phase 2.6)

**Host Daemon responsibilities**:
- Create Firecracker spec for Store VM.
- Launch Store VM at startup (future `launchStoreVM()`).
- Monitor health and restart on failure.
- Graceful shutdown.

**Store VM responsibilities**:
- Initialize stores on start.
- Serve requests (vsock in future).
- Manage its own persistence.

This keeps daemon TCB minimal.

### AegisHub, Network Boundary VM (existing sections remain)

## Phase 2: Store VM Realization

**Phase 2.6 Outcome**: Store VM contract, ownership boundary, and launch responsibilities defined. `launchStoreVM` noted as future core daemon responsibility. Migration table updated.

**Updated Roadmap**:
1. Phase 2.6 (In Progress): Define contract + launch pattern.
2. Real Store VM microVM definition.
3. vsock protocol + remote client.
4. Dual-mode `NewStoreVM()` support.
5. Integrate launch + monitoring into daemon.

**This document will be updated as work progresses.**