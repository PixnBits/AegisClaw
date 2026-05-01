# Package: cmd/aegisclaw

## Overview
`cmd/aegisclaw` is the primary binary for the AegisClaw platform. It combines a multi-command CLI (built with Cobra) and a long-running daemon. When invoked as `aegisclaw start` it becomes the daemon: it provisions Firecracker assets, launches the AegisHub system VM, wires up all subsystems, and serves an HTTP/Unix-socket API. All other subcommands are thin CLI clients that communicate with the running daemon.

The daemon owns the ReAct agent loop, the Governance Court SDLC, the vault, the event bus, worker VM management, and the optional web dashboard portal.

## Architecture

```
aegisclaw start  →  runtimeEnv (runtime.go)
                      ├─ FirecrackerRuntime (sandbox)
                      ├─ AegisHub VM         (IPC router)
                      ├─ SkillRegistry       (skill VMs)
                      ├─ ProposalStore       (SDLC)
                      ├─ CourtEngine         (governance)
                      ├─ Vault               (secrets)
                      ├─ MemoryStore         (agent memory)
                      ├─ EventBus            (timers/signals)
                      ├─ WorkerStore         (background tasks)
                      ├─ LookupStore         (tool discovery)
                      ├─ GitManager          (repo operations)
                      └─ api.Server          (HTTP + Unix socket)
                              ├─ chat_handlers.go    (ReAct loop)
                              ├─ dashboard_handlers.go
                              ├─ vault_handlers.go
                              ├─ memory_handlers.go
                              ├─ worker_handlers.go
                              └─ … (all *_handlers.go)
```

## Files

| File | Description |
|------|-------------|
| `main.go` | Binary entry point; calls `Execute()` |
| `root.go` | Cobra command tree; registers all subcommands and global flags |
| `globals.go` | Package-level booleans for `--json`, `--verbose`, `--dry-run`, `--force` |
| `runtime.go` | `runtimeEnv` aggregate and `initRuntime()` singleton factory |
| `start.go` | `aegisclaw start` — daemon boot sequence |
| `stop_cmd.go` | `aegisclaw stop` — sends shutdown to running daemon |
| `status.go` | `aegisclaw status` — daemon health summary |
| `chat.go` | `aegisclaw chat` — interactive TUI REPL |
| `chat_handlers.go` | Daemon `POST /chat/message` handler; runs ReAct loop |
| `session_handlers.go` | Daemon handlers for `sessions.list/history/send/spawn` |
| `handlers_git.go` | Daemon handlers for `git.browse/log/diff/commit` |
| `composition_handlers.go` | Daemon handlers for `composition.current/rollback` |
| `lookup_handlers.go` | Daemon handlers for `lookup.search/list` |
| `dashboard_handlers.go` | Daemon handlers for dashboard skills/proposals/templates view |
| `eventbus_handlers.go` | Daemon handlers for approvals, timers, signal dispatch |
| `vault_handlers.go` | Daemon handlers for `vault.secret.*` |
| `system_handlers.go` | Daemon handler for `system.stats` (from `/proc`) |
| `worker_handlers.go` | Daemon handlers for `worker.list/status` |
| `memory_handlers.go` | Daemon handlers for `memory.list/search/compact/delete` |
| `audit_log.go` | `aegisclaw audit log/why/trace` subcommands |
| `audit_verify.go` | `aegisclaw audit verify` — offline chain verification |
| `init_cmd.go` | `aegisclaw init` — workspace and kernel initialisation |
| `onboard_cmd.go` | `aegisclaw onboard` — 5-step first-time setup wizard |
| `skill_cmd.go` | `aegisclaw skill add/list/revoke/info/sbom/activate` |
| `secrets_cmd.go` | `aegisclaw secrets add/list/rotate/refresh` |
| `memory_cmd.go` | `aegisclaw memory retrieve/list/compact/delete` |
| `version.go` | `aegisclaw version` — build metadata |
| `self_cmd.go` | `aegisclaw self propose/status/diagnose` |
| `worker_cmd.go` | `aegisclaw worker list/status` |
| `eval_cmd.go` | `aegisclaw eval run/report` |
| `event_cmd.go` | `aegisclaw event timers/signals/approvals` |
| `doctor_cmd.go` | `aegisclaw doctor` — standalone installation health check |
| `gateway_daemon.go` | Optional Gateway daemon (webhook → chat.message) |
| `dashboard_daemon.go` | Optional Portal VM daemon + vsock bridges |
| `eventbus_daemon.go` | Background timer polling; spawns worker VMs on fire |
| `memory_daemon.go` | Background 24h memory compaction daemon |
| `trace_context.go` | Context helpers for ReAct trace ID propagation |
| `tool_registry.go` | `ToolRegistry` — central tool dispatch table |
| `tool_events.go` | `ToolEventBuffer` ring buffer (max 400 entries) |
| `thought_events.go` | `ThoughtEventBuffer` ring buffer (max 600 entries) |
| `helpers.go` | `truncateStr`, `boolYesNo` formatting utilities |
| `sbom_helpers.go` | `readSBOMFile` wrapper |
| `default_bootstrap.go` | Ensures `default-script-runner` skill is active at startup |
| `court_init.go` | `initCourtEngine` — loads Governance Court personas |
| `worker_spawn.go` | `spawnWorker` — ephemeral worker VM creation |
| `script_tools.go` | `run_script` and `list_script_languages` built-in tools |
| `worker_helpers.go` | `formatWorkerRecord` human-readable formatting |

## Test Files

| File | Description |
|------|-------------|
| `chat_handlers_synthesize_test.go` | Unit tests for `synthesizeEmptyFinalMessage` |
| `chat_integration_test.go` | Integration tests with real proposal store (no KVM) |
| `chat_message_live_test.go` | Live tests requiring KVM + root (skipped in standard CI) |
| `eventbus_daemon_test.go` | Unit tests for event-bus daemon and memory store integration |
| `golden_test.go` | Golden-file trace harness for multi-step scenario assertions |
| `inprocess_integration_test.go` | In-process agent loop tests (build tag: `inprocesstest`) |
| `fuzz_test.go` | Fuzz targets for tool parsing and ReAct termination |
| `react_journey_test.go` | 12-scenario table-driven journey tests (no KVM) |
| `lookup_journey_test.go` | 11-scenario journey tests for tool-lookup feature |
| `first_skill_tutorial_test.go` | End-to-end SDLC journey: create→review→approve skill |
| `first_skill_tutorial_inprocess_test.go` | Same journey, in-process mode (build tag: `inprocesstest`) |
| `first_skill_tutorial_live_test.go` | Same journey, live KVM mode (build tag: `livetest`) |
| `secrets_injection_test.go` | Secret injection security contract tests |
| `skill_network_policy_test.go` | Network capability enforcement tests |
| `portal_contract_test.go` | JSON shape contract tests for portal dashboard events |
| `vault_handlers_test.go` | Vault handler unit tests with real vault + kernel |
| `script_tools_test.go` | `parseRunScriptParams` validation unit tests |
