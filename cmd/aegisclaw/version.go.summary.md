# version.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw version` command. Prints build metadata embedded at link time via `-ldflags`.

## Key Types / Functions
- `buildCommit string` — Git commit SHA (set via `-ldflags "-X ...buildCommit=<sha>"`).
- `buildDate string` — ISO-8601 build timestamp (set via `-ldflags`).
- `runVersion(cmd, args)` — prints version, commit, and date; falls back to `"dev"` / `"unknown"` for local builds.

## System Fit
Lightweight informational command. Build metadata is injected by the Makefile release target.

## Notable Dependencies
- Standard library only.
