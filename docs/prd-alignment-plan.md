# PRD Alignment Plan

Date: 2026-03-26  
Updated: 2026-03-28 (reflects DirectLauncher removal, agent VM wiring, D2-b/D2-c/DC resolution)

Source:
- Derived from [docs/prd-deviations.md](docs/prd-deviations.md).
- Goal is implementation alignment with [docs/PRD.md](docs/PRD.md), not PRD simplification.

## What Is Already Done

The following deviations have been fully resolved and require no further work unless a regression is introduced:

| ID | What was done |
|---|---|
| D1 | `FirecrackerLauncher` is the only court launcher. `DirectLauncher` deleted. Daemon fails hard without KVM/Firecracker. |
| D2-b | Daemon `makeChatMessageHandler` forwards conversation to agent microVM via vsock; no direct Ollama calls from daemon. |
| D2-c | `internal/court/direct_launcher.go` deleted. No opt-out from microVM isolation anywhere in the codebase. |
| D3 | Court approval auto-triggers the builder pipeline. |
| D4 | Skill activation resolves artifact manifests from builder output. |
| D5 | `aegisclaw secrets add` uses secure terminal prompt. Secret injection wired through activation path. |
| D6 | `audit log` (with filters) and `audit why` (with chain verification) implemented. |
| D8 | Mandatory SAST, SCA, secrets scanning, and policy-as-code gates in builder pipeline — no bypass. |
| D10 | Versioned composition manifests with automatic rollback on health failures. |
| D13–D16 | CLI surface matches published specification; global flags; version/status metadata. |
| DC | `ensureAgentVM` lazily creates the agent microVM on first chat message; auto-restarts on crash. |

## Open Work

The following items remain open in priority order. All isolation-critical items come before usability items.

### Priority 1 — Isolation correctness

#### D2-a: Full ReAct loop inside the agent VM

**Source:** `docs/architecture.md` §3, §7  
**Why it matters:** The outer ReAct loop (iterate tool calls, execute via `toolRegistry`, re-send to agent VM) currently runs in the daemon. The daemon still drives the control flow for tool dispatch, which is business logic that belongs inside the sandbox per §1.

**What needs to change:**

1. `cmd/guest-agent/main.go` — `handleChatMessage` must loop internally:
   - Call Ollama via proxy.
   - Parse for a `tool-call` block.
   - If found: send `{"type":"tool.exec","payload":{"tool":"...","args":"..."}}` to daemon via vsock, receive `{"type":"tool.result","payload":{"result":"...","error":""}}`, append to conversation, loop.
   - If not found: return `{"status":"final","role":"assistant","content":"..."}`.
2. `cmd/aegisclaw/chat_handlers.go` — `makeChatMessageHandler` becomes a thin forwarder: send conversation to agent VM once, await the single final response. Remove the outer `for i := 0; i < reactMaxIterations; i++` loop and the `toolRegistry.Execute` call.
3. `internal/ipc/` — Ensure the vsock channel from the agent VM to the daemon can carry `tool.exec`/`tool.result` message pairs (bidirectional protocol; guest initiates).

**Acceptance criteria:**
- `makeChatMessageHandler` sends one message to the agent VM and awaits one response.
- The agent VM returns only `{"status":"final",...}` — no `"tool_call"` status visible to the daemon.
- Tool execution still happens in the daemon (tool handlers are trusted host-side code).

---

#### D2-c-cli: Remove `ExecuteTool` callbacks from the CLI process

**Source:** `docs/architecture.md` §11  
**Why it matters:** `cmd/aegisclaw/chat.go` wires `model.ExecuteTool` to call `handleProposalCreateDraft`, `handleProposalSubmit`, and related handlers directly in the CLI process. The CLI is a thin TUI client; tool execution must not happen there.

**What needs to change:**

1. `cmd/aegisclaw/chat.go` — Remove the `model.ExecuteTool` callback entirely. The natural-language chat path must not call any `handleProposal*` function.
2. The CLI sends user input to the daemon; the daemon forwards to the agent VM; the agent VM issues `tool.exec` IPC; the daemon's `ToolRegistry` executes the handler. Tool results never pass through the CLI process.
3. Slash commands (`/status`, `/audit`, etc.) remain handled by the daemon and are unaffected.

**Acceptance criteria:**
- Deleting `model.ExecuteTool` assignment from `chat.go` does not break the natural-language path.
- `handleProposalCreateDraft` and related functions are only called from `cmd/aegisclaw/tool_registry.go`, not from the CLI.

---

#### DA: IPC message bus ACL enforcement

**Source:** `docs/architecture.md` §5  
**Why it matters:** Any connected vsock CID can request any registered tool. The message bus must enforce an ACL based on the authenticated sender role before dispatching.

**What needs to change:**

1. `internal/ipc/hub.go` — Add `ACLPolicy` type with `Check(senderRole, msgType, toolName) error`. Wire into `RouteMessage` after identity lookup, before handler dispatch.
2. Implement the ACL table from `docs/architecture.md` §5.1 (agent → proposal.*, list_*; court → none; builder → file.* in /workspace; skill → declared tools only).
3. Load the ACL from a compiled-in default at daemon startup — not from a config file.

**Acceptance criteria:**
- A vsock connection claiming `role:skill` cannot call `proposal.create_draft`.
- ACL violations are logged and return an error to the caller.

---

#### DB: Central tool registry in daemon

**Source:** `docs/architecture.md` §6  
**Why it matters:** Tool dispatch is currently ad-hoc (each handler registered independently in `start.go`). The ACL layer (DA above) needs a single registry to consult.

**What needs to change:**

1. `cmd/aegisclaw/tool_registry.go` already exists with the correct shape. Ensure all tool handlers that belong in the daemon are registered there and that `start.go` uses it as the single registration point.
2. The message bus `RouteMessage` dispatches `tool.exec` to the registry instead of ad-hoc handler maps.

**Acceptance criteria:**
- Adding a new tool requires only one registration call in `tool_registry.go`.
- The ACL check references the same registry.

---

### Priority 2 — Supply-chain and governance

#### D9: SBOM and provenance emission

**Source:** PRD  
**Why it matters:** `internal/builder/artifact.go` signs artifacts but does not emit an SBOM or provenance record. Without these, the supply chain is not fully auditable.

**What needs to change:**

1. `internal/builder/artifact.go` — Emit a CycloneDX or SPDX SBOM alongside each built artifact.
2. Include a provenance record (builder VM ID, build timestamp, git commit of generated code, signing key fingerprint).
3. Store SBOM and provenance in the artifact directory; include their hashes in the composition manifest.

**Acceptance criteria:**
- Every activated skill has an SBOM and provenance record on disk.
- `aegisclaw audit log` surfaces the provenance hash.

---

#### D7: Full JSON Schema validation for reviewer outputs

**Source:** PRD  
**Why it matters:** `ReviewResponse.Validate()` checks required fields but does not enforce a versioned JSON Schema. Malformed or adversarially crafted reviewer responses could slip through.

**What needs to change:**

1. Define a versioned JSON Schema for `ReviewResponse` under `docs/schemas/review-response.v1.json`.
2. Validate all reviewer responses against the schema in `internal/court/reviewer.go` before `Validate()` is called.

---

### Priority 3 — Governance and usability

#### D11: High-risk approval gates

**Source:** PRD, CLI  
**`--force` flag exists** but typed per-action approval gates (pause + require explicit human decision for high-risk operations) are not yet implemented. Deferred pending D2-a completion so the agent VM can drive the approval interaction.

#### D12: Full conversational proposal refinement

**Source:** PRD  
**`skill add` wizard + auto-submit exists** but full multi-turn conversational refinement through the sandboxed main agent is pending D2-a completion.

---

## Recommended Execution Sequence

1. **D2-a** — Internalize the ReAct loop in the agent VM. This is the prerequisite for D2-c-cli, D11, and D12.
2. **D2-c-cli** — Remove `ExecuteTool` from the CLI once D2-a is complete.
3. **DA + DB** — Add ACL enforcement and ensure the tool registry is the single dispatch point.
4. **D9** — SBOM and provenance emission.
5. **D7** — JSON Schema validation for reviewer outputs.
6. **D11 + D12** — High-risk approval gates and conversational refinement (after D2-a).

## Files Most Likely To Change

| File | Why |
|---|---|
| `cmd/guest-agent/main.go` | D2-a: internalize full ReAct loop including tool.exec/tool.result exchange |
| `cmd/aegisclaw/chat_handlers.go` | D2-a: simplify to single-call forwarder once agent VM owns loop |
| `cmd/aegisclaw/chat.go` | D2-c-cli: remove `ExecuteTool` callback |
| `cmd/aegisclaw/tool_registry.go` | DB: ensure single registration point for all daemon tools |
| `internal/ipc/hub.go` | DA: add ACL enforcement in `RouteMessage` |
| `internal/builder/artifact.go` | D9: emit SBOM and provenance |
| `internal/court/reviewer.go` | D7: JSON Schema validation |
