# AegisClaw Changelog

All notable changes to AegisClaw are documented here.
Format: date, version (if tagged), description, and a link to the relevant GitHub issue or PR.

---

## [Unreleased] — Issue #6: Extend Agent ReAct loop for long-running, persistent, and event-driven goals

**Date:** 2026-03-31  
**Branch:** `copilot/extend-react-loop-features`

### Summary

Implements the highest-priority changes from Issue #6 across four phases, enabling the agent to
handle long-running, persistent, and event-driven goals while preserving all existing security
invariants (microVM isolation, Merkle-tree audit log, zero-secrets-in-LLM-context, backward
compatibility with single-turn chat).

---

### Phase 1 – Configurable ReAct limits + `tool.continue`

#### `internal/config/config.go`
- **New fields** on `Agent` struct:
  - `MaxToolCalls int` — maximum tool dispatches per chat turn (default: 10)
  - `MaxLoopDepth int` — maximum ReAct loop depth (default: 10)
  - `LLMTimeoutSecs int` — per-Ollama-call deadline inside the agent VM (default: 120 s)
  - `TurnTimeoutMins int` — total wall-clock deadline for one chat turn (default: 10 min)
- `validateConfig()` now enforces minimum floor ≥ 1 on all numeric limits and an absolute-path
  check on `HistoryDir`.  Zero or negative values are rejected at load time to prevent accidental
  disabling of safety ceilings.
- Viper defaults registered so existing `config.yaml` files without the new keys still work.

#### `cmd/guest-agent/main.go`
- `ChatMessagePayload` extended with two optional per-session override fields:
  - `LLMTimeoutSecs int` — the daemon forwards its configured timeout; the guest-agent caps it at
    `ollamaTimeoutMax` (300 s) regardless, protecting against a compromised or misconfigured host.
  - `MaxToolCalls int` — forwarded for informational purposes; the outer loop remains in the daemon.
- Hard-coded `ollamaTimeout` constant renamed to `ollamaTimeoutDefault` (120 s) and
  `ollamaTimeoutMax` (300 s) added.
- `handleChatMessage` now resolves the per-call timeout from the payload with the hard cap applied.

#### `cmd/aegisclaw/chat_handlers.go`
- `agentChatPayload` extended with `LLMTimeoutSecs` and `MaxToolCalls` so the daemon's config
  values are forwarded to the guest-agent on every call.
- `makeChatMessageHandler` reads `LLMTimeoutSecs` from `env.Config.Agent` and passes it down.
- New `handleToolContinue()` function: intercepts a `tool.continue` tool call from the agent VM,
  extracts the LLM-provided `{"summary":"…"}`, compresses the message list to
  `[system + user-with-summary]`, and resets the loop counter — enabling tasks that span the
  tool-call budget without losing context.
- Each chat turn now emits an `agent.turn.start` kernel action to the Merkle audit log (metadata
  only — no raw user content).
- Each `tool.continue` event emits an `agent.tool_continue` kernel action.
- Per-turn `context.WithTimeout(TurnTimeoutMins)` added.
- System prompt updated to document `tool.continue`, `conversation.summarize`, `schedule.create`,
  `webhook.register`, and `monitor.start`.

#### `cmd/aegisclaw/court_init.go`
- Updated reference from deleted `reactMaxIterations` to `reactMaxIterationsDefault`.

---

### Phase 2 – Persistent conversation history

#### `cmd/aegisclaw/conversation_store.go` _(new file)_
- Append-only JSONL store (`ConversationStore`) for conversation history.
- System messages are **never** persisted (they are rebuilt per-session from the system prompt).
- Malformed JSON lines are skipped gracefully to tolerate partial file corruption.
- Store directory created with mode `0700`, file with mode `0600`.
- `LoadHistory()` returns the last N messages (configurable via `agent.history_max_messages`).

#### `cmd/aegisclaw/chat_handlers.go`
- `loadConversationHistory()` and `persistTurn()` wired around each chat turn.
- Non-fatal: failures are logged, not surfaced to the user.
- **Migration note:** This is a daemon-side (host filesystem) interim store. The target
  architecture (`architecture.md §8.1`) places the store inside the agent VM's Firecracker
  boundary. Migration is deferred until D2-a (full ReAct loop in agent VM) is resolved.

#### `internal/config/config.go`
- `Agent.HistoryDir string` — directory for JSONL conversation files (default:
  `~/.local/share/aegisclaw/conversations`).
- `Agent.HistoryMaxMessages int` — maximum past messages loaded at session start (default: 50).

---

### Phase 3 – Event-driven skill stubs

#### `cmd/aegisclaw/tool_registry.go`
- **`conversation.summarize`**: new stub for Phase 2 session-close summarization (PRD §10.6 A2).
  Logs the request to the Merkle audit chain (`agent.conversation.summarize` action).
- **`schedule.create`**: registers a cron-style recurring trigger. Validates args schema
  (`{"cron":"…","goal":"…"}`), logs to Merkle chain (`event.schedule.create`).
- **`webhook.register`**: registers an inbound webhook trigger. Validates args schema
  (`{"path":"…","goal":"…","secret_ref":"…"}`), enforces no path separators in `secret_ref`,
  logs to Merkle chain (`event.webhook.register`).
- **`monitor.start`**: starts polling a resource. Validates args schema
  (`{"target":"…","condition":"…","goal":"…","interval_secs":60}`), warns if target URL
  contains query params (potential secret leakage), logs to Merkle chain (`event.monitor.start`).

All Phase 3 stubs return clear "not yet implemented" errors referencing `docs/prd-deviations.md`
so the agent can inform the user rather than silently failing.

---

### Phase 4 – Architectural enablers

#### `internal/kernel/action.go`
- **New action types** (fully auditable from day 1):
  - `agent.turn.start` — emitted at the start of each chat.message handler invocation
  - `agent.tool_continue` — emitted when the agent compresses history via `tool.continue`
  - `agent.conversation.summarize` — emitted when conversation.summarize is called
  - `event.schedule.create` — emitted when schedule.create is called
  - `event.webhook.register` — emitted when webhook.register is called
  - `event.monitor.start` — emitted when monitor.start is called
- All new types added to `validActionTypes` map for validation.

#### `internal/ipc/acl.go`
- **`RoleOrchestrator VMRole = "orchestrator"`** — permits `event.trigger` + `status`.
  Roadmap: the Orchestrator microVM will inject scheduled/event-driven `chat.message`
  payloads into AegisHub.
- **`RolePlanner VMRole = "planner"`** — permits `tool.exec` + `status`.
  Roadmap: the Planner microVM will decompose high-level goals into ordered sub-proposals.

---

### Tests

#### `cmd/aegisclaw/agent_limits_test.go` _(new file)_
- `TestAgentReActConfigDefaults` — verifies default values match documented baselines.
- `TestHandleToolContinueValid` / `*EmptySummary` / `*BadJSON` / `*NoSystemMsg` — covers the
  full `handleToolContinue` contract.
- `TestReActMaxIterationsDefaultSentinel` — guards against accidental changes to the security
  baseline constant.
- `TestPhase3SkillStubsRegistered` / `*ReturnNotImplemented` — confirms stubs are wired and
  return informative errors.
- `TestSystemPromptMentionsToolContinue` — verifies the system prompt documents `tool.continue`.
- `TestAgentConfigFieldsPresent` / `TestAgentHistoryDirValidation` — structural config tests.

#### `cmd/aegisclaw/conversation_store_test.go` _(new file)_
- Round-trip, maxMessages truncation, zero-max disable, missing file, system-message filtering,
  corruption tolerance (malformed lines skipped), and directory-creation permissions (0700).

---

### Documentation

#### `docs/prd-deviations.md`
- **D17** — Configurable ReAct limits: resolved.
- **D18** — `tool.continue`: resolved.
- **D19** — Persistent conversation history: partially resolved (daemon-side JSONL; migration
  to VM boundary deferred).
- **D20** — Event-driven skill stubs + ACL roles: partially resolved (stubs and ACLs in place;
  Orchestrator and Planner VMs not yet launched).

---

### Governance Court — items still requiring approved proposals

The following capabilities were registered as stubs and **must go through a full
Governance Court review** before they are wired to real implementations:

| Capability | Action required |
|---|---|
| Raise `agent.max_tool_calls` above default 10 | Court-approved daemon config proposal |
| `conversation.summarize` full implementation | Court proposal for skill VM + rootfs |
| `schedule.create` full implementation | Court proposal for Orchestrator VM + cron daemon |
| `webhook.register` full implementation | Court proposal for Network Proxy VM + TLS config |
| `monitor.start` full implementation | Court proposal for Monitor VM + polling policy |
| Orchestrator microVM launch | Court proposal with rootfs, ACL wire-up, and audit coverage |
| Planner microVM launch | Court proposal with rootfs, restricted tool allowlist |
| Migrate conversation store inside VM boundary | Blocked on D2-a; requires agent VM writable-volume proposal |

---

## Prior History

_Detailed history before this feature branch is tracked in git commit messages._
