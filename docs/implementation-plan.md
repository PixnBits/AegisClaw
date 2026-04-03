# Implementation Plan – Hierarchical Multi-Agent Evolution

**Document Status**: Draft v0.1  
**Last Updated**: 2026-04-02  
**Owner**: Project Lead (Governance Court review required for any phase changes or major deliverables)  
**Related Documents**:  
- `docs/agentic-evolution.md` (Vision, hierarchical architecture, memory, async)  
- `docs/agent-prompts.md` (Orchestrator & Worker prompts)  
- `docs/event-bus-and-async.md` (Event Bus, timers, signals)  
- `docs/memory-store.md` (Tiered memory system)  
- `docs/web-dashboard.md` (Local control plane)  
- `docs/PRD.md` (Overall vision, security principles, phases, Governance Court)  
- `docs/architecture.md` (Current Firecracker + AegisHub design, integration notes)  

This document outlines a **phased, incremental implementation plan** to evolve AegisClaw from its current single ReAct Agent VM into a full hierarchical multi-agent platform with persistent memory, async signals/timers, tiered compaction, human approvals, and a local web dashboard — while strictly preserving paranoid isolation, Merkle auditing, and Governance Court oversight.

All work must follow the existing skill/proposal lifecycle: changes are proposed, reviewed by Court (Coder, Tester, CISO, Security Architect, User Advocate), built in Builder VM, and deployed via audited git.

## Guiding Principles
- **Incremental & Reversible**: Each phase delivers working value and can be rolled back via git + Firecracker snapshots.
- **Security First**: New components (Event Bus, Memory Store, dashboard) treated as high-risk; use Firecracker where possible, proxy injection otherwise, and full Merkle logging.
- **Hardware Targets**: 32 GB RAM + RTX 3080/4080; prefer GPU offload for Orchestrator; snapshots for fast worker/Orchestrator restore (sub-second on modern Firecracker with CoW).
- **Testing**: Integration tests with real Firecracker/KVM/Ollama; add agentic eval harness for the three key workflows.
- **Models**: Start with Qwen2.5-Coder 14B (or equivalent) for Orchestrator; lighter models for Workers; nomic-embed-text for memory.

## Phase 0: Foundations (1–2 weeks)
**Goal**: Stabilize current system and prepare infrastructure for new features without breaking existing ReAct loop or Court.

Deliverables:
- Update `docs/PRD.md` and `docs/architecture.md` to reference the new agentic docs.
- Add `aegisclaw self diagnose --extended` (resource usage, snapshot support, Ollama model health).
- Implement basic snapshot management in the daemon (create/restore Orchestrator baseline snapshot).
- Introduce ToolRegistry with `search_tools` (semantic + keyword) — replace any hardcoded tool lists.
- Add structured output enforcement (Ollama JSON mode + grammar) for ReAct in Agent VM.
- Governance Court review of all new docs (`agentic-evolution.md`, etc.).

**Acceptance Criteria**:
- Existing skills and Court reviews continue to work unchanged.
- Snapshot restore time < 5s for baseline Orchestrator.
- 98%+ ReAct format compliance on test suite.

## Phase 1: Memory Store & Retrieval (2–3 weeks)
**Goal**: Add persistent, tiered memory with semantic search — the foundation for continuity and async wakeups.

Deliverables:
- Implement encrypted SQLite + vector store backend (system keyring encryption).
- Build proxy service (injected via AegisHub) for `store_memory`, `retrieve_memory`, `compact_memory`, `delete_memory`.
- Background compaction daemon with daily cron (tier transitions: 90d → 180d → etc.).
- Optional lightweight Memory Critic (3B model) for consistency checks.
- Integrate auto-injection of memory summary on Agent VM startup (for now, via daemon).
- Update Orchestrator prompt (move to new Agent VM or enhanced current one) to enforce "memory-first" rule.
- Add Memory Vault CLI commands (`aegisclaw memory search`, `compact`, `delete`).
- Basic dashboard stub (Overview + Memory Vault page) with HTMX.

**Integration Notes**:
- All memory ops routed through AegisHub for ACL and audit.
- Start with Orchestrator-only writes; Workers get read access.

**Acceptance Criteria**:
- Agent can store/retrieve across restarts with correct tier fidelity.
- Semantic search returns relevant memories for the three workflows.
- GDPR-style delete works and is audited.
- Storage usage stays < 2 GB after 30 days of synthetic activity.

## Phase 2: Event Bus, Timers & Signals (3–4 weeks)
**Goal**: Enable async primitives and reliable wakeups.

Deliverables:
- Lightweight host-level Event Bus (SQLite-backed queue + cron timer daemon).
- Implement `set_timer`, `cancel_timer`, `subscribe_signal`, `unsubscribe_signal` as AegisHub-proxied tools.
- Cryptographic signal validation (bridge signing).
- Wakeup Dispatcher: restore Orchestrator snapshot + inject signal + memory summary as first Observation.
- Idempotency helpers and retry logic for failed wakeups.
- Update Orchestrator prompt with async rules and few-shot examples for your three workflows (research, OSS PR, recurring summaries).
- Extend Async Hub in dashboard (timers, subscriptions, signal history) with SSE real-time updates.
- Add `request_human_approval` tool and Approvals Queue page.

**Key Technical**:
- Prefer Firecracker snapshots for Orchestrator restore (fast CoW support in 2026 Firecracker).
- Resource guards: max 20 pending async items.

**Acceptance Criteria**:
- End-to-end recurring daily summary works (timer fires → wakeup → summary sent → memory compacted).
- Background research completes and notifies via dashboard.
- All events logged in Merkle tree.
- No dangling timers after cancel or completion.

## Phase 3: Hierarchical Agents & Worker Spawning (3–4 weeks)
**Goal**: Full supervisor (Orchestrator) + ephemeral Worker pattern.

Deliverables:
- `spawn_worker(task_description, role, tools_needed, timeout)` tool.
- Worker role templates (Researcher, Coder, Summarizer) with specialized prompts.
- Ephemeral Worker microVM lifecycle (spawn from snapshot or cold, destroy on completion).
- Orchestrator delegates subtasks (e.g., research deep-dive, code implementation for OSS PR).
- Update prompts for delegation logic and worker handoff.
- Live Agents dashboard page with real-time ReAct steps for Orchestrator and Workers.
- Integration tests for the three user stories end-to-end.

**Acceptance Criteria**:
- OSS issue → PR workflow completes with human approval gate before push.
- Worker destruction frees resources cleanly.
- No direct Worker-to-Worker communication (all via Orchestrator + AegisHub).

## Phase 4: Web Dashboard Polish & Compliance Foundations (2–3 weeks)
**Goal**: Production-ready UI and privacy controls.

Deliverables:
- Complete all dashboard pages (Live Agents, Skills, Async Hub, Memory Vault, Approvals, Audit Explorer, Settings).
- SSE for real-time updates across pages.
- Full CLI parity for all dashboard actions.
- Privacy features: PII redaction toggle, bulk delete, consent logging.
- SOC 2 Type 1 controls documentation (encryption, audit, access logs).
- Exportable task traces (Markdown/PDF).

**Acceptance Criteria**:
- User can monitor and control all async/memory/agent activity from dashboard.
- Dashboard itself runs with minimal privileges.
- No sensitive data leaked in UI.

## Phase 5: Evaluation, Hardening & Release (2 weeks)
**Goal**: Validate, optimize, and ship.

Deliverables:
- Agentic eval harness: synthetic + real-world tests for the three workflows (success rate, audit completeness, resource usage).
- Performance tuning: snapshot frequency, embedding quality, compaction speed.
- Resource guardrails and auto-pruning.
- Documentation updates + example skills (email bridge, git/PR helper, calendar summarizer).
- Governance Court self-review of the entire evolution.
- Tagged release with migration guide (from single-agent to hierarchical).

**Success Metrics** (aligned with PRD):
- Zero isolation violations in tests.
- Async tasks complete reliably with full audit.
- Dashboard responsive; system stays under hardware limits.
- Court can review and approve changes to the new agentic components.

## Risks & Mitigations
- **Snapshot Staleness**: Frequent baseline snapshots + validation on restore.
- **Resource Pressure**: Hard limits + monitoring in dashboard; fallback to smaller models.
- **LLM Non-Determinism**: Strict grammar + retries + critic.
- **Complexity**: Incremental phases with Court gates at each boundary.
- **Firecracker Integration**: Leverage existing lifecycle management; test snapshot restore early.

## Timeline Estimate (Sequential, with overlap possible)
- Phase 0: 1–2 weeks  
- Phase 1: 2–3 weeks  
- Phase 2: 3–4 weeks  
- Phase 3: 3–4 weeks  
- Phase 4: 2–3 weeks  
- Phase 5: 2 weeks  
**Total**: ~3–4 months for MVP hierarchical agentic system (aggressive but achievable with focused effort).

## Next Immediate Steps (After Court Review of Specs)
1. Merge the five new docs into `docs/`.
2. Start Phase 0: ToolRegistry + snapshot basics.
3. Propose and Court-review the first code changes (Memory Store proxy).

This plan is living — update it via Court-reviewed proposals as we learn from implementation.

**Decision Log**
- **2026-04-02**: Phased approach chosen to deliver value incrementally while maintaining security invariants from PRD and architecture.md.
- **2026-04-02**: Leverage Firecracker snapshots heavily for fast Orchestrator/Worker wakeups.
- **2026-04-02**: Dashboard uses HTMX + Go backend for minimal attack surface.

Any deviation from this plan requires a formal proposal and Governance Court consensus.
