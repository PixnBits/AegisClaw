# AegisClaw Changelog

## Unreleased — Phases 3, 4, 5

### Phase 5: Eval Harness
- **`internal/eval`**: New `eval` package with `NewRunner`, `RunAll`, `RunScenario`, `EvalReport`.
- Three built-in synthetic scenarios: `background_research`, `oss_issue_to_pr`, `recurring_summary`.
- Each scenario exercises the full Phase 1-3 stack against a configurable `DaemonProbe`.
- **CLI**: `aegisclaw eval run [--scenario X]` and `aegisclaw eval report`.

### Phase 4: Web Dashboard
- **`internal/dashboard`**: Go HTTP server with HTMX-free, pure Go template pages.
- Pages: Agents (workers), Async Hub (timers/signals), Memory Vault, Approvals, Audit Explorer, Settings.
- SSE endpoint `/events` streams live updates (active workers, pending approvals) every 5 s.
- Approvals page supports in-browser approve/reject via POST form.
- `dashboard.APIClient` interface + `daemonAPIClient` for in-process dispatch (no socket round-trip).
- `api.Server.CallDirect` allows same-process handler dispatch.
- Config: `dashboard.enabled` (default `false`), `dashboard.addr` (default `127.0.0.1:7878`).
- Daemon: `startDashboard()` in `dashboard_daemon.go` starts the server if enabled.

### Phase 3: Hierarchical Agents & Worker Spawning
- **`internal/worker`**: `WorkerRecord` store (JSON persistence), `WorkerStatus`, `Role` types.
  Worker roles: `researcher`, `coder`, `summarizer`, `custom` — each with specialized prompts,
  max tool-call limits, and default timeouts.
- **Kernel action types**: `worker.spawn`, `worker.complete`, `worker.timeout`, `worker.destroy`
  — all Merkle-audited.
- **Tools**: `spawn_worker` (synchronous ReAct loop in ephemeral VM, returns structured result)
  and `worker_status` (inspect records) added to the daemon `ToolRegistry`.
- **Worker VM lifecycle**: create → start → LLM proxy → restricted tool registry → ReAct loop →
  destroy. Always ephemeral; VM deleted on completion or timeout.
- **Worker tool restriction**: `buildWorkerToolRegistry` grants only the Orchestrator-specified
  tool allow-list (default: memory + search tools). Workers cannot spawn sub-workers.
- **Orchestrator prompt**: DELEGATION RULES section added — when/how to delegate, role selection,
  tool grant hygiene, result synthesis.
- **Config**: `worker.dir`, `worker.max_concurrent` (default 4), `worker.default_timeout_mins`
  (default 20), `worker.rootfs_path`.
- **CLI**: `aegisclaw worker list [--active]` and `aegisclaw worker status <id>`.
- **API handlers**: `worker.list`, `worker.status` registered on the daemon API server.
- **Memory integration**: worker completion auto-stores a summary entry in the memory vault
  with tags `["worker", role, task_id]`.

## Phase 2 — Event Bus, Timers, Signals, Approvals

*(See previous commits.)*

## Phase 1 — Memory Store

*(See previous commits.)*

## Phase 0 — Structured Output, ToolRegistry, Snapshots

*(See previous commits.)*
