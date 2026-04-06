# AegisClaw: Full Agentic Loop Implementation Plan

**Status:** D2-a open deviation (ReAct loop partially in daemon)  
**Goal:** Complete the agentic loop by moving the entire ReAct cycle into the Agent microVM (`guest-agent`), deeply integrate the new sandboxed script runner as a first-class transient tool, and lay the foundation for hierarchical multi-agent evolution while preserving AegisClaw’s zero-trust, auditable, Firecracker-isolated design.

## Background & Current State

- The ReAct-style agentic loop exists but is **hybrid**: `cmd/guest-agent` makes one Ollama call per round and returns control to the daemon. The daemon drives the outer loop, parses tool calls, and executes handlers.
- **Deviation D2-a**: Move the full loop (multiple LLM calls + tool execution + result appending) entirely inside the Agent VM for stronger isolation.
- The `main` branch now has a hardened, sandboxed script runner with regression tests, live dashboard UX, and integration into tool flows.
- Long-term vision (see `docs/agentic-evolution.md` and `docs/implementation-plan.md`): evolve to a persistent **Orchestrator** + ephemeral **Worker** agents, tiered memory, async event bus, and human-in-the-loop approvals.

**Key invariants to maintain:**
- Every component handling untrusted input (LLM output, tool results, scripts) runs in a Firecracker microVM with read-only rootfs and `cap-drop ALL`.
- All actions are logged to the append-only Merkle-tree audit log.
- New permanent capabilities must go through Governance Court review.
- Transient scripts (inner-loop) use the new sandboxed runner without Court overhead.

## Phase 0: Preparation (1–2 days)

1. Ensure you have the latest commits of `main`.
2. Create this plan file: `docs/plans/agentic-loop-full-implementation.md`.
3. Update `docs/architecture.md` with a new section stub titled “Agentic Loop – Target State (D2-a resolved)”.
4. Run existing tests:
   - Script runner regressions
   - Court review flows
   - Full integration tests (`go test ./...`)
5. Add a new internal package: `internal/agent/loop` (or extend `cmd/guest-agent` directly).

**Success criteria:** All current tests pass; new package skeleton exists.

## Phase 1: Move Full ReAct Loop into Guest-Agent (Core D2-a Resolution) (2–3 days)

**Primary files to modify:**
- `cmd/guest-agent/main.go` (or create `cmd/guest-agent/loop.go`)
- `cmd/aegisclaw/chat_handlers.go` (simplify to single forward + final result handler)
- `cmd/aegisclaw/tool_registry.go` (keep registry on host; tool execution stays daemon-side for now)

**Implementation steps:**

1. In `guest-agent`:
   - Implement a function `RunAgenticLoop(sessionID string, initialMessages []Message) (finalResponse Message, err error)`
   - Inside the loop (max 10–15 iterations, configurable):
     - Assemble context (conversation history + system prompt + available tools description)
     - Call Ollama (structured JSON mode preferred; fallback to fenced parsing)
     - Parse response for either:
       - Final answer → return it
       - Tool call block → send `tool.exec` via vsock to AegisHub → wait for `tool.result` → append as `tool` role message → continue
   - Add safeguards: iteration limit, token usage guard, per-call timeout (120s), total timeout (10 min).
   - Stream intermediate thoughts/tool traces via vsock events for dashboard/TUI visibility.

2. Update daemon:
   - Change chat handler to launch (or reuse) the Agent VM and call the new `RunAgenticLoop` once.
   - Remove outer loop logic and tool-parsing fallback from daemon.
   - Keep tool execution handlers in daemon (via `tool_registry.go`).

3. Update vsock message types if needed for streaming partial results.

**Success criteria:**
- Agent can perform multi-step tool use (e.g., propose → court review simulation → execute) entirely from within the VM.
- D2-a marked resolved in `docs/architecture.md`.
- No behavioral regression in TUI or dashboard chat.

## Phase 2: Integrate Sandboxed Script Runner as Transient Tool (2 days)

**Primary files:**
- `cmd/aegisclaw/tool_registry.go`
- New file: `cmd/aegisclaw/tools/script_runner.go`
- `cmd/guest-agent` (update tool description schema)
- `internal/sandbox/script_runner.go` (existing from branch — expose cleanly)

**Steps:**

1. Register a new core tool: `script.exec` (or `run_sandboxed_script`).
   - Arguments: `language`, `source`, `timeout_seconds`, optional `env` map.
   - Description in tool schema: “Execute arbitrary code/script in an isolated Firecracker sandbox. Use for transient computations, tests, or one-off tasks. Permanent skills must use Governance Court.”

2. Implement handler:
   - Use the existing script runner infrastructure (Firecracker launcher, rootfs from `scripts/build-rootfs.sh`).
   - Capture stdout/stderr, exit code, and logs.
   - Return structured `tool.result` including execution metadata.
   - Emit audit log entry and dashboard event.

3. Distinguish clearly in prompts/documentation:
   - **Transient** → `script.exec` (fast, no Court)
   - **Permanent skill** → `proposal.create_draft` → Court → Builder → deployed Skill VM

**Success criteria:**
- Agent can call `script.exec` inside its loop and receive results.
- Script execution appears in live dashboard tool-call stream.
- All script runs are captured in Merkle audit log.

## Phase 3: Foundation for Hierarchical & Persistent Agency (3–4 days)

**Primary files:**
- `cmd/guest-agent` (Orchestrator prompt + loop extensions)
- New: `internal/agent/orchestrator.go` or extend loop
- `internal/eventbus/` and `internal/memory/` (if not already present)
- `cmd/aegisclaw/sandbox.go` (for worker spawning)

**Steps:**

1. Enhance Orchestrator prompt (add to `config/agent-prompts/` or embed):
   - Role: persistent supervisor
   - Tools: `spawn_worker`, `store_memory`, `set_timer`, `request_human_approval`, `script.exec`, etc.

2. Implement `spawn_worker` tool:
   - Args: task description, role, tools subset, timeout
   - Launches ephemeral Worker microVM from snapshot or cold start
   - Worker runs a simplified ReAct loop
   - Result routed back through Orchestrator

3. Add basic session/memory persistence in guest-agent (load/save via vsock or shared volume if safe).

4. Integrate event bus hooks for async wakeup (initial stub).

**Success criteria:**
- Orchestrator can delegate a subtask to a Worker and incorporate the result.
- Basic memory read/write works across loop iterations.

## Phase 4: Security, Testing, Polish & Documentation (2–3 days)

1. **Security:**
   - Enforce ACLs on all vsock messages.
   - Add human approval gate for destructive actions.
   - Ensure script runner applies all security gates (SAST, PII redaction, etc.).

2. **Testing:**
   - Unit tests for new loop logic.
   - Integration tests: multi-step script + tool sequences.
   - Regression suite for Court flows (must still work).

3. **UX:**
   - Full ReAct trace streaming to Bubbletea TUI and web dashboard.
   - Improve error messages on loop limits/timeouts.

4. **Documentation:**
   - Update `docs/architecture.md` with final loop diagram/flow.
   - Expand `docs/agentic-evolution.md` with progress.
   - Add usage examples for `script.exec` in first-skill tutorial.

**Success criteria:**
- All tests green.
- No new CVEs or security violations introduced.
- Documentation reflects current state.

## Phase 5: Optional Advanced Features (future)

- Hot-reloading of skills inside running loop.
- Multi-model support (different Ollama models per role).
- Full tiered memory with semantic search.
- Periodic self-improvement proposals via Court.

## Reminders for Implementation

- **Paranoid design first:** Never trust LLM output. Always validate schemas, sandbox everything, audit every step.
- Use structured output (JSON mode) where possible; keep fenced parsing as fallback.
- Prefer minimal changes to host daemon — push logic into VMs.
- Every new tool or feature that persists must route through Governance Court unless explicitly transient.
- When in doubt, check `adrs/` and existing security gates in `internal/builder/securitygate/`.

**Next Step:** Start with Phase 0 and Phase 1. Once the core loop is fully inside `guest-agent`, the system will feel significantly more autonomous while remaining auditable and secure.
