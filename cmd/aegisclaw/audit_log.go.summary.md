# audit_log.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw audit log`, `audit why <id>`, and `audit trace <trace-id>` subcommands. Retrieves audit records from the running daemon for operator inspection.

## Key Types / Functions
- `runAuditLog(cmd, args)` — lists audit entries; supports `--since`, `--skill`, and `--limit` flags.
- `runAuditWhy(cmd, args)` — fetches the full rationale for a specific audit entry by ID.
- `runAuditTrace(cmd, args)` — retrieves all audit entries sharing a trace ID.

## System Fit
Read-only introspection path into the immutable audit chain stored by `internal/audit`. Operators use this to understand why the agent took an action.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
