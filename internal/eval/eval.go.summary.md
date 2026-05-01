# `eval.go` — Synthetic Evaluation Harness

## Purpose
Implements a declarative evaluation framework (Phase 5) that runs predefined acceptance-test scenarios against the daemon (or a stub) and produces structured pass/fail `EvalReport` objects. Three built-in scenarios cover the three core agentic workflows.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `ScenarioID` | Typed string: `background_research`, `oss_issue_to_pr`, `recurring_summary` |
| `CriterionResult` | Name, passed flag, optional message for a single acceptance criterion |
| `ScenarioResult` | All criteria results for one scenario plus duration and error |
| `EvalReport` | Aggregates all `ScenarioResult` values; `Passed`, `Failed`, `Total` counters; `Summary()` one-liner |
| `DaemonProbe` | Interface abstracting daemon interactions: `Chat`, `CallTool`, `GetMemory`, `AuditContains` |
| `Scenario` | `ID`, `Name`, `Description`, and a `RunnerFunc` |
| `Runner` | Holds the scenario registry; `RunAll()` and `RunScenario()` entry points |
| `runBackgroundResearch()` | Verifies memory store/retrieve tools and audit log entry |
| `runOSSIssueToPR()` | Verifies approval creation, pending list, and audit log entry |
| `runRecurringSummary()` | Verifies cron timer creation, pending list, audit log, and cleanup |

## Role in the System
Acts as a living acceptance test suite. In CI it is backed by the in-memory `testProbe` stub from `eval_test.go`; in integration environments it can be backed by a real daemon client over the Unix socket.

## Notable Dependencies
- Standard library only: `context`, `fmt`, `strings`, `time`
