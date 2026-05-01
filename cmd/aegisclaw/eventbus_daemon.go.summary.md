# eventbus_daemon.go — cmd/aegisclaw

## Purpose
Runs a background goroutine that polls `EventBus.CheckAndFire()` periodically. When a timer fires and its payload includes a `task_description`, automatically spawns an ephemeral worker VM.

## Key Types / Functions
- `startEventBusDaemon(ctx, env)` — starts the polling loop; calls `env.eventBus.CheckAndFire()` on a fixed interval.
- `timerSpawnPayload` — JSON struct `{ task_description string }` embedded in timer payloads to trigger worker spawns.
- At-least-once delivery: if the daemon restarts while a timer is due, the next poll will fire it.

## System Fit
The bridge between the event bus (timers, signals) and the worker VM system. Decouples scheduling from execution.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/eventbus` — `CheckAndFire`
- `worker_spawn.go` — `spawnWorker`
