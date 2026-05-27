# Phase 2: Store VM Persistent State & Timers

**Status:** Partially Started (reconciliation functions moved)  
**Priority:** P0  
**Estimated Effort:** 2–3 weeks

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
