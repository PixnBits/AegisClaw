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

### Phase 0: Foundations (Completed)
- Create TCB boundaries document (`docs/planning/03-tcb-boundaries.md`)
- High-level audit of current daemon responsibilities + migration map
- Decide initial microVM scope (Host Daemon, AegisHub, Store VM, Network Boundary VM)
- Baseline LOC/memory measurement recorded in boundaries doc
- Prerequisite chore (simplify-directory-layout-remove-legacy-migrations) completed (legacy path migration code removed)

### Phase 1: Aggressive Stripping of Host Daemon (In Progress)
- **Store migration seam complete**: unified `Store` interface + `runtimeEnv.Store` in place.
- **Court Engine extraction started**: Real `court.Engine` initialization disabled (`court_init.go` neutralized). Direct `env.Court` field removed from `runtimeEnv`. Court decision list/show handlers stubbed with "moved to Court Scribe" messages. Direct Court calls in `chat.go`, `pr_autocreate.go`, and handlers now go through `CourtClient` (currently StubClient → later Court VMs). Governance surface dramatically reduced in the daemon.
- **Vault / secret handling extraction started**: `vault.NewVault` + `kern.PrivateKeyBytes()` for secrets removed from `initRuntime`. `env.Vault` field removed. Vault API handlers (`vault.secret.*`) stubbed to "secrets handled by Network Boundary VM". Daemon no longer opens or operates on the encrypted vault.
- **BuildOrchestrator extraction started**: `initBuildOrchestrator` neutralized, `env.BuildOrchestrator` field removed. `builder_daemon.go` dispatch loop disabled (`startBuilderDispatchDaemon` / `processImplementingProposals` are now no-ops). Builder coordination responsibility moved to AegisHub / Builder VMs via `BuilderClient`.

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
**Status**: **Phase 0 complete** — Boundary document + migration map ready. Ready for Phase 1. (Chore finished.) — See `docs/planning/03-tcb-boundaries.md` for current boundary decisions and migration map.