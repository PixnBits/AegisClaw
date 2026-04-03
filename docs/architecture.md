# AegisClaw — Component Interaction Model

**Status**: North-star architecture document. Code must converge to this; deviations are tracked in `docs/prd-deviations.md`.  
**Last updated**: 2026-04-03

**Related documents** (Agentic Evolution):
- `docs/agentic-evolution.md` — Hierarchical multi-agent vision, Orchestrator + Worker model, memory, and async
- `docs/agent-prompts.md` — Orchestrator and Worker LLM prompt specifications
- `docs/event-bus-and-async.md` — Event Bus, timers, and cryptographic signal delivery
- `docs/memory-store.md` — Tiered persistent memory system (working → episodic → semantic)
- `docs/web-dashboard.md` — Local control-plane web UI
- `docs/implementation-plan.md` — Phased delivery roadmap (Phase 0–5)

---

## 1. Guiding principle

Every component boundary is a security boundary. The rule that determines whether a component is sandboxed is simple: **if it ever touches untrusted input (user text, LLM output, external network data, or generated code), it runs in a Firecracker microVM. No exceptions.**

This rule admits no opt-out mechanism of any kind. There are no environment variables, build tags, configuration flags, or runtime modes that permit a sandboxed component to run on the host. KVM and Firecracker are hard dependencies — the daemon refuses to start without them. Any code path that allows bypassing microVM isolation is a security defect and must be removed.

The only components that run directly on the host are:
- **The daemon** (`aegisclaw start`) — root process that manages VM lifecycles, launches AegisHub first, proxies CLI commands, and appends to the audit log. It does **not** own the routing plane; all IPC routing decisions belong to AegisHub. It does not do LLM inference, does not parse tool calls, and does not execute business logic that belongs to the agent.
- **The CLI** (`aegisclaw chat`, `aegisclaw skill`, etc.) — unprivileged thin client that communicates with the daemon over the Unix socket. It does not do LLM inference and does not execute tool handlers.

Every other component — **AegisHub**, the main agent, Governance Court reviewers, the builder, and all skills — runs inside a Firecracker microVM with a read-only rootfs and `cap-drop ALL`.

---

## 2. Component map

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Host (root)                                                               │
│                                                                            │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Daemon (aegisclaw start)  ← MINIMAL TCB after bootstrap             │  │
│  │                                                                      │  │
│  │  • Firecracker VM lifecycle (create/start/stop/delete)               │  │
│  │  • Unix socket API for CLI (slash command passthrough to AegisHub)   │  │
│  │  • Proposal store, audit log, composition store                      │  │
│  │  • Launches AegisHub FIRST; fatal error if AegisHub is unavailable   │  │
│  │                                                                      │  │
│  │  Does NOT: route IPC, enforce ACL, own the tool registry,            │  │
│  │            call Ollama, parse tool-call blocks, run ReAct loops      │  │
│  └──────────┬────────────────────────────────────────────────────────┬──┘  │
│             │ vsock (VMM ops only)         vsock (all IPC)           │     │
│   ┌─────────┼──────────────────────────────────────────────────────┐ │     │
│   │  microVMs (Firecracker, each with read-only rootfs, cap-drop)  │ │     │
│   │                                                                │ │     │
│   │  ┌─────────────────────────────────────────────────────────┐  │ │     │
│   │  │  AegisHub VM  (cmd/aegishub)   ← sole IPC router        │  │◄┘     │
│   │  │  • MessageHub + Router + ACL + IdentityRegistry         │  │       │
│   │  │  • Enforces ACL/policy before every message delivery    │  │       │
│   │  │  • Writes routing events to Merkle audit log            │  │       │
│   │  │  • No network egress — vsock only                       │  │       │
│   │  │  • aegishub-rootfs.ext4 built by build-rootfs.sh        │  │       │
│   │  └─────────────────────────────────────────────────────────┘  │       │
│   │                            │ vsock (all inter-VM traffic)      │       │
│   │  Agent VM    Court VMs(×5)  Builder VM    Skill VMs            │       │
│   │  (guest-agent) (guest-agent) (guest-agent) (guest-agent)       │       │
│   └────────────────────────────────────────────────────────────────┘       │
└────────────────────────────────────────────────────────────────────────────┘

     ┌──────────────┐
     │  CLI process │  (unprivileged, thin TUI client)
     │  aegisclaw   │
     │  chat/skill  │
     └──────┬───────┘
            │ Unix socket /run/aegisclaw.sock
            ▼
         Daemon  →  (proxies control requests)  →  AegisHub VM
```

---

## 3. Natural language chat — correct message flow

This is the primary interaction path and the one most likely to be implemented incorrectly. The daemon **forwards** to the agent; it does not process the message itself.

```
1. User types "please add a skill that says hello..."
   in the CLI TUI (aegisclaw chat).

2. CLI → daemon: POST /api  {"action": "chat.message", "data": {"input": "...", "history": [...]}}
   (over Unix socket /run/aegisclaw.sock)

3. Daemon → Agent VM: vsock send  {"type": "chat.message", "payload": {"messages": [...], "model": "..."}}
   The daemon forwards the full conversation; it does not interpret it.

4. Agent VM executes the ReAct loop (inside the microVM):

   loop:
     a. Agent calls Ollama at 127.0.0.1:11434  (allowed by sandbox network policy)
        with the current message list.

     b. Parse LLM response for a tool-call block:
        ```tool-call
        {"skill": "proposal", "tool": "create_draft", "args": {...}}
        ```

     c. If NO tool-call block found:
        → Return final LLM content to daemon.
        → Loop exits.

     d. If tool-call block found:
        → Send to message bus via vsock:
          {"type": "tool.exec", "from": "<agent-vm-id>", "to": "message-hub",
           "payload": {"tool": "proposal.create_draft", "args": {...}}}

     e. Receive from message bus:
          {"type": "tool.result",
           "payload": {"tool": "proposal.create_draft", "result": "...", "error": ""}}

     f. Append to conversation:
          {"role": "tool", "name": "proposal.create_draft", "content": "<result>"}

     g. Continue loop (step a).

5. Daemon receives final LLM response from Agent VM.

6. Daemon → CLI: API response  {"success": true, "data": {"role": "assistant", "content": "..."}}

7. CLI TUI renders the message.
```

**What the daemon's `chat.message` handler must do (and only do):**
- Ensure the agent VM is running; start one if not.
- Send the full conversation payload to the agent VM over vsock.
- Wait for the agent VM's final response.
- Return it to the CLI.

**What the daemon's `chat.message` handler must NOT do:**
- Call Ollama.
- Parse tool-call blocks.
- Call `handleProposalCreateDraft` or any other tool handler.
- Run a ReAct loop.

---

## 4. Slash command dispatch

Slash commands (`/status`, `/audit`, `/safe-mode`, `/help`, `/quit`) are **not** forwarded to the agent VM. They are handled directly by the daemon because they are operational commands about the system, not natural language requests.

```
CLI → daemon: {"action": "chat.slash", "data": {"command": "/status"}}
Daemon: executes handler, returns structured response.
CLI: renders response.
```

The agent VM is never involved in slash command handling.

---

## 5. Message bus ACL policy

The `ipc.MessageHub.RouteMessage` method must enforce an ACL before delivering any `tool.exec` message. The ACL is checked against the authenticated sender identity (from the vsock connection, not from the message envelope — to prevent spoofing).

### 5.1 ACL table

| Sender role | Permitted `type.*` targets | Denied targets |
|---|---|---|
| **Agent VM** (`role: agent`) | `tool.exec`, `chat.message`, `status` | All others |
| **CLI** (`role: cli`, single-user mode) | All (wildcard) | — |
| **Court reviewer VM** (`role: court`) | `review.result`, `status` | All others |
| **Builder VM** (`role: builder`) | `build.result`, `status` | All others |
| **Skill VM** (`role: skill`) | `tool.result`, `status` | All others |
| **AegisHub VM** (`role: hub`) | All (wildcard) | — |
| **Daemon tool endpoint** (`role: daemon`) | `tool.result`, `status` | All others |

The `hub` wildcard is necessary because AegisHub acts as the routing authority — it must be able to forward any permitted message to its destination. This does not bypass ACL; AegisHub itself enforces the ACL before delivering. The daemon validates that only the AegisHub VM may be assigned `RoleHub`.

### 5.2 Policy enforcement in code

The ACL check belongs in `internal/ipc/hub.go` `RouteMessage`, after identity verification and before handler lookup:

```go
// Pseudocode
func (h *MessageHub) RouteMessage(senderVMID string, msg *Message) (*DeliveryResult, error) {
    role := h.identityRegistry.RoleOf(senderVMID)
    if err := h.acl.Check(role, msg.Type, toolNameFromPayload(msg)); err != nil {
        return nil, fmt.Errorf("ACL denied: %w", err)
    }
    // ... existing routing logic
}
```

The `ACLPolicy` type is a table keyed by `(role, toolPrefix)` → `allow|deny`. It must be immutable at runtime and loaded at daemon startup. Changes to the ACL require a daemon restart (which requires a Court-reviewed proposal for production environments).

---

## 6. Tool registry

The daemon maintains a **tool registry** that maps qualified tool names to handler functions. This is separate from the skill registry (`sandbox.SkillRegistry`, which tracks active microVMs).

```
Tool name                Handler location
─────────────────────── ─────────────────────────────────────────
proposal.create_draft    handleProposalCreateDraft (cmd/aegisclaw/chat.go)
proposal.update_draft    handleProposalUpdateDraft
proposal.get_draft       handleProposalGetDraft
proposal.list_drafts     handleProposalListDrafts
proposal.submit          handleProposalSubmit
proposal.status          handleProposalStatus
list_proposals           (inline in tool handler)
list_sandboxes           (inline in tool handler)
<skillname>.<toolname>   routes to skill VM via skill.invoke API action
```

When the message bus receives `{"type": "tool.exec", "payload": {"tool": "proposal.create_draft", "args": ...}}`, it looks up `proposal.create_draft` in the tool registry and calls the handler directly. The handler runs in the daemon process. The result is returned to the agent VM as `{"type": "tool.result", ...}`.

This is safe because:
- The handler is running in the daemon, which is trusted.
- The tool args come from the agent VM, which is sandboxed and isolated.
- The ACL has already verified the agent VM is permitted to call this tool.
- The args are schema-validated inside the handler before use.

---

## 7. ReAct loop wire protocol

All messages between the agent VM and the message bus use the existing `ipc.Message` envelope.

### tool.exec (agent → hub)

```json
{
  "id": "<uuid>",
  "from": "<agent-vm-id>",
  "to":   "message-hub",
  "type": "tool.exec",
  "timestamp": "...",
  "payload": {
    "tool": "proposal.create_draft",
    "args": {
      "title": "Add time-of-day greeter skill",
      "description": "...",
      "skill_name": "time-of-day-greeter",
      "tools": [{"name": "greet", "description": "..."}],
      "data_sensitivity": 1,
      "network_exposure": 1,
      "privilege_level": 1
    }
  }
}
```

### tool.result (hub → agent)

```json
{
  "id": "<same-uuid>",
  "from": "message-hub",
  "to":   "<agent-vm-id>",
  "type": "tool.result",
  "timestamp": "...",
  "payload": {
    "tool":   "proposal.create_draft",
    "result": "Draft proposal created.\n  ID: 550e8400-e29b-41d4-a716-446655440000\n  ...",
    "error":  ""
  }
}
```

If the tool fails, `"error"` is non-empty and `"result"` is empty. The agent VM appends the error to the conversation as a `tool` role message and continues the ReAct loop, allowing the LLM to respond to the failure.

### Conversation message format (tool results in LLM history)

After receiving a `tool.result`, the agent appends to the in-memory message list:

```json
{"role": "tool", "name": "proposal.create_draft", "content": "<result or error text>"}
```

This follows the standard function-calling convention for chat models. The next LLM call includes this message so the model can incorporate the tool output.

---

## 8. ReAct loop limits and safety bounds

To prevent runaway loops (e.g., from a confused or injected LLM response):

| Bound | Value | Rationale |
|---|---|---|
| Max tool calls per turn | 10 | Prevents infinite loop; a real skill-creation flow needs ≤5 |
| Max ReAct loop depth | 10 | Same ceiling, enforced independently |
| Agent VM LLM timeout | 120 seconds per Ollama call | Prevents hung requests |
| Total `chat.message` timeout | 10 minutes | Enforced by daemon; returns error to CLI on expiry |

If the loop limit is hit, the agent returns the last LLM response with an appended note: `"[system: tool call limit reached — please try again or rephrase your request]"`.

---

## 8.5. Governance Court review flow

The Governance Court is a multi-round, multi-persona review system that evaluates every skill proposal before it can be approved. All reviewer LLM inference runs inside Firecracker microVMs — never on the host.

### 8.5.1 Overview

```
CLI (skill add) → daemon API (court.review) → Court Engine → [rounds 1..3]
                                                                │
               ┌────────────────────────────────────────────────┘
               │
               ▼
         ┌─────────────────────────────┐
         │  For each round:            │
         │  1. Launch reviewer VMs     │  5 personas × 1–2 models each
         │  2. Send review.execute     │  via vsock through LLM proxy
         │  3. Collect reviews         │  verdicts: approve / reject / ask
         │  4. Check consensus         │  weighted quorum (0.8 threshold)
         │  5. If no consensus:        │
         │     → round updater         │  agent VM incorporates feedback
         │     → next round            │
         └─────────────────────────────┘
               │
               ▼
         Consensus reached → approve / reject
         Max rounds (3) hit → escalate (human review required)
```

### 8.5.2 Personas and models

Five security personas review every proposal, each with a distinct focus area:

| Persona | Role | Models |
|---|---|---|
| **CISO** | Security risk assessment | mistral-nemo, llama3.2:3b |
| **SecurityArchitect** | Architecture review | mistral-nemo, llama3.2:3b |
| **SeniorCoder** | Code quality | mistral-nemo, llama3.2:3b |
| **Tester** | Test coverage | llama3.2:3b |
| **UserAdvocate** | Usability | llama3.2:3b |

Persona definitions live in `config/personas/`. Each reviewer VM is a Firecracker microVM with no network (vsock-only access to Ollama via the host-side LLM proxy).

### 8.5.3 Round updater

Between court rounds, the **round updater** (`makeCourtRoundUpdater` in `cmd/aegisclaw/court_init.go`) drives an agent VM to incorporate reviewer feedback into the proposal:

1. Daemon sends aggregated feedback to the agent VM with a focused system prompt instructing it to call `proposal.update_draft`.
2. Agent VM calls Ollama, generates a tool call to update the proposal's description/title.
3. The daemon detects the version advance (`Proposal.BumpVersion()` increments version, hash chain, and timestamp).
4. If the version advanced, the engine proceeds to the next round.
5. If the agent fails to produce a valid tool call after a nudge retry, the proposal is escalated.

**Daemon-side fallback**: Small LLMs sometimes omit the closing fence of a tool-call block or return the tool call inside a `"final"` response. The daemon's `extractToolCallFromContent` function extracts and executes these tool calls directly when the guest-agent classifies the response as `"final"`. This is an interim measure tracked under D2-a; the target architecture has the full ReAct loop inside the agent VM.

### 8.5.4 Consensus

The consensus engine (`internal/court/consensus.go`) uses weighted voting with a 0.8 quorum threshold. Each persona has an equal weight. Verdicts of `approve` count toward the quorum; `reject` counts against; `ask` (questions/concerns) counts as non-approval but not rejection. If the quorum is not met after 3 rounds, the proposal is escalated to `StatusEscalated` for human review via `aegisclaw court vote`.

### 8.5.5 Session persistence

Court sessions are persisted to `~/.local/share/aegisclaw/court-sessions/` as JSON files. On daemon restart, `Engine.ResumeStalled()` finds proposals in `submitted` or `in_review` status that lack an active session and re-queues them with a concurrency limit of 2 simultaneous reviews.

### 8.5.6 Proposal status machine

```
draft → submitted → in_review → approved → implementing → ...
                        │              ↑
                        ▼              │
                    escalated ─────────┘  (human vote can resolve)
                        │
                        ▼
                    rejected / draft  (human vote can reset)
```

The `StatusEscalated` state is entered when the court cannot reach consensus within the maximum number of rounds. Escalated proposals require a human vote (`aegisclaw court vote <id> approve "reason"`) to proceed.

---

## 9. Agent VM provisioning

### 9.1 Rootfs and InitPath

The agent VM uses the standard rootfs built by `scripts/build-rootfs.sh` with the `guest-agent` binary embedded. No special agent rootfs is needed — the guest-agent binary already handles `chat.message` and `tool.exec` dispatch.

Each `SandboxSpec` has an optional `InitPath` field (default: `/sbin/guest-agent`). This allows VMs with different init binaries (e.g., AegisHub uses `/sbin/aegishub`) to share the same Firecracker launch code. The init path is passed as the kernel `init=` boot argument.

Network policy for the agent VM:
- **Allow**: `127.0.0.1:11434` (Ollama on host, port-forwarded by Firecracker)
- **Allow**: vsock to host (for message bus communication)
- **Deny**: all other outbound

### 9.2 Lifetime

The agent VM is started the first time a `chat.message` request arrives at the daemon and is kept running for the lifetime of the daemon process (or until `/shutdown` is issued). It is not started at daemon startup — lazy initialization avoids unnecessary VM overhead when chat is not used.

A future enhancement may allow the agent VM to be restarted on error without full daemon restart.

### 9.3 VM identity

The agent VM registers with the message bus at startup by sending an `ipc.send` message of type `vm.register` with its vsock CID. The daemon assigns it the `role: agent` in the identity registry. This role is used by the ACL.

---

## 10. Integration test strategy

### 10.1 Requirements

Integration tests that exercise the natural language chat path MUST:
- Start a real Firecracker microVM for the agent.
- Use a real Ollama endpoint (not mocked).
- Use a real proposal store (temp directory, not mocked).
- Assert on observable side effects (proposal in store, audit log entries), not on LLM text output.

There are NO process-level fallbacks, no mock VMs, no mock Ollama responses. If the environment cannot satisfy the requirements, the test skips.

### 10.2 Build tag and environment variables

```go
//go:build integration

// Required environment:
//   AEGISCLAW_INTEGRATION=1          — must be set to opt in
//   AEGISCLAW_OLLAMA_ENDPOINT        — e.g. http://127.0.0.1:11434 (default if Ollama is local)
//   AEGISCLAW_OLLAMA_MODEL           — e.g. mistral-nemo
//   KVM accessible at /dev/kvm       — required for Firecracker
```

Skip conditions (call `t.Skip()`, do not fail):
- `AEGISCLAW_INTEGRATION` not set to `"1"`
- `/dev/kvm` not accessible
- Ollama endpoint not reachable (ping before test)

### 10.3 Tutorial integration test — canonical example

File: `cmd/aegisclaw/tutorial_integration_test.go`  
Build tag: `//go:build integration`

**Setup:**
1. Build the guest-agent binary if not present (`go build ./cmd/guest-agent`).
2. Build minimal rootfs embedding guest-agent (use `scripts/build-rootfs.sh` output or a cached path from `AEGISCLAW_TEST_ROOTFS`).
3. Start a real `api.Server` on a temp socket, wired with all production handlers from `runStart`.
4. Start the agent VM via `sandbox.FirecrackerRuntime.Create` + `Start` using the test rootfs.
5. Register the agent VM with the message bus.

**Test steps (mirrors `docs/first-skill-tutorial.md` Step 4 chat path):**

```go
// Send the tutorial's exact natural language request.
resp, err := client.Call(ctx, "chat.message", api.ChatMessageRequest{
    Input: `please add a skill that says hello to the user with a message appropriate
for the time of day ("good morning", "good evening", etc.) respecting DST, in en-US`,
})
```

**Assertions (on side effects, not LLM text):**

```go
// 1. The call succeeded.
require.NoError(t, err)
require.True(t, resp.Success)

// 2. A proposal exists in the store with the correct skill name.
proposals, err := store.List()
require.NoError(t, err)
var found *proposal.Proposal
for _, s := range proposals {
    if s.TargetSkill == "time-of-day-greeter" {
        p, _ := store.Get(s.ID)
        found = p
        break
    }
}
require.NotNil(t, found, "agent must have called proposal.create_draft")

// 3. The proposal has the right structure.
assert.Equal(t, proposal.StatusSubmitted, found.Status)  // agent also called submit
assert.Equal(t, "low", string(found.Risk))
assert.NotEmpty(t, found.Spec)

// 4. The audit log recorded the create and submit actions.
assert.GreaterOrEqual(t, kern.AuditLog().EntryCount(), 2)
```

**Teardown:**
- Stop and delete the agent VM.
- Stop the API server.
- Kernel cleanup.

### 10.4 What is NOT acceptable in integration tests

| Pattern | Why not acceptable |
|---|---|
| Any process-based or host-side launcher | Bypasses the security boundary we are testing |
| Returning a canned `chat.message` response | Doesn't test that the agent's ReAct loop works |
| Asserting on exact LLM response text | LLM output is non-deterministic; assert on side effects |
| Mocking `proposal.create_draft` | Defeats the purpose; we need to know the real handler was called |
| `t.Skip` if Firecracker is unavailable (except on non-Linux) | These tests exist specifically to run on the development machine with KVM |

---

## 11. What must change in the code (implementation checklist)

This section lists the specific code changes implied by this architecture. Each item corresponds to a deviation entry in `docs/prd-deviations.md`.

### D2-a: Move ReAct loop into agent VM — **Open**

File: `cmd/guest-agent/main.go`, function `handleChatMessage`

Current behavior: Guest-agent makes one Ollama call per daemon round-trip, parses tool-call blocks, and returns either `{status:"tool_call"}` or `{status:"final"}`. The daemon drives the outer ReAct loop and executes tool handlers inline.

Required behavior:
1. Loop up to 10 times:
   a. Call Ollama with current message list.
   b. Parse response for `tool-call` block (same regex/JSON logic as `parseToolCalls` in `cmd/aegisclaw/chat.go`).
   c. If tool-call found: send `tool.exec` via vsock, receive `tool.result`, append to messages, continue.
   d. If no tool-call: return final content.
2. Return `{"role": "assistant", "content": "<final>"}`.

The guest-agent can send vsock messages back to the host using the existing vsock connection (the connection is bidirectional — the host sends requests and the guest sends responses, but the guest can also initiate `ipc.send` messages on the same or a separate vsock channel).

### D2-b: Daemon chat.message handler — forward only — **Resolved**

File: `cmd/aegisclaw/chat_handlers.go`, function `makeChatMessageHandler`

`makeChatMessageHandler` calls `ensureAgentVM` and forwards the conversation to the agent VM via `SendToVM`. The daemon no longer calls Ollama for chat. System prompt is built daemon-side and included in the forwarded messages.

### D2-c: Delete DirectLauncher — **Resolved**

File: `internal/court/direct_launcher.go`

`DirectLauncher` has been deleted. `FirecrackerLauncher` is the only supported court launcher. The daemon fails with a fatal error if KVM or the Firecracker binary is unavailable.

### DA-new: IPC ACL enforcement — **Resolved**

File: `internal/ipc/hub.go`, `internal/ipc/acl.go`

`ACLPolicy` type with `Check(role, msgType)` method is implemented and wired into `RouteMessage` after identity verification. Policy is compiled-in (not a config file).

### DB-new: Tool registry in daemon — **Resolved**

File: `cmd/aegisclaw/tool_registry.go`

`ToolRegistry` maps qualified tool names to handler functions. `buildToolRegistry(env)` populates it at daemon startup. Tool dispatch in the chat handler uses `toolRegistry.Execute()`.

### DC-new: Agent VM startup in daemon — **Resolved**

File: `cmd/aegisclaw/chat_handlers.go`

`ensureAgentVM` lazily creates and starts the agent VM on first `chat.message`, starts the per-VM LLM proxy for vsock-based Ollama access, and caches the VM ID. Automatically restarts the VM if it crashes.

---

## 12. Non-negotiable constraints (do not regress)

These rules take precedence over any convenience, testing, or performance argument:

1. **The daemon never calls Ollama.** LLM inference happens only inside microVMs. The daemon forwards conversations to agent/reviewer VMs via vsock; the VMs call Ollama via the host-side LLM proxy.
2. **The daemon never parses tool-call blocks.** The agent VM owns the ReAct loop. *(Interim exception: the court round updater's `extractToolCallFromContent` performs daemon-side tool extraction as a fallback for small LLMs that omit closing fences. This is tracked under D2-a and will be removed when the full ReAct loop moves into the agent VM.)*
3. **Firecracker is mandatory for all sandboxed components.** There are no process-level fallbacks in production code paths.
4. **ACL is enforced at the message bus.** No tool handler is callable without passing the ACL check.
5. **Integration tests use real microVMs and real Ollama.** No process-level substitutes or mocked LLM responses.
6. **Secrets never appear in LLM context, logs, or generated code.** Enforced at the proxy; not the agent's responsibility to sanitize.
7. **AegisHub is the first and last VM in the launch sequence.** No other VM may be started before AegisHub is registered. No message routing decision may bypass AegisHub.

---

## 13. AegisHub microVM — specification

### 13.1 Purpose

AegisHub is the **sole IPC router** for the AegisClaw system. All inter-VM traffic routes through it. No VM may communicate with another VM directly — every message must pass through AegisHub's identity verification and ACL check.

Moving routing out of the root-privileged daemon shrinks the privileged Trusted Computing Base (TCB) to the minimum required for VMM operations. AegisHub benefits from the same hardware-level isolation as every other component: Firecracker + read-only rootfs + `cap-drop ALL` + no shared memory.

### 13.2 Binary and location

- **Binary**: `cmd/aegishub/` (`aegishub`)
- **VM image**: `aegishub-rootfs.ext4` (built with `sudo ./scripts/build-rootfs.sh --target=aegishub`; override path via `AEGISCLAW_HUB_ROOTFS` env var)
- **InitPath**: `/sbin/aegishub` — set in the `SandboxSpec.InitPath` field so the Firecracker kernel boots this binary instead of the default `/sbin/guest-agent`
- **Vsock port**: 1024 (same as `guest-agent`, since only one process listens inside the VM)

### 13.3 Launch sequence

```
1. Daemon starts (host, root).
2. Daemon provisions Firecracker assets (kernel, standard rootfs template).
3. Daemon logs kernel start action.
4. Daemon calls launchAegisHub() — REQUIRED. Fatal error if AegisHub rootfs is missing.
   Build with: sudo ./scripts/build-rootfs.sh --target=aegishub
5. AegisHub VM starts, runs aegishub binary, listens on vsock port 1024.
6. Daemon registers AegisHub VM identity with RoleHub in the MessageHub.
7. Daemon registers AegisHub in the versioned composition manifest.
8. Daemon starts API server, court engine, and all other components.
9. On first chat.message: Daemon lazy-starts Agent VM, registers it with the hub.
10. On shutdown: all VMs stopped before daemon exits.
```

### 13.4 Responsibilities

**AegisHub IS responsible for:**
- Identity verification (checking `From` field matches vsock-verified sender)
- ACL enforcement (role → permitted message types)
- Message routing decisions (find handler for destination VM/ID)
- Audit log entries for every routing event
- VM identity registration and unregistration (routing table is live operational state — see §13.6)
- Hub health/stats reporting (`hub.status`, `hub.routes`)

**AegisHub is NOT responsible for:**
- VM lifecycle management (create/start/stop/delete) — that stays in the daemon
- Tool handler execution — tool handlers (e.g. `proposal.create_draft`) are implemented and executed in the daemon, but **invocations arrive as AegisHub-routed `tool.exec` messages**. The daemon registers itself with AegisHub as a tool-handler endpoint (with a restricted `RoleDaemon` role), so every tool call from an agent VM is ACL-gated by AegisHub before reaching the daemon. Direct daemon calls that bypass AegisHub's routing plane are a security defect. (Current state: daemon registers but full ACL-gated tool dispatch is tracked as D2-a.)
- Secret injection — that stays in the daemon (injected at VM launch time, not routed through the message plane)
- LLM inference — that stays in agent/court/builder VMs

### 13.5 Protocol (daemon ↔ AegisHub)

The daemon communicates with AegisHub over vsock using JSON envelopes:

```json
// Request
{
  "id": "<uuid>",
  "type": "hub.register_vm | hub.unregister_vm | hub.route | hub.status",
  "payload": { ... }
}

// Response
{
  "id": "<same-uuid>",
  "success": true,
  "error": "",
  "data": { ... }
}
```

For `hub.route`, the response `data` contains:
```json
{
  "delivery_result": { "message_id": "...", "success": true, "response": { ... } },
  "deliver_to_vm": "<vm-id-if-forwarding-needed>",
  "forward_message": { ... }
}
```

When `deliver_to_vm` is non-empty, the daemon must forward the message to that VM via vsock.

### 13.6 Governance

AegisHub has two distinct kinds of state with different mutability guarantees:

**Immutable** — the AegisHub binary and rootfs image:
- The running AegisHub microVM cannot modify its own binary or filesystem at runtime (rootfs is read-only, `cap-drop ALL`).
- Replacing the binary or image requires a full Governance Court SDLC cycle:
  1. Proposal created with `proposal.create_draft` (target: `aegishub`)
  2. Court review with all 5 personas (mandatory CISO and Security Architect reviews)
  3. Builder pipeline: SAST + SCA + policy gates + artifact signing
  4. Signed composition manifest update
  5. Daemon restarts AegisHub from the new signed image; rolls back automatically on health failure
- No direct operator modifications to the AegisHub binary or image are permitted outside this process.

**Dynamic operational state** — the routing table:
- AegisHub's in-memory routing table tracks which VM IDs are currently registered and their roles.
- The table changes at runtime as VMs start and stop: the daemon sends `hub.register_vm` when it starts a skill, agent, or court VM, and `hub.unregister_vm` when the VM stops.
- This is expected and intentional — the table must grow as tools, skills, and new agent types are added to the system.
- The ACL policy (which roles may send which message types) is part of the **immutable binary** — it does not change when new VMs register. Adding a new skill only adds a `RoleSkill` routing entry; it does not grant any new ACL permissions beyond those already defined for `RoleSkill`.

---

## 14. STRIDE threat model — AegisHub boundary

This section documents the STRIDE analysis for the AegisHub VM boundary as required by the problem statement. Apply this analysis whenever the AegisHub protocol or VM image changes.

### Threat: **Spoofing** (identity)

| Scenario | Mitigation |
|---|---|
| VM A claims `From: VM-B` in its IPC message, impersonating VM B | The `Router.Route()` method validates `msg.From == senderVMID` (vsock-verified identity). Message is rejected if they differ. |
| Attacker sends messages to AegisHub claiming to be the daemon | Daemon-side requests come over vsock UDS owned by the daemon process; socket permissions (0600) prevent other processes from connecting. |
| AegisHub VM image is replaced with a malicious one | Images must be signed; daemon verifies signature before loading. (Full enforcement: future work via composition manifest signing.) |

### Threat: **Tampering** (integrity)

| Scenario | Mitigation |
|---|---|
| AegisHub in-memory routing table is modified by a compromised VM | AegisHub runs in its own Firecracker VM with read-only rootfs and `cap-drop ALL`. No other process can modify its memory. |
| Message payload is modified in transit between VM and AegisHub | All communication is over vsock (VM → host → AegisHub); no shared memory; Firecracker provides isolation boundaries. |
| AegisHub audit log entries are modified | Merkle-tree audit log provides tamper evidence; entries are signed by the daemon's Ed25519 key. |

### Threat: **Repudiation** (non-repudiation)

| Scenario | Mitigation |
|---|---|
| A VM denies having sent a message | Every `hub.route` call is signed and appended to the Merkle audit log with the vsock-verified sender ID. |
| Routing decisions are not traceable | AegisHub logs `(message_id, from, to, type, sender_vmid, timestamp)` for every routing attempt, including rejections. |

### Threat: **Information Disclosure**

| Scenario | Mitigation |
|---|---|
| Skill VM reads messages intended for the agent VM | No direct VM-to-VM communication; all traffic goes through AegisHub's routing table which enforces destination. |
| AegisHub leaks routing metadata (who talks to whom) | AegisHub runs in an isolated VM; its memory is not accessible to other VMs. Log entries are visible only to the daemon. |
| Secrets injected into a skill VM are visible to AegisHub | Secret injection is a direct vsock call from the daemon to the skill VM; AegisHub is not in this path. |

### Threat: **Denial of Service**

| Scenario | Mitigation |
|---|---|
| Flood of messages overwhelms AegisHub | `maxPayloadLen = 4 MiB` cap per message; connection deadline of 30s; per-VM vsock limits via Firecracker. |
| AegisHub VM crashes, all inter-VM communication stops | Daemon health monitoring detects AegisHub VM exit; restarts it and re-registers all VMs. (Implementation: future work.) |
| Agent VM sends 10,000 `tool.exec` messages rapidly | Rate limiting at the vsock layer (future work). Currently bounded by the daemon's chat handler timeouts. |

### Threat: **Elevation of Privilege**

| Scenario | Mitigation |
|---|---|
| Skill VM tries to call proposal.create_draft directly | ACL denies `tool.exec` from `RoleSkill`; only `tool.result` and `status` are permitted. |
| A compromised VM tries to register itself with `RoleHub` | `IdentityRegistry.Register()` rejects role changes after initial registration; only the daemon can register VMs at startup. |
| AegisHub itself is compromised and starts routing arbitrary messages | AegisHub runs with `cap-drop ALL`; it cannot escape its VM boundary or access the host filesystem. The daemon's audit log would detect abnormal routing patterns. |
| Daemon bypasses AegisHub to deliver messages directly | All vsock communication goes through the ControlPlane which logs every message. Direct delivery without routing would still be audited. |

---
