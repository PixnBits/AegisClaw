# thought_events.go — cmd/aegisclaw

## Purpose
Implements `ThoughtEventBuffer` — a fixed-size ring buffer that records agent reasoning phases (thinking, acting, observing, finalizing) for observability.

## Key Types / Functions
- `ThoughtEventBuffer` — ring buffer (max 600 entries) protected by a `sync.RWMutex`.
- `Record(phase, tool, summary, details)` — appends a thought event with timestamp.
- `Snapshot()` — returns a copy of all entries.

## System Fit
Feeds the thought-stream display in the dashboard and the `--verbose` chat output. Bounded ring buffer prevents memory growth.

## Notable Dependencies
- Standard library only.
