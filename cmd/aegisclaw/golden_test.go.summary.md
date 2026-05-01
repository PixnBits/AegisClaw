# golden_test.go — cmd/aegisclaw

## Purpose
Golden-file test harness for ReAct trace assertions. Defines trace event types and comparison utilities for snapshot-based testing.

## Key Types / Functions
- `TraceEventType` — enum: `thought`, `tool_called`, `tool_result`, `task_progress`, `task_complete`.
- `TraceEvent` — one step in the ReAct loop, used for assertion and diff.
- Golden-file helpers: load expected trace from JSON files in `testdata/golden/`, compare against actual trace with field normalisation (UUIDs, timestamps, durations).
- Used by `react_journey_test.go` and `first_skill_tutorial_test.go` for multi-step scenario assertions.

## System Fit
Prevents silent regressions in the agent's reasoning trace. Updating a golden file is an explicit reviewer decision.

## Notable Dependencies
- Standard library (`encoding/json`, `os`, `path/filepath`, `strings`, `testing`).
