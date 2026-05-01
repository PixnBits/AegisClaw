# `bus_test.go` — Tests for the Event Bus

## Purpose
Covers the full public API of `Bus`: timer CRUD, subscription CRUD, approval lifecycle, resource limits, cron expression evaluation, and the timer daemon background goroutine.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestBus_SetAndCancelTimer` | Timer created, listed, cancelled; re-cancel is a no-op |
| `TestBus_TimerResourceLimit` | `MaxPendingTimers` cap is enforced; error after limit is reached |
| `TestBus_CronNextFireAt` | `NextFireAt` is populated for cron timers |
| `TestBus_Subscribe` | Subscription created, listed, and unsubscribed |
| `TestBus_SubscriptionResourceLimit` | `MaxSubscriptions` cap enforced |
| `TestBus_RequestAndDecideApproval` | Approval created as `pending`, transitioned to `approved` |
| `TestBus_ApprovalExpiry` | Approval with past `ExpiresAt` is auto-expired on list |
| `TestBus_TimerDaemon_Fires` | One-shot timer set to fire immediately; `WakeupFunc` called within 2s |
| `TestBus_Persistence` | Timer and approval survive a `Bus` close and re-open from same directory |

## Role in the System
Regression safety for the event bus — the component that drives all agent async scheduling, signal delivery, and human approval gating.

## Notable Dependencies
- Package under test: `eventbus`
- Standard library (`context`, `testing`, `time`)
