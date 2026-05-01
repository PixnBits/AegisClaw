# `eval_test.go` — Tests for the Evaluation Harness

## Purpose
Provides a complete in-memory `DaemonProbe` stub (`testProbe`) backed by an in-process `eventbus.Bus` and a simple key-value memory map. Runs all three built-in scenarios and the `RunAll` aggregator entirely in-process without a live daemon.

## Key Components

| Component | Description |
|---|---|
| `memStub` | Thread-safe in-memory key-value store simulating the Memory Store |
| `testProbe` | Implements `DaemonProbe`; routes `CallTool` calls to `memStub` and `eventbus.Bus` |
| `testProbe.CallTool()` | Dispatches: `store_memory`, `retrieve_memory`, `list_pending_async`, `request_human_approval`, `set_timer`, `cancel_timer`, `worker_status` |
| `testProbe.AuditContains()` | Always returns `(true, nil)` — no real audit log in unit tests |

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestEval_BackgroundResearch` | All 4 criteria pass with the in-memory stub |
| `TestEval_OSSIssueToPR` | All 4 criteria pass; approval appears in pending list |
| `TestEval_RecurringSummary` | Timer created, in pending list, cancelled successfully |
| `TestEval_RunAll` | 3 scenarios run; `report.Total == 3`; `Summary()` non-empty |

## Role in the System
Ensures the eval framework and its three acceptance scenarios are always runnable in CI without external dependencies.

## Notable Dependencies
- `internal/eval`, `internal/eventbus`
- Standard library (`context`, `sync`, `testing`)
