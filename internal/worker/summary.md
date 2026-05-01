# Package: worker

## Overview
The `worker` package manages ephemeral sub-agent worker VMs spawned by the AegisClaw orchestrator for specialised subtasks. It defines four worker roles (Researcher, Coder, Summarizer, Custom), each with a dedicated system prompt, tool call budget, and timeout limit. A persistent JSON store tracks every worker's full lifecycle, enabling crash recovery and status reporting.

## Files
- `store.go`: `Store`, `WorkerRecord`, `Role`, `WorkerStatus` — persistent JSON array with atomic writes and deep-copy isolation
- `roles.go`: `RolePrompt`, `RoleMaxToolCalls`, `RoleDefaultTimeoutMins` — role-specific agent configuration
- `worker_test.go`: Tests for store CRUD/persistence/CountActive and role configuration correctness

## Key Abstractions
- `WorkerRecord`: the complete lifecycle record for one worker VM — from spawn to destruction, including tools granted, step count, and final result
- `Store`: atomic JSON persistence with sort-by-SpawnedAt ordering; `List(activeOnly)` for in-progress monitoring
- Role system: four predefined roles with scope-constraining prompts and budgets; custom role for ad-hoc tasks
- `cloneWorkerRecord`: defensive deep copy ensures callers cannot mutate stored state

## System Role
The worker store is consumed by the daemon orchestrator when spawning multi-agent task graphs. The orchestrator reads active worker counts to enforce concurrency limits, updates status on completion, and uses the role configuration to initialise each worker's ReAct FSM via `internal/runtime/exec`. The status dashboard reads the store to display in-flight workers.

## Dependencies
- `encoding/json`: JSON array persistence
- `os`, `sync`: atomic file writes via `.tmp` + rename; `RWMutex` for concurrent access
- `github.com/google/uuid`: WorkerID generation
- `time`: spawn/finish/timeout timestamps
