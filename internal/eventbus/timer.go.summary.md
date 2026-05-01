# `timer.go` — Timer Store & Cron Utilities

## Purpose
Implements the persistent storage layer for `Timer` records and provides the cron-expression evaluator used to compute `NextFireAt` for recurring timers.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `TimerType` | `one-shot` or `cron` |
| `TimerStatus` | `active`, `fired`, `cancelled`, `expired` |
| `Timer` | Full timer record: `TimerID`, `Name`, `Type`, `TriggerAt`, `Cron`, `Payload`, `TaskID`, `Owner`, `CreatedAt`, `LastFiredAt`, `NextFireAt`, `Status`, `FiredCount` |
| `timerStore` | Internal: mutex, in-memory map keyed by `TimerID`, JSON file path |
| `newTimerStore(dir)` | Opens or creates `timers.json` |
| `NextCronTime(expr, from)` | Evaluates a cron expression against a reference time and returns the next fire UTC time. Supports `@daily`, `@hourly`, `@weekly`, `@monthly`, `*/N` (every N minutes), and standard 5-field cron |
| `newTimerID()` | Returns a UUID string |

## Role in the System
Underpins the `set_timer` / `cancel_timer` agent tools. The timer daemon loop in `bus.go` calls `NextCronTime` to advance `NextFireAt` after each cron fire and persists the updated record.

## Notable Dependencies
- Standard library: `encoding/json`, `os`, `sync`, `time`
- `github.com/google/uuid`
