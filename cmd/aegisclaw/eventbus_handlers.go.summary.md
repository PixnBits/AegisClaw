# eventbus_handlers.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers for event bus management: approvals list, approval decision, timer list, and signal dispatch.

## Key Types / Functions
- `makeApprovalsListHandler(env)` — lists all or pending-only approval requests; `{ pending_only bool }`.
- `approvalsDecideRequest` — `{ approval_id, approved, decided_by, reason }`.
- `makeApprovalsDecideHandler(env)` — records a human approve/reject decision; signs the audit entry with the kernel key.
- `makeTimersListHandler(env)` — lists all configured timers with next-fire times.
- `makeSignalDispatchHandler(env)` — injects an external signal into the event bus.

## System Fit
Implements the human-in-the-loop control plane. Every decision is kernel-signed and audit-logged with the operator's identity.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/eventbus`
- `github.com/PixnBits/AegisClaw/internal/kernel`
