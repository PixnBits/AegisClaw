# Package `internal/eventbus` — Event Bus

## Purpose
The host-level async event backbone for AegisClaw. Provides three persistent services: a **Timer** service (one-shot and cron) for agent wakeups, a **Signal Subscription** registry for external event sources, and a **Human Approval** queue for high-risk operations. All state is persisted as JSON files and changes are recorded in the Merkle audit chain.

## Files

| File | Description |
|---|---|
| `bus.go` | `Bus` coordinator, `Config`, `WakeupFunc`, timer/subscription/approval API, `RunTimerDaemon` |
| `bus_test.go` | Full API coverage: CRUD, resource limits, timer daemon firing, persistence |
| `timer.go` | `Timer`, `timerStore`, `NextCronTime`, `TimerType`, `TimerStatus` |
| `subscription.go` | `Subscription`, `Signal`, `subscriptionStore`, `SignalSource` |
| `approval.go` | `ApprovalRequest`, `approvalStore`, `ApprovalStatus` |

## Key Abstractions

- **`Bus`** — single entry point; aggregates all three stores and the optional `WakeupFunc`
- **`Timer`** — one-shot (by UTC time) or cron (by expression); `NextCronTime` evaluates `@daily`, `@hourly`, standard 5-field, and `*/N` syntax
- **`Subscription`** — interest registration for a `SignalSource`; receives `Signal` events
- **`ApprovalRequest`** — human-in-the-loop gate; pending until `DecideApproval` is called
- **`WakeupFunc`** — callback invoked by `RunTimerDaemon` when a timer fires; intended to restore a VM snapshot and inject the fired signal as the agent's first observation

## Resource Guardrails
Active timers and active subscriptions each have independent hard caps (`MaxPendingTimers`, `MaxSubscriptions`) to prevent runaway async growth.

## How It Fits Into the Broader System
Created once by the daemon and injected into the tool-handler layer. The agent's `set_timer`, `subscribe`, and `request_human_approval` tools call the `Bus` API. The dashboard and CLI `decide` commands call `DecideApproval`. `RunTimerDaemon` runs as a long-lived daemon goroutine alongside the main API server.

## Notable Dependencies
- Standard library: `encoding/json`, `os`, `sync`, `time`
- `github.com/google/uuid`
- `github.com/robfig/cron` (for cron expression parsing)
