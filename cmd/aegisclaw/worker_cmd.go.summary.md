# worker_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw worker` subcommand tree: `list` and `status`. Provides visibility into ephemeral worker VMs spawned for background tasks.

## Key Types / Functions
- `runWorkerList(cmd, args)` — lists worker VMs; `--active` flag filters to currently running workers only.
- `runWorkerStatus(cmd, args)` — prints detailed status for a single worker by ID, including task description, elapsed time, and exit code.

## System Fit
Observability layer on top of `worker.Store` (managed by `worker_spawn.go`). Both subcommands route via daemon API.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
