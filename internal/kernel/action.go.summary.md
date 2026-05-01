# `action.go` — Kernel Action Types & Signed Action

## Purpose
Defines every auditable operation type (`ActionType`) the kernel can log, plus the `Action` and `SignedAction` structs that form the entries in the Merkle audit chain.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `ActionType` | Typed string; ~50 constants covering sandbox, skill, proposal, builder, secret, memory, event bus, worker, and system component lifecycle events |
| `Action` | `ID` (UUID), `Type`, `Source`, `Payload json.RawMessage`, `Timestamp` |
| `SignedAction` | Wraps `Action` with an Ed25519 `Signature []byte` |
| `NewAction(type, source, payload)` | Constructor; auto-assigns UUID and UTC timestamp |
| `Action.Validate()` | Returns error if `ID`, `Type` (unknown), `Source`, or `Timestamp` is missing/invalid |
| `Action.Marshal()` | Returns canonical JSON bytes used as the Ed25519 signing input |
| `validActionTypes` | Compiled map of all recognized `ActionType` values for validation |

## Role in the System
Every kernel operation creates an `Action`, which is signed via `Kernel.SignAndLog()` and appended to the Merkle audit log. The `ActionType` constants also serve as the vocabulary for audit-log queries (e.g., `AuditContains` in the eval harness).

## Notable Dependencies
- Standard library: `encoding/json`, `fmt`, `time`
- `github.com/google/uuid`
