# `bus.go` — Event Bus Core

## Purpose
Central coordinator for the AegisClaw event bus. Aggregates the timer, subscription, and approval stores into a single `Bus` type with a unified constructor and a thread-safe public API.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `Config` | `Dir`, `MaxPendingTimers`, `MaxSubscriptions` |
| `FiredEvent` | Carries the fired `Timer` and its generated `Signal` to the wakeup dispatcher |
| `WakeupFunc` | Callback invoked when a timer fires; should restore snapshot and inject signal |
| `Bus` | Aggregates `timerStore`, `subscriptionStore`, `approvalStore`, optional `WakeupFunc` |
| `New(cfg)` | Opens/creates all three stores; returns error if directory is not writable |
| `Bus.SetWakeupFunc(fn)` | Registers the callback before starting the timer daemon |
| `Bus.SetTimer(params)` | Creates a one-shot or cron timer; enforces `MaxPendingTimers` cap |
| `Bus.CancelTimer(id)` | Marks a timer as cancelled; idempotent |
| `Bus.ListTimers(status)` | Returns timers filtered by status (empty = all) |
| `Bus.Subscribe(source, filter, taskID, owner)` | Creates a signal subscription; enforces `MaxSubscriptions` cap |
| `Bus.RequestApproval(...)` | Creates a pending `ApprovalRequest`; enforces `MaxPendingApprovals` cap |
| `Bus.DecideApproval(id, approved, decidedBy, reason)` | Transitions approval to `approved` or `rejected` |
| `Bus.RunTimerDaemon(ctx)` | Background goroutine: polls every `timerCheckInterval`, fires due timers, calls `WakeupFunc` |

## Role in the System
The event bus is the async backbone of the agent's temporal awareness. It is created once by the daemon and injected into the tool-handler layer so the agent's `set_timer`, `subscribe`, and `request_human_approval` tools can register work to be done later.

## Notable Dependencies
- Standard library: `encoding/json`, `os`, `sync`, `time`
- `github.com/google/uuid`
