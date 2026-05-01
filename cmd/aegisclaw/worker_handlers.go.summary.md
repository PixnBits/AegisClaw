# worker_handlers.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers for worker VM management: `worker.list` and `worker.status`.

## Key Types / Functions
- `makeWorkerListHandler(env)` — returns all worker records; `{ active_only bool }` filters to running workers.
- `makeWorkerStatusHandler(env)` — returns a single worker record by `worker_id`; returns a 404-style error if not found.

## System Fit
Read-only view into `worker.Store`. The CLI's `aegisclaw worker list/status` commands call these handlers via the daemon API.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/worker`
- `github.com/PixnBits/AegisClaw/internal/api`
