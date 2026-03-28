# PRD and CLI Deviations Review

Date: 2026-03-26
Updated: 2026-03-27 (alignment refactor applied; D1, D2, D10, D8 resolved)
Updated: 2026-03-28 (D2 re-opened; new deviations D2-a, D2-b, D2-c, DA, DB, DC added after architectural audit)
Updated: 2026-03-28 (DirectLauncher deleted; agent VM wiring complete; D2-b, D2-c, DC resolved)

Scope:
- Compared the implementation in this repository against [docs/PRD.md](docs/PRD.md) and [docs/cli-design.md](docs/cli-design.md).
- Treated code paths wired into the runnable product as authoritative over package-level scaffolding and aspirational docs.
- Used repository code, not README-level intent, to determine what is actually implemented.

Summary:
- The repository has solid building blocks for proposal lifecycle management, Merkle audit logging, Firecracker runtime management, builder orchestration, and encrypted secret storage.
- **Update**: The CLI surface has been aligned with the published specification. Court and builder pipeline wiring has been connected. Security gates and audit coverage have been strengthened.
- **Update (2026-03-27)**: D1 (FirecrackerLauncher), D2 (main-agent sandbox), D10 (versioned compositions with rollback), and D8 (SAST/SCA/policy-as-code) have been resolved.
- **Update (2026-03-28)**: D2 has been re-opened after a detailed architectural audit. `makeChatMessageHandler` in the daemon calls Ollama directly (D2-b). The guest-agent `handleChatMessage` performs one LLM call with no ReAct loop (D2-a). `ExecuteTool` callbacks run in the CLI process (D2-c-cli). Three additional deviations were identified: IPC bus lacks ACL enforcement (DA), no central tool registry in daemon (DB), and no agent-VM startup on first chat message (DC). See `docs/architecture.md` for the correct north-star architecture.
- **Update (2026-03-28)**: `DirectLauncher` (`internal/court/direct_launcher.go`) deleted — there is no opt-out from microVM isolation. `docs/architecture.md` §1 updated to make the no-opt-out rule explicit. D2-b resolved (daemon now forwards to agent VM). DC resolved (lazy agent VM startup via `ensureAgentVM`). D2-c (court DirectLauncher) resolved. D2-a and D2-c-cli remain open.

## Deviation Resolution Status

| ID | Source | Requirement | Status | Notes |
| --- | --- | --- | --- | --- |
| D1 | PRD | Governance Court reviewers must run in isolated microVMs. | **Resolved** | Court initialization uses `FirecrackerLauncher` exclusively. `DirectLauncher` has been deleted — there is no fallback path to host execution. Daemon fails hard if KVM or Firecracker binary is unavailable. Guest agent handles `review.execute` inside sandbox. See `cmd/aegisclaw/court_init.go`. |
| D2 | PRD, CLI | The main agent should be a sandboxed component. | **Partially resolved** | The daemon's `makeChatMessageHandler` now starts the agent microVM lazily (`ensureAgentVM`) and forwards every conversation turn to it; the daemon no longer calls Ollama directly. The outer ReAct loop (tool dispatch) is driven by the daemon pending D2-a completion. CLI `ExecuteTool` callbacks still run on the host (D2-c-cli). See sub-items below. |
| D2-a | architecture.md §3, §7 | Agent VM must run the full ReAct loop: parse tool-call blocks → send `tool.exec` IPC → receive `tool.result` → append to conversation → loop until clean response. | **Open** | `handleChatMessage` in `cmd/guest-agent/main.go` makes one Ollama call per turn and returns either `tool_call` or `final`. The daemon's `makeChatMessageHandler` drives the outer loop (iterate on `tool_call`, execute via `toolRegistry`, append result, re-send to agent VM). Full loop internalization in the agent VM is future work. |
| D2-b | architecture.md §3 | Daemon `chat.message` handler must be a thin forwarder: receive conversation from CLI, route to agent VM via vsock, await final response, return it. | **Resolved** | `makeChatMessageHandler` in `cmd/aegisclaw/chat_handlers.go` now calls `ensureAgentVM` and forwards the conversation to the agent VM via `SendToVM`. Daemon no longer calls Ollama. System prompt is built daemon-side and included in the messages forwarded to the VM. |
| D2-c | architecture.md §11 | `DirectLauncher` must be deleted; no opt-out from microVM isolation. | **Resolved** | `internal/court/direct_launcher.go` deleted. `FirecrackerLauncher` is the only court launcher. `docs/architecture.md` §1 updated to make the no-opt-out rule explicit and unconditional. |
| D2-c-cli | architecture.md §11 | `ExecuteTool` callbacks must not execute proposal handlers in the CLI process for the natural-language path. The CLI is a thin TUI client; tool execution belongs to the agent VM. | **Open** | `handleProposalCreateDraft`, `handleProposalSubmit`, and related functions in `cmd/aegisclaw/chat.go` are called directly by the `ExecuteTool` callback wired into `tui.ChatModel`. Slash commands remain a distinct exception. |
| DA | architecture.md §5 | IPC message bus must enforce an ACL policy before dispatching any tool or message. Sender identity must be validated against an allow-list before the handler is invoked. | **Open** | `MessageHub.RouteMessage` in `internal/ipc/hub.go` has no policy check. Any connected socket or vsock CID can request any registered tool. ACL table and enforcement to be added; see `docs/architecture.md` §5. |
| DB | architecture.md §6 | Daemon must maintain a central tool registry mapping tool names to handler functions, used by the ACL and dispatch layer. | **Open** | Tool dispatch is ad-hoc: each handler in `cmd/aegisclaw/start.go` is registered independently with no shared registry or capability manifest. See `docs/architecture.md` §6 for the required registry shape. |
| DC | architecture.md §9 | Agent VM must be lazy-started on the first `chat.message` request and registered with the message bus before the forwarding call is made. | **Resolved** | `ensureAgentVM` in `cmd/aegisclaw/chat_handlers.go` lazily creates and starts the agent VM on first use, starts the per-VM LLM proxy, and caches the VM ID. Automatically restarts the VM if it crashes. |
| D3 | PRD | Approved skill should trigger builder pipeline automatically. | **Resolved** | Court review handler auto-transitions approved proposals to `implementing` status, connecting to builder pipeline. See `cmd/aegisclaw/start.go`. |
| D4 | PRD | Skill runtime should execute reviewed, versioned artifacts. | **Resolved** | Skill activation resolves artifact manifests from the builder output directory. See `cmd/aegisclaw/start.go`. |
| D5 | PRD, CLI | Secrets must use secure prompt and runtime injection. | **Resolved** | `aegisclaw secrets add` uses secure terminal prompt (no echo). Activation resolves proposal-linked secrets for injection. See `cmd/aegisclaw/secrets_cmd.go`. |
| D6 | PRD, CLI | All actions covered by audit log with `audit log` and `audit why`. | **Resolved** | `audit log` with filters (--since, --skill, --limit) and `audit why` with chain verification. Skill invoke/deactivate audit-logged. See `cmd/aegisclaw/audit_log.go`. |
| D7 | PRD | Court schema validation and consistency checks. | **Improved** | ReviewResponse.Validate() now requires Comments, Evidence for non-abstain verdicts. Full JSON Schema enforcement is future work. See `internal/court/reviewer.go`. |
| D8 | PRD | Security gates (SAST, SCA, policy-as-code). | **Resolved** | Four mandatory security gates implemented in `internal/builder/securitygate/`: SAST (regex-based pattern matching for Go security anti-patterns), SCA (banned dependency and unpinned version detection), secrets scanning (AWS keys, GitHub tokens, private keys, generic patterns), and policy-as-code (no unsafe exec, no host FS, no network unless declared, no privileged ops). Gates are wired as mandatory step 8.5 in the builder pipeline — pipeline fails automatically on blocking findings. See `internal/builder/pipeline.go`. |
| D9 | PRD | Artifact signing, SBOM, provenance. | **Partially resolved** | ArtifactStore with Ed25519 signing and SHA-256 verification exists. SBOM and provenance emission are future work. |
| D10 | PRD, CLI | Versioned compositions with rollback. | **Resolved** | New `internal/composition/` package implements versioned composition manifests (Component, Manifest, Store), persistent JSON versioning, rollback to specific or previous version, health status tracking per component, and automatic rollback on unhealthy components. Daemon API handlers: `composition.current`, `composition.rollback`, `composition.history`, `composition.health`. New kernel audit action: `composition.rollback`. See `internal/composition/manifest.go`, `cmd/aegisclaw/composition_handlers.go`. |
| D11 | PRD, CLI | High-risk approval gates. | **Annotated** | `--force` global flag added for skip-confirmation flows. Typed per-action approval gates are future work. |
| D12 | PRD | Multi-step refinement through main agent. | **Partially resolved** | `skill add` combines wizard + auto-submit. Full sandboxed main-agent refinement is future work. |
| D13 | CLI | CLI surface matches published specification. | **Resolved** | Top-level commands restructured: `init`, `start`, `stop`, `status`, `chat`, `skill`, `audit`, `secrets`, `self`, `version`. Skill subcommands: `add`, `list`, `revoke`, `info`. See `cmd/aegisclaw/root.go`. |
| D14 | CLI | Safe mode with dedicated banner and constrained command set. | **Resolved** | `start --safe` (renamed from `--safe-mode`) with ASCII banner and recovery mode messaging. See `cmd/aegisclaw/start.go`. |
| D15 | CLI | Global flags `--json`, `--verbose`, `--dry-run`, `--force`. | **Resolved** | All four global flags added to root command. `--json` supported in `status`, `version`, `skill list`, `skill info`, `audit log`, `audit why`. See `cmd/aegisclaw/root.go`. |
| D16 | CLI | `version` and `status` report build metadata. | **Resolved** | `version` reports git commit, build date, Go version, OS/arch. `status` reports health, registry root, audit chain head. See `cmd/aegisclaw/version.go`, `cmd/aegisclaw/status.go`. |

## Resolution Summary

### Resolved or substantially improved:
D1, D2-b, D2-c, D3, D4, D5, D6, D8, D10, D13, D14, D15, D16, DC — fully resolved
D2 (partially), D7, D9, D12 — partially resolved / improved

### Annotated with migration path:
D11 — clear path documented, implementation deferred

### Open:
D2-a — agent VM full ReAct loop not yet internalized (outer loop driven by daemon)
D2-c-cli — CLI ExecuteTool callbacks still run tool handlers in CLI process
DA — IPC message bus has no ACL enforcement
DB — no central tool registry in daemon

### Future work required:
D9 (partial) — SBOM and provenance emission

## Observations That Reduce Risk But Do Not Close Gaps

- Proposal lifecycle management is well structured in `internal/proposal/proposal.go`.
- Firecracker runtime support is substantial in `internal/sandbox/firecracker.go`, so several isolation gaps are wiring and guest-protocol gaps rather than complete absence of sandbox machinery.
- The Merkle audit chain is a real signed append-only log in `internal/audit/merkle.go`.
- Builder/runtime abstractions are far enough along that the main missing work is integration into approval, deployment, and runtime activation rather than greenfield subsystem creation.
- The vault and proxy design in `internal/vault/vault.go` and `internal/vault/proxy.go` are directionally consistent with the PRD, but they are not yet enforced by the live invocation path.

## Root Causes (Updated)

1. ~~The live product path still favors host-side fallbacks over PRD-mandated sandbox boundaries.~~ **Substantially resolved**: `DirectLauncher` deleted — no fallback from microVM isolation exists anywhere in the codebase. Court reviewers use `FirecrackerLauncher` exclusively (D1). The daemon forwards chat turns to an agent microVM instead of calling Ollama directly (D2-b). The outer ReAct loop is still driven by the daemon (D2-a open). CLI `ExecuteTool` callbacks still execute on the host for the natural-language path (D2-c-cli open).
2. ~~Proposal, Court, builder, activation, and runtime subsystems were implemented as separate capabilities but not connected into one enforced workflow.~~ **Addressed**: Court approval auto-triggers the builder pipeline. Skill activation resolves artifacts. Versioned composition manifests track every deployment (D10).
3. ~~Supply-chain, policy, and explanation requirements were modeled conceptually but not yet turned into launch-time or operator-facing enforcement.~~ **Substantially addressed**: Mandatory SAST/SCA/secrets/policy gates in the builder pipeline (D8). Versioned composition manifests with automatic rollback on health failures (D10). Structured audit log/why/verify commands (D6).
4. ~~The published CLI design is aspirational relative to the current implementation.~~ **Resolved**: CLI surface matches the published specification (D13–D16).
