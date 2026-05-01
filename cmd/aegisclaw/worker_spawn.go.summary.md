# worker_spawn.go — cmd/aegisclaw

## Purpose
Implements the `spawnWorker` function — creates an ephemeral worker microVM to run a background task autonomously. Enforces a concurrent-worker cap (default 4) and a task-timeout multiplier.

## Key Types / Functions
- `spawnWorkerParams` — `{ task_description, timeout_secs, skill_names, priority }`.
- `spawnWorker(ctx, env, params)` — validates params, checks worker cap, starts VM via `FirecrackerRuntime`, injects task context, registers the worker in `worker.Store`, and cleans up on exit.
- `maxTimeoutMultiplier = 3` — prevents runaway tasks by capping the requested timeout.
- Concurrent-worker cap stored in `env.config`; default is 4.

## System Fit
Called by `eventbus_daemon.go` (timer-fired tasks) and by the `worker.spawn` tool (agent-initiated tasks). Workers are tracked in `worker.Store` and visible via `aegisclaw worker list`.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/worker` — `Store`
- `github.com/PixnBits/AegisClaw/internal/sandbox` — Firecracker VM launch
