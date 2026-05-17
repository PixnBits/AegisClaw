# 03 - Host Daemon Minimal TCB Refactor

**Goal**: Refactor the Host Daemon (`cmd/aegisclaw` or equivalent) to **strictly** match `docs/specs/host-daemon.md` and the Minimal TCB principle in `docs/architecture.md`. The daemon must become the smallest possible trusted component — no business logic, no untrusted data processing, target < 2000 LOC and < 20 MB idle memory.

## Why This Matters (Paranoid Security)
The daemon runs with root/host privileges. Any extra responsibility dramatically increases the attack surface. Per spec, it must **never**:
- Process user messages or LLM output
- Handle secrets
- Make governance decisions
- Execute generated code

Current implementation significantly violates this.

## Tasks

We are taking an **aggressive** approach toward the multi-VM architecture in the `docs/lessons-learned` branch.

See the living boundary document: `docs/planning/03-tcb-boundaries.md`

### Phase 0: Foundations (In Progress)
- Create TCB boundaries document
- High-level audit of current daemon responsibilities
- Decide initial microVM scope (Host Daemon, AegisHub, Store VM, Network Boundary VM)

### Phase 1: Aggressive Stripping of Host Daemon
- Remove Court, BuildOrchestrator, persistent stores, vault handling, most API handlers, etc.
- Move ownership to Store VM, AegisHub, Court components, Network Boundary VM

### Phase 2: Introduce / Realize Store VM
- Establish Store VM as owner of persistent state
- Define protocol between AegisHub ↔ Store VM

### Phase 3: Strengthen AegisHub
- Move more control-plane logic into AegisHub

### Phase 4: Host Daemon Hardening
- Capability dropping, seccomp, lifecycle containment

### Phase 5: Verification
- Implement required tests from `host-daemon.md`
- Measure LOC and memory
- Final review for forbidden patterns

## Acceptance Criteria
- Daemon binary significantly reduced
- Idle memory target approached
- Passes core tests from `docs/specs/host-daemon.md`
- No business logic / governance / secret handling remains in daemon
- Store VM exists and owns persistent state
- Clear boundaries documented

**Dependencies**: Task 01 + Task 02 complete
**Estimated effort**: Significant (aggressive track)

**Owner**: TBD
**Status**: **Phase 0 started** — See `docs/planning/03-tcb-boundaries.md` for current boundary decisions and migration map.