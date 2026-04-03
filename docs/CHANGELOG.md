# AegisClaw Changelog

## Unreleased — Phase 6

### Phase 6: Security Hardening, Privacy & Self-Hosting

#### Software Bill of Materials (SBOM)
- **`internal/sbom`**: New package that generates CycloneDX 1.4 JSON SBOMs from builder output.
- `Generate(BuildInfo)` detects Go module dependencies from `go.mod` or import-scan fallback.
- `Write(dir, *SBOM)` serialises to `<sbom_dir>/<proposal_id>/sbom.json`.
- Builder pipeline: Step 9.5 emits `sbom.json` after every successful build (non-fatal on write error).
  `PipelineResult.SBOMPath` records the path.
- `Pipeline.SetSBOMDir(dir)` enables SBOM generation; default dir: `~/.local/share/aegisclaw/sbom`.
- Config: `builder.sbom_dir` (default `~/.local/share/aegisclaw/sbom`).
- Tool: `skill.sbom` — returns the SBOM for a skill by name or proposal ID.
- CLI: `aegisclaw skill sbom <skill-name|proposal-id>` — prints the SBOM to stdout.

#### Memory PII Redaction
- **`internal/memory/pii.go`**: `Scrubber` type with 7 regex rules for common PII patterns:
  email addresses, US phone numbers, SSNs, IPv4 addresses, JWT tokens, AWS access key IDs,
  and generic API keys/passwords.
- `Scrubber.Scrub(text)`, `Scrubber.ScrubEntry(*MemoryEntry)`, `Scrubber.ContainsPII(text)`.
- Hooked into `Store.Store()`: when `PIIRedaction: true`, every entry is scrubbed before encryption.
- Config: `memory.pii_redaction` (default `false`).
- Wired into `runtime.go` via `StoreConfig.PIIRedaction = cfg.Memory.PIIRedaction`.

#### Dashboard: Overview & Skills Pages
- **`/` Overview page**: New home page showing quick-stats cards (active workers, pending approvals,
  active timers, memory entry count) and live tables for active workers and pending approvals.
  No longer redirects to `/agents`.
- **`/skills` Skills & Proposals page**: Lists active skills (from `list_skills`) and all proposals
  (from `list_proposals`) with status badges.
- Navigation updated: `Overview`, `Agents`, `Skills`, `Async Hub`, `Memory`, `Approvals`, `Audit`, `Settings`.
- Settings page expanded to document `memory.pii_redaction` and `worker.default_timeout_mins`.
- Privacy Controls section added to Settings page.

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
