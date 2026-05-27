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
