# worker_test.go

## Purpose
Tests for the worker `Store` and the role configuration functions. Store tests exercise the full CRUD lifecycle with persistence verification across store restarts. Role configuration tests verify that all roles have non-empty prompts and positive timeouts, guarding against accidental empty strings or zero values.

## Key Types and Functions
- `TestUpsert_New`: creates a new worker record via `Upsert` and verifies it is returned by `Get`
- `TestUpsert_Update`: upserts a record twice with different status values; verifies the updated value is persisted
- `TestGet_NotFound`: calls `Get` with an unknown ID; verifies a not-found error is returned
- `TestList_AllRecords`: upserts multiple workers and verifies `List(false)` returns all of them
- `TestList_ActiveOnly`: upserts workers in active and completed states; verifies `List(true)` returns only the active ones
- `TestPersistence`: upserts a record, creates a new `Store` from the same directory, and verifies the record is still readable
- `TestCountActive`: verifies `CountActive` returns correct counts as workers transition through states
- `TestRolePrompts_NonEmpty`: iterates all four roles and verifies `RolePrompt` returns non-empty strings
- `TestRoleTimeouts_Positive`: verifies `RoleDefaultTimeoutMins` returns a positive value for each role
- `TestRoleMaxToolCalls_Positive`: verifies `RoleMaxToolCalls` returns a positive value for each role

## Role in the System
Prevents regressions in worker lifecycle tracking and misconfiguration of role parameters that would break the ReAct FSM iteration budgets and VM timeouts.

## Dependencies
- `testing`, `t.TempDir()`
- `internal/worker`: `Store`, `WorkerRecord`, `Role`, `RolePrompt`, `RoleMaxToolCalls`, `RoleDefaultTimeoutMins`
