# `subscription.go` — Signal Subscription Store

## Purpose
Implements the persistent storage layer for `Subscription` and `Signal` records. Subscriptions represent an agent's interest in signals from external sources (email, calendar, git, webhooks, etc.).

## Key Types

| Symbol | Description |
|---|---|
| `SignalSource` | Enum: `email`, `calendar`, `file`, `git`, `webhook`, `custom`, `timer` |
| `Subscription` | `SubscriptionID`, `Source`, `Filter json.RawMessage`, `TaskID`, `Owner`, `CreatedAt`, `Active`, `ReceivedCount` |
| `Signal` | `SignalID`, `Source`, `Type`, `Payload`, `TaskID`, `SubscriptionID`, `TimerID`, `ReceivedAt`, `Processed` |
| `subscriptionStore` | Internal: mutex, in-memory maps for subs and signals, JSON file paths |

## Key Internal Methods
- `newSubscriptionStore(dir)` — Opens or creates `subscriptions.json` and `signals.json`
- `subscriptionStore.addSub()`, `removeSub()`, `listSubs()`, `countActive()` — Subscription CRUD
- `subscriptionStore.addSignal()`, `listSignals()`, `markProcessed()` — Signal delivery tracking

The public API (`Subscribe`, `Unsubscribe`, `DeliverSignal`, `ListSignals`) is exposed on `Bus`.

## Role in the System
Enables the agent to register interest in external events and have them delivered as wakeup signals. The timer daemon creates synthetic `SourceTimer` signals when timers fire, then routes them through the subscription layer.

## Notable Dependencies
- Standard library: `encoding/json`, `os`, `sync`, `time`
- `github.com/google/uuid`
