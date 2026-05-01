# tool_events.go — cmd/aegisclaw

## Purpose
Implements `ToolEventBuffer` — a fixed-size ring buffer that records start and finish events for every tool call executed during agent turns.

## Key Types / Functions
- `ToolEventBuffer` — ring buffer (max 400 entries) protected by a `sync.RWMutex`.
- `RecordStart(traceID, tool, args)` — appends a start entry with timestamp.
- `RecordFinish(traceID, tool, result, err)` — appends a finish entry with elapsed time.
- `Snapshot()` — returns a copy of all entries for the `/status` endpoint.

## System Fit
Powers the tool-call history shown in `aegisclaw status` and the dashboard. Bounded size prevents unbounded memory growth.

## Notable Dependencies
- Standard library only.
