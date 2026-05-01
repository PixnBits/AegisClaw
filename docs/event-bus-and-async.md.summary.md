# `docs/event-bus-and-async.md` — Summary

## Purpose

Specifies the **Central Event Bus**, **Timer Service**, signal handling, and wakeup mechanisms that enable asynchronous and long-running agency in AegisClaw. Covers the architectural design, data models, persistence strategy, and integration with the Merkle audit log.

## Key Contents

### Goals
- Proactive stateful behaviour over hours/days/weeks (research completion, recurring summaries, reminders).
- Reliable Orchestrator/Worker wakeup from external events or timers.
- Exactly-once or at-least-once delivery semantics with idempotency.
- Minimal TCB; full audit visibility via dashboard and CLI.

### High-Level Architecture (host-level daemon service)
1. **Event Bus Core** — in-memory + SQLite-backed persistent queue.
2. **Timer Service** — cron-style scheduler for one-shot and recurring timers.
3. **Signal Router** — handles signals from bridges (email, calendar, file watcher, git webhook proxy).
4. **Wakeup Dispatcher** — restores Orchestrator/Worker snapshots and injects signal + memory context.
5. **Audit Proxy** — every event/timer/signal logged to central Merkle audit tree before processing.

All communication between the Event Bus and agent microVMs goes through **AegisHub** (vsock, ACL-enforced). No direct filesystem or memory access from VMs.

### Data Models
Timer, Signal, and WakeupEvent JSON schemas with fields for scheduling, delivery guarantees, and idempotency keys.

## Fit in the Broader System

Implemented as `internal/eventbus`. Drives the async primitives (`set_timer`, `subscribe_signal`, `request_human_approval` tools) used by the Orchestrator. Pairs with `docs/agentic-evolution.md` and `docs/memory-store.md`.
