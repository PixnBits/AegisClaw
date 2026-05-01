# `approval.go` — Human Approval Queue

## Purpose
Implements the persistent storage layer for `ApprovalRequest` records — the mechanism by which the agent pauses and awaits human sign-off before executing high-risk operations.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `ApprovalStatus` | Enum: `pending`, `approved`, `rejected`, `expired` |
| `ApprovalRequest` | Full approval record: `ApprovalID`, `Title`, `Description`, `RiskLevel`, `Payload`, `TaskID`, `RequestedBy`, `CreatedAt`, `ExpiresAt`, `Status`, decision fields |
| `approvalStore` | Internal struct: mutex-protected in-memory map + JSON file path |
| `newApprovalStore()` | Opens or creates `approvals.json` in the event bus directory |
| `approvalStore.load()` | Reads and deserializes the JSON array from disk |
| `approvalStore.save()` | Marshals the in-memory map to disk, sorted by `CreatedAt` |

The public API (`RequestApproval`, `DecideApproval`, `ListApprovals`, `GetApproval`) is exposed on `Bus` in `bus.go`.

## Role in the System
Enables the human-in-the-loop safety gate required by the PRD. The `request_human_approval` tool (called by the agent ReAct loop) creates an `ApprovalRequest` via `Bus.RequestApproval()`; the dashboard and CLI `decide` commands call `Bus.DecideApproval()`.

## Notable Dependencies
- Standard library: `encoding/json`, `os`, `sync`, `time`
- `github.com/google/uuid`
