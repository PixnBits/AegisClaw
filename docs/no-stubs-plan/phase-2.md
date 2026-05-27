# Phase 2: Store VM Persistent State & Timers

**Status:** Partially Started (reconciliation functions moved)  
**Priority:** P0  
**Estimated Effort:** 2–3 weeks

## Current State Assessment (Post Phase 1)

After completion of Phase 1 (real Agent Runtime + Memory VM), the following situation exists:

### Thin Surface Still Present in `cmd/aegis`
- `CLISession` struct + `~/.aegis/sessions.json` (0600) serves as the de-facto persistent store for autonomy grants and background work expirations.
- `reconcileExpiredAutonomy()` (cmd/aegis/main.go:1786) and `reconcileExpiredBackgroundWork()` (cmd/aegis/main.go:1821) are **actively implemented** and called from:
  - `runSessionsList`
  - `runTasks`
  - `runSessionsShow`
  - Startup paths
- These functions mutate local session state and publish via the in-process EventBus (`autonomy.expired`, `background.expired`).
- Multiple TODO(architecture) comments explicitly call out that this logic belongs in the Store VM per `store-vm.md` and `event-system.md`.

### Store VM Side (cmd/store)
- Placeholder functions `ReconcileExpiredAutonomy()` and `ReconcileExpiredBackgroundWork()` exist but return empty slices (cmd/store/main.go:125-134).
- The `reconcile.expired_grants` Hub command handler exists (cmd/store/main.go:209) but is non-functional.
- Store already has a simple file-based JSON persistence pattern (`loadFromFile` / `saveToFile`) used for proposals, skills, audit, etc. (cmd/store/main.go:55-69).

### Spec Alignment Issues
- `docs/specs/store-vm.md` currently does **not** list timer management, autonomy grants, or background work reconciliation among its responsibilities or public API commands. It focuses on proposals, git, skills, Court, and audit.
- `docs/specs/event-system.md` states: "Persistent timers are stored in Store VM" and gives the example `timer.fired.daily-summary`.
- The Phase 2 plan (this document) assumes Store VM ownership, but the Store spec has not yet been updated to reflect the new requirements.

### Gap Summary
- The "move" of reconciliation noted in the previous status was only partial (stubs + command skeleton in Store; real logic + calls remain in the daemon surface).
- No durable timer infrastructure exists in Store yet (no `ScheduleTimer`, no hard-coded timer loop, no recovery on restart).
- CLI autonomy and tasks commands still perform authoritative enforcement locally instead of delegating to Store via Hub.

This is the exact starting point for Phase 2 proper implementation work.

## Goal
Make the Store VM the single source of truth for all persistent timers, autonomy grants, background work, and scheduled tasks.

## Key Specifications
- `docs/specs/store-vm.md`
- `docs/specs/event-system.md`

## Definition of Done
- [ ] `reconcile.expired_grants` command fully implemented and functional in Store VM
- [ ] Durable storage (JSON files or simple DB) for autonomy grants and background work
- [ ] Timers survive daemon and Store VM restarts
- [ ] No thin wrapper functions remaining in `cmd/aegis`
- [ ] All expiration logic removed from CLI surface
- [ ] Full test coverage for timer scheduling, reconciliation, and persistence

## Detailed Tasks

### 2.1 Complete Store VM Timer Infrastructure (Week 1)
- Add real timer loop inside `cmd/store/main.go` (hard-coded timer per spec)
- Implement `ScheduleTimer`, `CancelTimer`, and `ListActiveTimers`
- Store timer metadata durably (with session_id, preset, expiration, signature)

### 2.2 Reconciliation Command (Week 1–2)
- Implement full `reconcile.expired_grants` Hub command
- Call `ReconcileExpiredAutonomy()` and `ReconcileExpiredBackgroundWork()` inside Store
- Publish results via Hub (with Merkle signing)
- Update CLI surface to call this command instead of local functions

### 2.3 Durable State & Recovery (Week 2)
- Persist autonomy grants and background work to `grants.json` / `background.json` (0600)
- Add startup recovery: re-schedule active timers from disk
- Implement graceful degradation if Store is unavailable

### 2.4 Testing & Removal of Surface Code (Week 2–3)
- Unit + integration tests for timer scheduling and reconciliation
- Chaos test: Store VM restart while timers are active
- Remove all local `reconcileExpired*` functions from `cmd/aegis/main.go`
- Update `docs/specs/store-vm.md` with final implementation notes

## Success Criteria
When this phase is complete:
- All autonomy and background expiration logic lives in Store VM
- CLI only displays state; enforcement happens in Store
- Timers are durable and survive restarts
- Zero surface scaffolding remains for timer/reconciliation logic

## Phase 2.0 Assessment Complete (This Slice)

Gap analysis performed against live code (cmd/aegis/main.go:1769-1841 and cmd/store/main.go) + specs.

**Honest starting point:** Reconciliation logic has only been stubbed in Store. The real enforcement + state still lives in the daemon's local `CLISession` + file system with active `reconcileExpired*` functions called from the CLI surface.

**Proposed First Implementation Slice (2.1a) — Status: In Progress / Core Done**

Completed in this slice:
- Added proper 0600 persistence helpers for `grants.json` and `background.json`.
- Implemented real `ReconcileExpiredAutonomy()` and `ReconcileExpiredBackgroundWork()` that actually expire entries and persist changes.
- Activated the `reconcile.expired_grants` Hub command so it returns real results.
- Added startup loading of grant state (basic recovery).

Remaining in 2.1a / next micro-slice:
- Wire some surface calls in `cmd/aegis` to prefer the Store version via Hub.
- Add unit tests for the new reconcile functions.
- Improve the data model (currently very loose map[string]interface{}).

This is the first concrete transfer of authority for timer reconciliation into the Store VM.

Citations: store-vm.md (durable state), event-system.md (Hub-mediated timers), phase-2.md DoD items 1-4.
