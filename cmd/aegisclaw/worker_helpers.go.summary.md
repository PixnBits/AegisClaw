# worker_helpers.go — cmd/aegisclaw

## Purpose
Provides human-readable formatting for worker records used by the `worker status` CLI command.

## Key Types / Functions
- `formatWorkerRecord(w *worker.WorkerRecord) string` — produces a multi-line summary with role, status, step count, task description, task ID, spawn time, duration, tools granted, result, and error fields. Truncates long values for readability.
- `truncate(s, n)` — local truncation helper (truncates to `n` runes with `…`).

## System Fit
Called by `runWorkerStatus` in `worker_cmd.go` for non-JSON output. Analogous to `helpers.go`'s `truncateStr` but dedicated to worker-record formatting.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/worker` — `WorkerRecord`
- Standard library (`fmt`, `time`).
