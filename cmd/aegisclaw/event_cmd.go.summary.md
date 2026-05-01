# event_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw event` subcommand tree: `timers`, `signals`, `approvals list`, `approvals approve`, and `approvals reject`. Provides visibility into and control over scheduled events and human-in-the-loop approval gates.

## Key Types / Functions
- `runEventTimers(cmd, args)` — lists configured event-bus timers with next-fire times.
- `runEventSignals(cmd, args)` — lists pending signals queued in the event bus.
- `runEventApprovalsList(cmd, args)` — lists approval requests awaiting human decision.
- `runEventApprovalsApprove(cmd, args)` — approves an event by ID, allowing the agent to proceed.
- `runEventApprovalsReject(cmd, args)` — rejects an event by ID with an optional reason.

## System Fit
Human-in-the-loop control plane. Approval operations are audit-logged with the operator's identity.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
