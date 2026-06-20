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
- Web portal real-time UI updates (overview stats, per-conversation deltas) via efficient topic subscriptions (see STOMP section)

## Implementation Notes

- Lightweight in-memory subscribers per Agent Runtime
- No dedicated EventBus component (reduces attack surface)
- Events carry `trace_id` for correlation
- Rate limiting and authorization enforced by AegisHub

## STOMP Topic Subscriptions for Presentation Layer

To relieve unnecessary data transfer and resource usage (network to browser, portal CPU for filtering/bundling, upstream event fan-out), the web portal exposes **STOMP over WebSocket** as the preferred mechanism for fine-grained, topic-based pub/sub from browser clients.

The web portal acts as a **trusted STOMP gateway** (presentation-only component, all traffic still mediated by Host Daemon reverse proxy per `web-portal/web-portal-vm.md`):

- Browser clients (embedded JS) use a lightweight STOMP-over-WS client (self-contained, no external deps) to `CONNECT` to the portal's STOMP endpoint (e.g. `/stomp` or WS upgrade on existing), `SUBSCRIBE` to topics, receive `MESSAGE` frames with JSON payloads, and `UNSUBSCRIBE` when leaving a view or closing tab.
- **Topic naming** (STOMP destination convention):
  - `/topic/overview.stats` — system stats, running microVMs/workers, pending approvals, resource usage, host load. Primary feed for Overview screen stat cards and tables. Replaces or supplements the global SSE ticker bundles.
  - `/topic/conversation.{sessionId}.updates` — incremental message content_deltas, thought_deltas, tool_events, status changes for a specific chat session. Enables per-conversation streaming with minimal overhead (no global broadcast to all clients).
  - `/topic/approvals.pending` — updates to the pending approvals list and decisions.
  - `/topic/canvas.events` (or finer `/topic/worker.{id}.updates`, `/topic/tool.{callId}.events`) — for live Canvas/agent monitoring views.
- On browser `SUBSCRIBE`, the portal translates (where cross-component coordination is needed) to internal `event.subscribe` calls via the trusted vsock bridge, so AegisHub routes relevant publishes. Local subscriber management in the portal handles efficient in-process fan-out for STOMP sessions.
- Internal publishes (orchestrator, chat system, Court, skill deploy, etc.) are mapped by the portal STOMP handler to only active matching topic subscribers. MESSAGE payloads preserve existing delta/cursor patterns, trace_ids, and JSON shapes from event buffers.
- **Benefits**:
  - Reduced network transfer: clients receive only subscribed topics (no irrelevant bundles or global ticker noise).
  - Lower compute: portal avoids per-client filtering of a single broad stream; AegisHub/event routing is more targeted.
  - Explicit lifecycle: `UNSUBSCRIBE` on navigation matches user intent and prevents stale connections/subscriptions.
  - Better multi-view / multi-tab support (different tabs subscribe to different subsets).
- STOMP features: heartbeats (keepalive), receipts (reliability for critical updates), error frames. Implement minimal STOMP 1.2 subset in Go (or small audited helper) to keep TCB and attack surface minimal.
- **Transition & coexistence**: Existing global SSE `/events` (with cursors) and chat hybrid streaming remain during migration for compatibility and E2E stability. New or migrated flows (Overview stats, per-session chat) adopt STOMP topics. The current `ToolEventBuffer`/`ThoughtEventBuffer` and contract tests extend naturally to STOMP MESSAGE payloads.
- **Security & constraints**: No new privileged code or external broker. All STOMP traffic stays inside the trusted portal VM sandbox and Host proxy. AegisHub ACLs, signing, and audit trail continue to apply to underlying events. Rate limiting and frame validation at STOMP layer as defence-in-depth.

This extension keeps the event system as the single source of truth while giving the presentation layer an efficient pub/sub surface aligned with the isolation and minimalism principles.

## Related Documents

- `../aegishub.md` — Central mediator for all events
- `../store-vm.md` — Persistent timer storage
- `web-portal/implementation-current.md` — STOMP gateway implementation and topic usage in UI
- `../skill-discovery.md`
- `../safe-mode.md`
- `../builder-security-gates.md`

## Implementation Status (Phase 7.2)

- In-process Bus with timers, approval queues, PublishPrivileged + security.Manager signing hook.
- Concrete publishers: orchestrator (vm.started/stopped), Court personas (court.decision.made).
- Consumers: agent reacts to approval.decision; dynamic skill index refresh.
- All privileged events have Merkle-style signing path available.
- **STOMP gateway extension planned** for web portal real-time efficiency (this spec).

## Traceability
**Driven by:**
- Need for coordinated signals without high complexity
- Lessons from previous `internal/eventbus`
- Multi-agent coordination and background tasks (User Journeys #3, #5, #8)
- Requirement to reduce data transfer and compute for real-time UI updates while preserving paranoid isolation model