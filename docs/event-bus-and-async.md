# Event Bus and Async Primitives

**Document Status**: Draft v0.1  
**Last Updated**: 2026-04-02  
**Owner**: Project Lead (Governance Court review required)  
**Related Documents**:  
- `docs/agentic-evolution.md` (Hierarchical architecture, persistent Orchestrator, ephemeral Workers, memory system)  
- `docs/agent-prompts.md` (Orchestrator and Worker prompts, few-shot async examples)  
- `docs/PRD.md` (Security invariants, auditability, isolation principles)  
- `docs/architecture.md` (Firecracker microVMs, AegisHub, vsock communication, Merkle audit tree)  

This document specifies the **Central Event Bus**, **Timer Service**, signal handling, and wakeup mechanisms that enable asynchronous and long-running agency in AegisClaw while preserving strict isolation and auditability.

## Goals

- Enable proactive, stateful behavior over hours/days/weeks (research completion, email replies, recurring summaries, OSS PR follow-ups, reminders).
- Support reliable wakeup of the Orchestrator or Worker agents from external events or timers.
- Guarantee exactly-once or at-least-once semantics with idempotency.
- Keep the trusted computing base (TCB) minimal and auditable.
- Integrate seamlessly with the hierarchical multi-agent design and tiered memory system.
- Provide full visibility and control via the web dashboard and CLI.

## High-Level Architecture

The Event Bus is a **lightweight host-level service** (not inside any Firecracker VM) that runs as part of the main AegisClaw daemon. It is sandboxed and has no direct network access except through approved bridges.

Components:

1. **Event Bus Core** — In-memory + persistent queue (SQLite-backed for durability).
2. **Timer Service** — Persistent cron-like scheduler for one-shot and recurring timers.
3. **Signal Router** — Handles incoming signals from bridges (email, calendar, file watcher, git webhook proxy, etc.).
4. **Wakeup Dispatcher** — Responsible for restoring Orchestrator/Worker snapshots and injecting signals + memory context.
5. **Audit Proxy** — Every event, timer, or signal is logged to the central Merkle audit tree before processing.

All communication between the Event Bus and agent microVMs goes through **AegisHub** (vsock) with strict ACLs. No direct filesystem or memory access.

## Data Models

### Timer
```json
{
  "timer_id": "string (uuid)",
  "name": "string",
  "type": "one-shot | cron",
  "trigger_at": "ISO8601 or null",
  "cron": "string or null",
  "payload": "json object",
  "task_id": "string (optional)",
  "owner": "user",
  "created_at": "ISO8601",
  "status": "active | fired | cancelled | expired"
}
```

### Signal
```json
{
  "signal_id": "string (uuid)",
  "source": "email | calendar | file | git | custom",
  "type": "reply | event | change | webhook",
  "payload": "json (sanitized)",
  "signature": "cryptographic signature from bridge",
  "received_at": "ISO8601",
  "task_id": "string (optional)"
}
```

### Subscription
```json
{
  "subscription_id": "string",
  "source": "string",
  "filter": "json",
  "task_id": "string",
  "active": "bool"
}
```

## Core Tools Exposed to Agents

All tools are discovered dynamically via `search_tools`. The Orchestrator (and authorized Workers) can call:

- `set_timer(name: str, trigger_at: str|null, cron: str|null, payload: json) → timer_id`
- `cancel_timer(timer_id: str) → bool`
- `list_pending_async(filter?: json) → list[Timer|Subscription]`
- `subscribe_signal(source: str, filter: json) → subscription_id`
- `unsubscribe_signal(subscription_id: str) → bool`

**Important**: These tools are implemented as thin proxies that forward requests to the host Event Bus via AegisHub. No direct access is granted.

## Timer & Signal Lifecycle

### Setting a Timer
1. Agent calls `set_timer` → validated by proxy → stored in Event Bus (SQLite) → audited.
2. Timer daemon wakes periodically and checks due timers.
3. When due: mark as "firing", generate signed signal, dispatch to Wakeup Dispatcher.

### Recurring (Cron) Timers
- Use standard cron syntax (e.g., `0 20 * * *` for 8pm daily).
- Each firing creates an independent signal with the original payload + firing timestamp.

### Signal Flow (Incoming or Timer-Fired)
1. Signal arrives (from bridge or Timer Service) → cryptographic validation.
2. If valid: persist to audit log → lookup associated task_id and memory.
3. Wakeup Dispatcher:
   - Restores the appropriate microVM snapshot (Orchestrator preferred; Worker if specified).
   - Injects the signal as the first `Observation`.
   - Prepends a compact memory summary from the tiered Memory Store (`retrieve_memory` for the task_id).
   - Starts/Reumes the ReAct loop with:
     ```
     Thought: Signal received from [source] for task [task_id]. Loading relevant memory...
     ```
4. Agent processes → may cancel timers, unsubscribe, store new memory, spawn Workers, or request human approval.
5. On completion or explicit cancel: cleanup resources.

### Idempotency & Recovery
- Every timer and signal carries a unique `signal_id`.
- Agents must design actions to be idempotent (e.g., check state before acting).
- On daemon restart or crash: Event Bus replays pending due timers (at-least-once semantics).
- Failed wakeups are retried (configurable backoff) and escalated to user via dashboard.

## Integration with Hierarchical Agents

- **Orchestrator** is the primary wakeup target for most signals (persistent snapshot).
- **Workers** can be spawned on signal if the Orchestrator decides a narrow task is needed (e.g., "process email reply").
- Snapshot reuse is strongly preferred for speed (sub-second restore on 3080/4080 hardware).
- Resource guardrails: max concurrent wakeups, max pending timers/subscriptions per user (default 20).

## Security & Compliance

- **Minimal TCB**: Event Bus runs with least-privilege (no internet, limited filesystem, seccomp).
- **Cryptographic Validation**: All signals from external bridges must be signed by the bridge’s private key (provisioned at setup).
- **Data Sanitization**: Payloads are stripped of raw secrets before storage/delivery.
- **GDPR/CCPA Support**:
  - All timers and subscriptions are queryable for deletion.
  - `privacy_delete` cascades to related signals and memories.
- **Auditability**: Every creation, firing, cancellation, and wakeup is appended to the Merkle tree with full context.
- **Resource Protection**: Hard limits prevent DoS via timer spam. Auto-pruning of expired items.

## Web Dashboard Integration

The dashboard exposes:
- Live list of timers and subscriptions with one-click cancel.
- Signal history (filtered by task or source).
- Wakeup logs and resource usage during restores.
- Manual trigger for testing.

## Open Questions & Trade-offs

- Snapshot frequency vs. cold boot: How often should Orchestrator snapshots be taken? (After every major state change?)
- Exactly-once vs. at-least-once: Current design leans at-least-once with agent-level idempotency. Is stronger guarantee needed?
- Bridge security model: Should bridges run in their own Firecracker VMs?
- Multi-user isolation: Per-user Event Bus queues?

## Decision Log

- **2026-04-02**: Event Bus runs as host-level service (minimal TCB) rather than inside a permanent VM to simplify timer scheduling and signal routing.
- **2026-04-02**: Adopted at-least-once delivery with agent responsibility for idempotency.
- **2026-04-02**: All signals validated cryptographically; no unauthenticated events accepted.
- **2026-04-02**: Preferred wakeup target is the persistent Orchestrator; Workers are spawned only when Orchestrator delegates.

**Next Actions**:
- Implement Event Bus core + SQLite persistence.
- Define bridge interface contract (signing, payload schema).
- Add snapshot management to the daemon.
- Governance Court review of this specification.

Any changes to timers, signals, or wakeup behavior must be proposed as a skill or architecture change and reviewed by the Governance Court.
