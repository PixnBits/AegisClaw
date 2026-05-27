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
