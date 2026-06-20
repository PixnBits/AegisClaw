# Real-time Contracts Specification

**Status**: Target State

## Overview

This document defines the real-time communication contracts for the Web Portal. It focuses on STOMP-over-WebSocket as the primary mechanism for efficient, targeted updates while maintaining the strict paranoid security model of AegisClaw.

The portal must remain **presentation-only**. All real-time data originates from the Host Daemon via the vsock bridge. The portal never generates business logic or persists state.

## Design Constraints (Paranoid Security)

- Minimal TCB: The STOMP handler and client must be small, auditable, and self-contained.
- No secrets or sensitive data may transit in a way that could be exposed to the browser.
- All subscriptions are view-specific and must be cleaned up promptly.
- Frame validation, size limits, and rate limiting are mandatory on the server side.
- Fallback to existing SSE must remain functional and secure.

## STOMP Protocol Subset

The portal implements a minimal, safe subset of STOMP 1.2:
- `CONNECT` / `CONNECTED`
- `SUBSCRIBE` / `UNSUBSCRIBE`
- `MESSAGE`
- `DISCONNECT`
- Heartbeats (both directions)

No transactions, ACK/NACK, or advanced features are used.

## Topic Naming & Purpose

Topics follow a consistent, hierarchical naming scheme:

- `/topic/overview.stats` — Global dashboard metrics and active work summary.
- `/topic/conversation.{sessionId}.updates` — Per-conversation chat deltas, thoughts, and tool events.
- `/topic/channel.{channelId}.activity` — Channel activity feed updates (messages, proactive agent updates, proposal events).
- `/topic/canvas.events` — Inter-agent pipeline and agent status updates.
- `/topic/approvals.pending` — Court / approvals list updates.
- `/topic/proposal.{proposalId}.updates` — Individual proposal status and vote changes.

Topic names are stable and versioned implicitly through payload structure.

## Payload Shapes (Target)

All `MESSAGE` frames carry JSON payloads. The following shapes are the target contracts:

### Overview Stats
```json
{
  "type": "overview.stats",
  "timestamp": "2026-...",
  "active_agents": { "total": 12, "by_role": {...} },
  "background_tasks": { "total": 5, "avg_progress": 67 },
  "pending_proposals": 3
}
```

### Conversation Update
```json
{
  "type": "conversation.update",
  "session_id": "...",
  "delta": { /* streaming content, thought, or tool event */ },
  "timestamp": "..."
}
```

### Channel Activity
```json
{
  "type": "channel.activity",
  "channel_id": "...",
  "event": { /* message, proactive_update, proposal_event, etc. */ },
  "timestamp": "..."
}
```

### Canvas Event
```json
{
  "type": "canvas.event",
  "agent_id": "...",
  "task_id": "...",
  "stage": "Execute",
  "progress": 45,
  "timestamp": "..."
}
```

Payloads must be small, schema-validated on the server where feasible, and never contain raw secrets or internal addresses.

## Subscription Lifecycle

1. Client connects via WebSocket and sends `CONNECT` (with heartbeat negotiation).
2. On view mount, client sends `SUBSCRIBE` for relevant topics.
3. Server maintains a map of `topic → active sessions`.
4. On relevant internal event, server routes only to matching subscribers.
5. On view unmount or tab hidden, client sends `UNSUBSCRIBE`.
6. On disconnect or navigation, server cleans up subscriptions.

Subscriptions are **view-scoped**, not global. The server must enforce cleanup to prevent resource leaks.

## Security & Validation Rules

- All incoming STOMP frames are validated for size, structure, and allowed commands.
- `SUBSCRIBE` frames are checked against an allow-list of topics the portal is permitted to subscribe to.
- No client can subscribe to topics belonging to other sessions or channels they do not have access to.
- `MESSAGE` frames sent to clients are sanitized; sensitive fields are stripped before transmission.
- Rate limiting is applied per connection and per topic.
- Heartbeat intervals are negotiated and enforced.

## Fallback to SSE

If STOMP connection fails or is unavailable, the portal gracefully degrades to the existing SSE `/events` endpoint. The same sanitization and access control rules apply.

## Implementation Notes (Go)

- Use a lightweight, self-contained STOMP server (or a minimal wrapper around `golang.org/x/net/websocket` + custom frame handling).
- Keep the STOMP handler in `internal/dashboard/stomp` with clear separation from template rendering.
- Subscription manager must be concurrency-safe and support efficient fan-out.
- All frame processing must happen with strict timeouts and resource limits.

This contract ensures efficient real-time updates while preserving the paranoid security model.