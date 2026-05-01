# Package `internal/eval` — Synthetic Evaluation Harness

## Purpose
Provides a declarative acceptance-test framework (Phase 5) for AegisClaw's three core agentic workflows. Scenarios are run against a `DaemonProbe` abstraction so CI can execute them entirely in-process without a live daemon, while integration environments can point the same scenarios at a real daemon.

## Files

| File | Description |
|---|---|
| `eval.go` | `Runner`, `Scenario`, `DaemonProbe` interface, three built-in `RunnerFunc` implementations, `EvalReport` |
| `eval_test.go` | In-process `testProbe` stub + tests for all three scenarios and `RunAll` |

## Key Abstractions

- **`DaemonProbe`** — interface: `Chat`, `CallTool`, `GetMemory`, `AuditContains`; backed by real API client or test stub
- **`Scenario`** — `ID`, `Name`, `Description`, `Runner RunnerFunc`; entirely declarative
- **`Runner`** — registry of scenarios; `RunAll()` and `RunScenario()` produce `EvalReport`
- **`EvalReport`** — structured pass/fail summary with per-criterion detail and timing

## Built-In Scenarios

| Scenario | Core Criteria |
|---|---|
| `background_research` | Memory store/retrieve tools callable; audit log entry present |
| `oss_issue_to_pr` | Approval created and appears in pending list; audit log entry present |
| `recurring_summary` | Cron timer created, listed, and cancellable; audit log entry present |

## How It Fits Into the Broader System
Used in CI via the `testProbe` stub and can be wired to the real daemon for integration smoke tests before releases. The same `DaemonProbe` interface could be implemented against the Unix socket API client.

## Notable Dependencies
- `internal/eventbus` (test stub only)
- Standard library: `context`, `fmt`, `strings`, `time`
