# 12 - EventBus Full Background Services

**Goal**: Complete the EventBus implementation with timers, signals, approval queues, and proactive background service management.

## Current State
Basic EventBus exists (`cmd/aegisclaw/eventbus_daemon.go`), but full background capabilities (timers, signals, approval queues, proactive actions) are incomplete per `docs/specs/additional-requirements-and-gaps.md`.

## Tasks

1. **Extend EventBus core**
   - Add timer support (one-shot and recurring)
   - Add signal handling (custom events from agents or external systems)
   - Implement approval queue with priority and expiration
2. **Background service manager**
   - Create `internal/events/background.go`
   - Support long-running background tasks with health checks and graceful shutdown
3. **Integration points**
   - Wire into Agent Runtime for proactive behaviors
   - Connect to Governance Court for approval workflows
4. **Tests**
   - Timer accuracy and cancellation tests
   - Approval queue priority and expiration tests
   - Background service lifecycle tests

## Acceptance Criteria
- Timers, signals, and approval queues are fully functional
- Background services can be started/stopped/monitored
- Full alignment with `docs/specs/event-system.md`

**Dependencies**: Core runtime and event infrastructure
**Estimated effort**: 2–3 days

**Owner**: TBD
**Status**: Ready to start