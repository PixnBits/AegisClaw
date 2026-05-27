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

**Phase 2 Slice Progress (2.1a + 2.1b + 2.1c)**

**2.1a (Durable core in Store):**
- Real Reconcile* functions + 0600 grants.json / background.json.
- Functional `reconcile.expired_grants` Hub command.
- Startup loading for basic recovery.

**2.1b (Wiring surface):**
- Added helper + wired key surface paths to prefer Store reconciliation.

**2.1c (Autonomous Store timer):**
- Added background timer loop in Store.

**2.1d (Timer management API + Hub surface - this slice):**
- Implemented `ScheduleTimer`, `CancelTimer`, and `ListActiveTimers` in Store with durable 0600 storage (`timers.json`).
- Wired the three APIs as Hub commands (`timer.schedule`, `timer.cancel`, `timer.list`).
- Enhanced `reconcile.expired_grants` to also reconcile the general timer collection.

This completes the user's explicit starting tasks for this Phase 2 session (real timer loop + the three management functions + durable metadata + full reconcile command).

Citations: phase-2.md §2.1, store-vm.md, event-system.md.

**2.3 Surface Cutover Progress (this slice):**
- Wired additional call sites in cmd/aegis to prefer Store VM reconciliation via Hub:
  - `runTasksList`
  - Main paths inside `runAutonomyGrant` (pre- and post-grant reconciliation)
- Combined with prior slices (`reconciliation.tick` subscriber and `runSessionsStatus`), several high-visibility surfaces now delegate to the authoritative Store implementation when the daemon is running.
- Local thin `reconcileExpired*` functions remain only as explicit, documented fallbacks.

This is steady, measurable progress on the DoD item "No thin wrapper functions remaining in `cmd/aegis`".

**Further cutover + Store-driven events in this slice:**
- Wired `runSessionsList` to prefer Store.
- In the autonomy grant path: new grants are recorded in the Store *and* their expiration timers are scheduled in the Store using the new timer APIs (reducing reliance on local `eventbus.DefaultBus.ScheduleTimer` for authoritative expiration).
- Added explicit event publishing from the Store's autonomous timer/reconciliation: when grants expire, the Store now publishes `autonomy.expired`, `background.expired`, and `timer.fired.*` style events via the Hub (signed). This fulfills the "Store-driven event publishing" goal so downstream components can react without the daemon-local EventBus being the source of truth.

This is direct, high-value progress on both "no thin wrappers" and making the Store the real owner of timer-driven state and events (per store-vm.md and event-system.md).

Citations: phase-2.md DoD, event-system.md, store-vm.md.

## Phase 2.4 Group Complete (Timer Restart Recovery + Additional Surface Cutover)

**Changes in this group (2.4):**
- **Timer recovery / restart survival (critical DoD item "Timers survive daemon and Store VM restarts")**:
  - Explicit `loadTimers()` at Store startup (was previously unassigned; only grants/background were loaded).
  - Immediate boot-time catch-up reconciliation: on startup after register, run `ReconcileExpired*` + `reconcileExpiredTimers()` for anything that expired while Store was offline, then publish the corresponding signed `autonomy.expired` / `background.expired` / `timer.fired` events via Hub (using new `publishExpirationEvent` helper).
  - Documented the recovery model in code: because the design is "hard-coded 30s ticker + full scan of the 0600 JSON files on every signal" (no in-memory time.Timer heap), simply loading the durable state + one catch-up reconcile + the running ticker is sufficient to make all scheduled timers and grants survive full Store/daemon restarts. Non-expired entries stay in `timers.json` / `grants.json` and are caught on the first post-restart tick.
  - Extended the autonomous ticker drain to also reconcile general timers on every signal and publish `timer.fired` events (previous autonomous path only handled the two grant types; the on-demand Hub command already returned timer expirations).
- **Surface cutover (continuing erosion of thin wrappers)**:
  - In `runAutonomyGrant`: the two `eventbus.DefaultBus.ScheduleTimer` calls for autonomy/background expiration are no longer unconditional. They are now executed ONLY in the explicit fallback block when the primary `autonomy.grant` + `timer.schedule` sends to Store fail. The Store path (already present) is now the clear primary for durable grant + timer ownership.
  - Added precise comments citing `event-system.md` ("Persistent timers are stored in Store VM") and `store-vm.md` at every changed site.
  - The local `reconcileExpiredAutonomy` / `reconcileExpiredBackgroundWork` thin funcs + the `reconciliation.tick` subscriber + local sessions.json remain as documented fallbacks (consistent with prior slices).
- **Helper + event publishing hygiene**:
  - New `publishExpirationEvent` helper in Store to centralize signed `event.publish` for all expiration types (reduces duplication, guarantees the same shape for downstream consumers per event-system.md).
- **Citations in every material change**:
  - `docs/specs/event-system.md` (Persistent timers section, event names, Store VM management of cron-like timers).
  - `docs/specs/store-vm.md` (durable state ownership, even though the current text of the spec focuses on proposals/git; the Phase 2 plan + event-system drive the timer extension).
  - `docs/no-stubs-plan/phase-2.md` §2.3 / DoD and the 2.1/2.3 detailed tasks.

**Verification performed (this group):**
- `go build ./cmd/store ./cmd/aegis` → clean
- `make build-binaries` → all binaries (including store + aegis) built successfully
- `go test -count=1 -short ./...` → all packages OK (exit 0)
- `./bin/aegis doctor` → clean (normal non-root / daemon-not-running warnings only)
- Tree remains clean; no direct sudo; AGENTS.md followed.

**Honest DoD re-audit after 2.4:**
- [x] `reconcile.expired_grants` fully implemented and functional in Store VM (prior)
- [x] Durable storage (JSON 0600) for autonomy grants and background work (prior + timers.json)
- [x] **Timers survive daemon and Store VM restarts** — now explicitly implemented via startup load + boot catch-up reconcile + autonomous ticker full-scan model. Any timer/grant still in the JSON files after restart will be processed and will emit the correct events. (This was the primary target of the group.)
- [ ] No thin wrapper functions remaining in `cmd/aegis` (advanced: the grant-path local ScheduleTimer is now fallback-only; the two reconcile* funcs + periodic tick subscriber + local EventBus scheduling in other paths still exist as documented scaffolding. Full removal is future work per 2.4 detailed task.)
- [ ] All expiration logic removed from CLI surface (same status as above; the local funcs are still the implementation of the fallback paths and still mutate `~/.aegis/sessions.json`.)
- [ ] Full test coverage for timer scheduling, reconciliation, and persistence (no new unit tests added in 2.4 for the new recovery/publish paths; cmd/store and cmd/aegis packages have minimal/no dedicated timer tests. Existing tree tests pass. This gap remains open — a follow-up group should add `cmd/store/*_test.go` exercising Schedule/Cancel, reconcile, and the recovery catch-up.)

**Remaining gaps (transparent):**
- The local `reconcileExpired*` implementations and EventBus scheduling in cmd/aegis are the last significant thin surface. Per the detailed tasks, 2.4 "Removal of Surface Code" will require actually deleting (or completely guarding) those funcs once every call site has been cut over or the CLI surfaces no longer need local state for display.
- `docs/specs/store-vm.md` still does not list timer/grant responsibilities (noted in initial assessment); a future doc-only update should sync the spec.
- No chaos/restart test yet exercising "Store VM restart while timers active" (mentioned in plan).
- General timer recovery publishes "timer.fired" (with id in payload); if callers expect "timer.fired.<preset>" style, that can be evolved without breaking the current shape.

**Status:** Steady measurable progress. The restart-survival DoD bullet is now satisfied in the implementation (with clear comments). One more high-visibility thin site (grant timer scheduling) has been converted to Store-primary + fallback. All verification passed. Ready for next "continue" slice or a dedicated test-coverage group.

Citations for this group: phase-2.md §2.3/2.4/DoD, event-system.md (full "Persistent timers" and "Event Flow" sections), store-vm.md (Responsibility Boundaries + Architecture for durable state).
