# Phase 1: Core Runtime (Agent Runtime + Memory VM)

**Status:** Not Started  
**Priority:** P0  
**Estimated Effort:** 3–4 weeks

## Goal
Implement the real Agent Runtime VM with the full 6-step loop and integrate it with a real Memory VM.

## Key Specifications
- `docs/specs/agent-runtime.md`
- `docs/specs/memory-vm.md`
- `docs/prd/runtime-architecture.md`
- `docs/prd/security-model.md`

## Definition of Done
- [ ] Full 6-step loop executes inside a real Firecracker microVM
- [ ] Agent can call skills/tools exclusively through AegisHub (vsock/JSON-RPC)
- [ ] Memory VM provides short-term context + conversation history + ACLs
- [ ] No "surface-only" or "limited mode" disclaimers in the agent execution path
- [ ] All tests in `internal/agent/` and `cmd/agent/` pass with ≥80% coverage
- [ ] End-to-end journey (chat → autonomy grant → background work) works with real runtime

## Detailed Tasks

### 1.1 Agent Runtime Skeleton (Week 1)
- Create `cmd/agent/main.go` with real 6-step loop structure
- Implement `Observe`, `Think`, `Plan`, `Act`, `Execute`, `Judge` steps as separate packages
- Wire vsock client to AegisHub
- Add basic context passing to Memory VM

### 1.2 Memory VM Implementation (Week 1–2)
- Create `cmd/memory/main.go` with real short-term context store
- Implement conversation history + ACL enforcement
- Add vsock interface for Agent Runtime
- Support TTL-based eviction and secure zeroization

### 1.3 Integration & Wiring (Week 2–3)
- Connect Agent Runtime ↔ Memory VM ↔ AegisHub
- Implement skill/tool invocation through Hub only
- Add event subscription for user messages and Court feedback
- Wire proactive/background task handling

### 1.4 Testing & Hardening (Week 3–4)
- Unit tests for each step of the 6-step loop
- Integration tests with real Hub + Memory VM
- Chaos tests for VM crash + recovery
- Update `docs/specs/agent-runtime.md` with implementation notes
- Remove all surface-only code paths in CLI/Portal for agent execution

## Success Criteria
When this phase is complete, an agent should be able to:
- Start from a user message
- Maintain conversation context across turns
- Call real skills through the Hub
- Execute background work with autonomy grants
- Survive VM restarts with memory preserved

**No stubs remaining in the core execution path.**

---

## Autonomous Execution Progress (P0 – docs/lessons-learned branch)

**Session:** 019e6885-3431-7201-99d2-3d49e90085b1 (Grok 4.3, April 2026 model)  
**Started:** 2026-05 (this interaction)  
**Execution Mode:** Fully autonomous per user query + approved plan in `~/.grok/sessions/.../plan.md`. Follows **No-Stubs-Left Resolution Plan** exactly (Phase 1 only).  
**Spec-First Citations (every change + commit):** `docs/specs/agent-runtime.md`, `docs/specs/memory-vm.md`, `docs/prd/runtime-architecture.md`, `docs/prd/security-model.md`, `docs/specs/aegishub.md`, AGENTS.md, `docs/no-stubs-left-resolution-plan.md`, this file.

**Status Update:**  
**Status:** In Progress – Group 0 (Setup) complete. Beginning 1.1 starting tasks (Agent Runtime skeleton + 6 packages + vsock client in Hub + Memory skeleton) on next "continue".

**Group 0 Actions Completed (this commit):**
- Re-read key specs (agent-runtime.md §Overview/Responsibilities/Communication, memory-vm.md, prd docs, acls.yaml, current stubs in cmd/agent:155-236 etc., cmd/memory, aegishub, orchestrator key dist, AGENTS.md).
- Created tracking todo list (one in_progress at a time).
- This progress note added (with full spec refs).
- Verification commands executed: `make test`, `make build-binaries`, `./bin/aegis doctor` (all passed; clean baseline, no regressions).
- Atomic commit created.

**Next (on "continue"):** Group 1.1a – foundational `internal/transport/hubclient` (unix + vsock dial/register per aegishub.md §Handshake Sequence "MicroVM connects to AegisHub via vsock", agent-runtime.md §Communication "Only allowed interfaces: vsock/JSON-RPC", security-model.md §Communication & Mediation + paranoid fail-closed). Then 1.1b (agent 6-step packages + thin main removing all mocks/fallbacks from prod path), 1.1c (hub vsock listener :9999), 1.2 (memory skeleton).

**Definition of Done Progress (will be checked at end of 1.4):**
- [ ] Full 6-step loop executes inside a real Firecracker microVM
- [ ] Agent can call skills/tools exclusively through AegisHub (vsock/JSON-RPC)
- [ ] Memory VM provides short-term context + conversation history + ACLs
- [ ] No "surface-only" or "limited mode" disclaimers in the agent execution path
- [ ] All tests in `internal/agent/` and `cmd/agent/` pass with ≥80% coverage
- [ ] End-to-end journey (chat → autonomy grant → background work) works with real runtime

**Paranoid Security / Rules Followed:** Fail-closed design in all new paths, least-privilege (reuse ACLs + per-VM Ed25519 from orchestrator.go:89-164), zeroization on key load, audit logging via hub, no secret material in agent/memory. Never start/stop daemon except `make start`/`make stop` (AGENTS.md). Verification-first always.

**Commits (atomic, spec-referenced):**
- This one: "docs: Phase 1 Group 0 progress + autonomous execution start (refs no-stubs-left-resolution-plan.md:§2, phase-1.md:1.1, agent-runtime.md:§Responsibilities, aegishub.md:§Handshake Sequence, AGENTS.md)"

**No surface/stub code added.** All future changes will delete surface (mockLLMWithFallback etc. from cmd/agent/main.go:146-153,147) and replace with real per specs.

**Ready for "continue" to Group 1.1a.** (User prompt will trigger next group; this session works completely autonomously per query.)

**Group 1.1a COMPLETE (Transport Hub Client – foundational vsock/unix client)**

- Created `internal/transport/hubclient/types.go` + `client.go` + `client_test.go` (new package).
- Full paranoid Client interface + DialUnix / DialVsock + Register (exact handshake per aegishub.md) + Send (auto-sign + reply) + zeroization on Close + context deadlines + error mapping to all ERR_* sentinels.
- 100% wire-compatible Message + signMessage logic copied from cmd/agent:68 and cmd/aegishub:170.
- Unit tests: hermetic net.Pipe() simulation of full register + Send roundtrip (the exact flow the future 6-step loop will use), early fail-closed on bad key material, pre-register guard, vsock graceful failure, ZeroPrivateKey hygiene.
- All tests pass, new pkg integrated into tree.
- **No changes to hub, agent, or memory binaries yet** (per plan: "prep hub (no change yet)").
- Verification (this group): targeted go test + make build-binaries + ./bin/aegis doctor — all green, no regressions.
- Commit will follow immediately after this note (atomic, spec-referenced).

**Spec citations in the new code + this note + upcoming commit message:**
- aegishub.md §Handshake Sequence (1-4, "via vsock", register with pubkey, receive ACLs + assigned_id)
- agent-runtime.md §Communication ("Only allowed interfaces: vsock / JSON-RPC to AegisHub")
- security-model.md §Communication & Mediation + §Isolation Strategy (fail-closed, least-privilege, every boundary is a boundary)
- no-stubs-plan/phase-1.md 1.1a (transport before any agent runtime changes)
- runtime-architecture.md (Agent + Memory VMs only talk via the router)

**Next on "continue":** Group 1.1b — the real 6-step packages under internal/agent/ + thin cmd/agent/main.go skeleton (remove all mockLLMWithFallback + inline steps from the prod execution path).

**Group 1.1b COMPLETE (Agent Runtime Skeleton + 6 Real Step Packages)**

- Moved the proven 7.3 AgentSkillIndex (Jaccard/Levenshtein search, format helpers, etc.) to `internal/agent/skills/index.go` with full spec citations.
- Created `internal/agent/types.go` (TurnContext, StepResult, LLMCallFunc, MemoryClient).
- Created `internal/agent/loop/loop.go` — RunTurn that:
  - Calls memory.get_context first (real, via hubclient, per memory-vm.md)
  - Executes the 6 steps in order using the injected real LLMCallFunc (no mocks/fallbacks in the hot path)
  - Provides NewRealLLMCaller (the production path: signed "llm.call" via hubclient to network-boundary)
- Created the 6 separate packages (`internal/agent/{observe,think,plan,act,execute,judge}/step.go`) — each a real (if minimal) step that performs LLM reasoning via the hubclient.
- **Thinned cmd/agent/main.go** from ~875 lines of inline surface code + mocks to a small skeleton:
  - Uses hubclient.DialUnix / DialVsock + Register (real distributed key consumption + zeroization)
  - Wires the real loop + NewRealLLMCaller for the 6-step path
  - Preserves useful 7.3/7.6 surface (tool.list, autonomy, background, index updates) via aliases to the new skills package
  - Removed callLLMWithFallback, mockLLMResponse, the 6 old inline funcs, the massive duplicated index, and all "for demo / in full system" disclaimers from the execution path.
- cmd/agent and all new internal/agent packages build and the legacy tests still pass.
- Full verification (this group): `go test ./internal/agent/... ./cmd/agent/...`, `make build-binaries`, `./bin/aegis doctor` — all green.
- Atomic commit follows.

**Spec citations (in every new file, the thinned main, this note, and the commit):**
- agent-runtime.md §Responsibilities (full 6-step loop executes, skills/tools exclusively through Hub, Memory context at start of every turn, no surface-only disclaimers)
- agent-runtime.md §Communication + §Key Interfaces
- memory-vm.md §Communication Interface §1
- security-model.md (fail-closed on LLM/tool/memory paths; only through AegisHub)
- no-stubs-plan/phase-1.md 1.1b + resolution-plan §Phase 1

**Coverage note:** New packages currently have no dedicated _test.go (the loop is exercised via the thin main + existing cmd/agent tests). ≥80% coverage target will be achieved across 1.1b + 1.3/1.4 as more tests are added (per plan).

**Ready for "continue" to 1.1c (hub vsock listener) or 1.2 (Memory skeleton).**

**Group 1.1c COMPLETE (AegisHub vsock listener for guest microVMs)**

- Added `startVsockListener` in `cmd/aegishub/main.go` (launched as goroutine from startHub).
  - Listens on vsock port 9999 (using `hubclient.HubVsockPort`).
  - Reuses `handleConnection(net.Conn, ...)` exactly (vsock.Conn satisfies the interface).
  - Graceful degradation on non-Linux / no vsock (logs and continues with unix only).
  - Matches the convention used by the client side (hubclient.DialVsock + Host=2).
- Updated `config/acls.yaml` with additive wildcard rules for `agent*` / `memory*` (so per-session "agent-uuid" and "memory-uuid" instances work when we launch real microVMs). Cited specs in comments.
- Added note to `cmd/aegishub/main_test.go` acknowledging the new vsock path (existing unix tests still cover the shared handshake/message logic).
- Full verification passed: `go test ./cmd/aegishub/...`, `make build-binaries`, `./bin/aegis doctor`.
- Atomic commit follows.

**Spec citations (code, comments, this note, commit):**
- `aegishub.md` §Handshake Sequence ("MicroVM connects to AegisHub via vsock")
- `agent-runtime.md` §Communication ("Only allowed interfaces: vsock / JSON-RPC to AegisHub")
- `security-model.md` §Isolation Strategy + §Communication & Mediation
- `no-stubs-plan/phase-1.md` 1.1c + resolution plan §Phase 1

This completes the "Add a basic vsock client to AegisHub" starting task from the user query (server-side listener for guests).

**Ready for "continue" to Group 1.2 (Memory VM real skeleton in cmd/memory + internal/memory).**

**Group 1.2 COMPLETE (Memory VM Real Skeleton)**

- Created `internal/memory/`:
  - `context.go`: ShortTermContext (hard 32k limit + auto-eviction + zeroization), LongTermMemory (TTL + semantic search + zeroization on purge).
  - `acl.go`: Strict per-agent ACL enforcement (fail-closed; one agent cannot read another's memories — memory-vm.md Test Requirements).
  - `vm.go`: VM orchestrator that ties everything together and delegates commands securely.
- Thinned `cmd/memory/main.go` to skeleton using hubclient (distributed key + zeroization, unix/vsock transport, real delegation to internal/memory).
- Removed all surface-only code, fake global stores, and disclaimers from the memory execution path.
- Added basic tests in `internal/memory/context_test.go`.
- Full verification passed (tests, build-binaries, doctor).
- Atomic commit follows.

**Spec citations (heavy emphasis on memory-vm.md):**
- memory-vm.md (Purpose, Communication Interface §1 `memory.get_context`, 32k limit + auto-summarize, ACLs, Test Requirements)
- security-model.md (per-agent isolation, zeroization, fail-closed)
- agent-runtime.md (1:1 pairing)
- aegishub.md (vsock path)

This completes the fourth starting task from the user query.

**Ready for "continue" to Group 1.3 (Integration & Wiring).**
