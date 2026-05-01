# status.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw status` command. Queries the running daemon for health, version, and runtime information and prints a human-readable or JSON summary.

## Key Types / Functions
- `runStatus(cmd, args)` — calls `api.Client.Status()`, formats daemon state (running/stopped, version, uptime, active skills, VM count, safe-mode flag).

## System Fit
Primary operational health check for operators. Uses the same daemon socket as `stop`; exits non-zero if the daemon is unreachable so it can be used in scripts.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
