# Event System Specification

## Overview
AegisClaw uses a **lightweight, AegisHub-mediated Event System** instead of a full traditional EventBus. This keeps complexity low while still supporting necessary signals, notifications, and background coordination.

## Design Principles

- All events go through **AegisHub** (no direct component-to-component messaging)
- Events are **signed and audited**
- The system supports both **fire-and-forget** and **request-response** patterns
- Persistent timers are stored in Store VM

## Core Commands (noun.verb)

- `event.publish <event_name> [payload]`
- `event.subscribe <event_name>`
- `event.unsubscribe <event_name>`

**Example event names:**
- `skill.deployed`
- `court.decision.made`
- `safe_mode.activated`
- `agent.session.updated`
- `timer.fired.daily-summary`

## Event Flow

1. Any component publishes an event via AegisHub
2. AegisHub routes the event to authorized subscribers
3. All events are logged in the tamper-evident audit trail
4. Persistent timers (cron-like) are managed by Store VM + Event System

## Use Cases

- Skill deployment → notify all Agent Runtimes to refresh `tool.list`
- Court decision → notify user and relevant agents
- Safe Mode trigger → broadcast to all components
- Background task completion → update UI and monitoring
- Scheduled tasks (daily report, etc.)

## Implementation Notes

- Lightweight in-memory subscribers per Agent Runtime
- No dedicated EventBus component (reduces attack surface)
- Events carry `trace_id` for correlation
- Rate limiting and authorization enforced by AegisHub

## Related Documents
- `../aegishub.md` — Central mediator for all events
- `../store-vm.md` — Persistent timer storage
- `../skill-discovery.md`
- `../safe-mode.md`
- `../builder-security-gates.md`

## Implementation Status (Phase 7.2)
- In-process Bus with timers, approval queues, PublishPrivileged + security.Manager signing hook.
- Concrete publishers: orchestrator (vm.started/stopped), Court personas (court.decision.made).
- Consumers: agent reacts to approval.decision; dynamic skill index refresh.
- All privileged events have Merkle-style signing path available.

## Traceability
**Driven by:**
- Need for coordinated signals without high complexity
- Lessons from previous `internal/eventbus`
- Multi-agent coordination and background tasks (User Journeys #3, #5, #8)