# AegisClaw — Component Interaction Model

**Status**: North-star architecture document. Code must converge to this; deviations are tracked in `docs/prd-deviations.md`.  
**Last updated**: 2026-03-27

---

## 1. Guiding principle

Every component boundary is a security boundary. The rule that determines whether a component is sandboxed is simple: **if it ever touches untrusted input (user text, LLM output, external network data, or generated code), it runs in a Firecracker microVM. No exceptions.**

The daemon is the only component that runs on the host as root. It manages microVM lifecycles (create, start, stop, delete) and mediates all inter-component communication via the message bus. It does not do LLM inference, does not parse tool calls, and does not execute business logic that belongs to the agent.

---

## 2. Component map

```
┌────────────────────────────────────────────────────────────────────────┐
│  Host (root)                                                           │
│                                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │  Daemon (aegisclaw start)                                        │  │
│  │                                                                  │  │
│  │  • Firecracker VM lifecycle (create/start/stop/delete)           │  │
│  │  • Unix socket API for CLI                                       │  │
│  │  • Message bus (ipc.MessageHub + ipc.Router)                     │  │
│  │  • Tool registry (maps tool names → handler funcs)               │  │
│  │  • Proposal store, audit log, composition store                  │  │
│  │  • Slash command dispatch (/status, /audit, /safe-mode…)         │  │
│  │                                                                  │  │
│  │  Does NOT: call Ollama, parse tool-call blocks, run ReAct loops  │  │
│  └──────────┬───────────────────────────────────────────────────────┘  │
│             │ vsock                                                    │
│   ┌─────────┼───────────────────────────────────────────────────────┐  │
│   │  microVMs (Firecracker, each with read-only rootfs, cap-drop)   │  │
│   │                                                                 │  │
│   │  Agent VM          Court VMs (×5)   Builder VM   Skill VMs      │  │
│   │  (guest-agent)     (guest-agent)    (guest-agent) (guest-agent) │  │
│   └─────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘

     ┌──────────────┐
     │  CLI process │  (unprivileged, thin TUI client)
     │  aegisclaw   │
     │  chat/skill  │
     └──────┬───────┘
            │ Unix socket /run/aegisclaw.sock
            ▼
          Daemon
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

| Sender role | Permitted `tool.*` targets | Denied targets |
|---|---|---|
| **Agent VM** (`role: agent`) | `proposal.*`, `list_proposals`, `list_sandboxes`, any registered skill tool (`<skillname>.*`) | `kernel.*`, `sandbox.*`, `court.*`, `composition.*`, `safe-mode.*` |
| **CLI** (`role: cli`, single-user mode) | All registered tools, all `proposal.*`, `skill.*`, `composition.*`, `safe-mode.*` | `kernel.shutdown` requires explicit `--force` flag in audit log |
| **Court reviewer VM** (`role: court`) | None — court VMs only respond to `review.execute` requests from the daemon; they do not initiate `tool.exec` calls | All |
| **Builder VM** (`role: builder`) | `file.read`, `file.write`, `file.list` within `/workspace` only | All other tools |
| **Skill VM** (`role: skill`) | Only the tools explicitly declared in its approved `SandboxSpec.AllowedTools` list | All others |

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

## 9. Agent VM provisioning

### 9.1 Rootfs

The agent VM uses the standard rootfs built by `scripts/build-rootfs.sh` with the `guest-agent` binary embedded. No special agent rootfs is needed — the guest-agent binary already handles `chat.message` and `tool.exec` dispatch.

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
| `DirectLauncher` or process-based fallback | Bypasses the security boundary we are testing |
| Returning a canned `chat.message` response | Doesn't test that the agent's ReAct loop works |
| Asserting on exact LLM response text | LLM output is non-deterministic; assert on side effects |
| Mocking `proposal.create_draft` | Defeats the purpose; we need to know the real handler was called |
| `t.Skip` if Firecracker is unavailable (except on non-Linux) | These tests exist specifically to run on the development machine with KVM |

---

## 11. What must change in the code (implementation checklist)

This section lists the specific code changes implied by this architecture. Each item corresponds to a deviation entry in `docs/prd-deviations.md`.

### D2-a: Move ReAct loop into agent VM

File: `cmd/guest-agent/main.go`, function `handleChatMessage`

Current behavior: one Ollama call, return raw content.

Required behavior:
1. Loop up to 10 times:
   a. Call Ollama with current message list.
   b. Parse response for `tool-call` block (same regex/JSON logic as `parseToolCalls` in `cmd/aegisclaw/chat.go`).
   c. If tool-call found: send `tool.exec` via vsock, receive `tool.result`, append to messages, continue.
   d. If no tool-call: return final content.
2. Return `{"role": "assistant", "content": "<final>"}`.

The guest-agent can send vsock messages back to the host using the existing vsock connection (the connection is bidirectional — the host sends requests and the guest sends responses, but the guest can also initiate `ipc.send` messages on the same or a separate vsock channel).

### D2-b: Daemon chat.message handler — forward only

File: `cmd/aegisclaw/chat_handlers.go`, function `makeChatMessageHandler`

Current behavior: calls Ollama directly, returns raw LLM content.

Required behavior:
1. Look up or start the agent VM.
2. Send `{"type": "chat.message", "payload": {"messages": [...], "model": "..."}}` to agent VM over vsock.
3. Wait for response (with 10-minute timeout).
4. Return the agent VM's response to the CLI.

Remove: all Ollama client code, system prompt construction, and tool-call parsing from `makeChatMessageHandler`.

### D2-c: Remove DirectLauncher from production path

File: `cmd/aegisclaw/court_init.go`

`DirectLauncher` must not be used in any production code path. It may be retained behind a build tag `//go:build dev_direct` with a compile-time warning, but the default build must use `FirecrackerLauncher` only. The `AEGISCLAW_DIRECT_REVIEW` environment variable override must be removed.

### DA-new: IPC ACL enforcement

File: `internal/ipc/hub.go`

Add `ACLPolicy` type and `Check(role, msgType, toolName)` method. Wire into `RouteMessage` after identity verification. Load ACL at daemon startup from a compiled-in default policy (not a config file — the ACL is a security invariant, not a user preference).

### DB-new: Tool registry in daemon

File: `internal/ipc/` (new file `tool_registry.go`) or `cmd/aegisclaw/`

Register all tool handlers at daemon startup. The message bus dispatches `tool.exec` messages to the registry.

### DC-new: Agent VM startup in daemon

File: `cmd/aegisclaw/start.go` or `cmd/aegisclaw/chat_handlers.go`

Lazy-start the agent VM on first `chat.message`. Register with message bus. Track VM ID in `runtimeEnv`.

---

## 12. Non-negotiable constraints (do not regress)

These rules take precedence over any convenience, testing, or performance argument:

1. **The daemon never calls Ollama.** LLM inference happens only inside microVMs.
2. **The daemon never parses tool-call blocks.** The agent VM owns the ReAct loop.
3. **Firecracker is mandatory for all sandboxed components.** There are no process-level fallbacks in production code paths.
4. **ACL is enforced at the message bus.** No tool handler is callable without passing the ACL check.
5. **Integration tests use real microVMs and real Ollama.** No process-level substitutes or mocked LLM responses.
6. **Secrets never appear in LLM context, logs, or generated code.** Enforced at the proxy; not the agent's responsibility to sanitize.
