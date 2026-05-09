# EventBus Specification

## Overview
The EventBus is the internal pub/sub system that allows loose coupling between components while maintaining strict security boundaries.

## Responsibilities

- Publish and subscribe to events across components (via AegisHub)
- Persistent timers / cron-style scheduled tasks
- Approval workflow queues
- Skill deployment notifications (`skill.deployed`, `skill.updated`)
- Safe Mode triggers
- Background task lifecycle events

## Event Types (noun.verb style)

**Core Events:**
- `skill.deployed`
- `skill.updated`
- `agent.session.started`
- `court.decision.made`
- `safe_mode.activated`
- `timer.fired.<name>`

## Implementation Notes

- Lightweight in-process + AegisHub-mediated pub/sub
- Persistent timers survive restarts (stored in Store VM)
- Events are signed and logged in the tamper-evident audit trail
- Subscribers can only receive events they are authorized for

## Related Documents
- `../aegishub.md` — Primary transport
- `../store-vm.md` — Persistent timer storage
- `../skill-discovery.md`
- `../safe-mode.md`

## Traceability
**Driven by:**
- Old `internal/eventbus` implementation
- Need for coordinated background behavior
- Multi-agent and scheduled task requirements