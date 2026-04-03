# Agentic Evolution – Hierarchical Multi-Agent System with Persistent Agency

**Document Status**: Draft v0.1  
**Last Updated**: 2026-04-02  
**Owner**: Project Lead (with Governance Court review required for changes)  
**Related Documents**:  
- `docs/PRD.md` (Vision, Security Principles, Governance Court, Skill Lifecycle)  
- `docs/architecture.md` (Current Firecracker-based components, AegisHub, ReAct loop, Merkle audit)  
- `docs/agent-prompts.md` (to be created – system prompts and few-shots)  

This document defines the evolution of AegisClaw from a single ReAct agent to a **hierarchical multi-agent platform** with long-term memory, asynchronous signals/timers, tiered persistence, human-in-the-loop approvals, and a local web dashboard. All changes preserve the core **paranoid-by-design** principles: zero-trust isolation via Firecracker microVMs, mandatory Governance Court oversight for new capabilities or self-improvements, append-only Merkle-tree auditing, and local-first execution with Ollama.

## Vision & Motivation

AegisClaw’s mission is a secure-by-design local agent platform that users can trust like a paranoid enterprise. The current single Agent VM + ReAct loop provides a strong foundation for tool use and skill invocation, but real-world agency requires:

- **Long-horizon reasoning** across hours, days, or weeks.
- **Proactive behavior** via timers and signals (e.g., email replies, recurring summaries).
- **Scalable decomposition** of complex tasks without context-window bloat or security violations.
- **Transparency and control** through a unified dashboard.

**Hierarchical multi-agent orchestration** (persistent Orchestrator + ephemeral specialized Workers) is the chosen pattern. This aligns with 2026 industry consensus favoring supervisor-worker and hierarchical structures for reliability, debuggability, and auditability in production agentic systems.

Key user stories to support immediately:

1. **Background Research**: User requests research → agent works asynchronously → summarizes and notifies when complete.
2. **OSS Issue to PR**: User assigns an issue → agent gathers context, proposes options (with human guidance if needed), implements/tests in isolation, creates PR, and reports back.
3. **Recurring Summaries**: User defines a periodic task (e.g., daily event digest) → agent executes on schedule, sends results, and maintains compacted history.

Success metrics (extending PRD goals):
- Zero isolation violations or unauthorized actions in async flows.
- End-to-end async task completion with full audit trail.
- Average resource usage stays within typical end-user hardware (32 GB RAM + RTX 3080/4080).
- 98%+ structured output compliance for ReAct and Court interactions.

## Hierarchical Architecture

We adopt a **two-tier hierarchical model** (supervisor/orchestrator + workers), which balances simplicity and power while fitting Firecracker constraints.

### Core Components

- **Persistent Orchestrator Agent** (evolves from current Agent VM):
  - Always-on (or snapshot-restored) microVM.
  - Runs the main ReAct loop with extended capabilities for memory, async primitives, and delegation.
  - Maintains high-level task state and coordinates Workers.
  - Uses a stronger model (e.g., Qwen2.5-Coder 14B/32B or Gemma3 equivalents, Q4/Q3 quant) for robust reasoning.
  - Communicates exclusively via AegisHub (vsock).

- **Ephemeral Worker Agents**:
  - Spawned on-demand by the Orchestrator via new tool `spawn_worker(task_description, role, tools_needed, timeout)`.
  - Short-lived microVMs (or restored from role-specific snapshots for speed).
  - Specialized prompts and lighter/faster models optimized for narrow tasks (research, coding, summarization).
  - Execute subtasks and return structured results to the Orchestrator.
  - Destroyed after completion (or on timeout/failure) to minimize attack surface and resource use.
  - Firecracker snapshot/restore support enables sub-second wakeups for common roles.

- **Central Event Bus & Timer Service** (new host-level but minimal-TCB component):
  - Runs outside VMs (managed by the root daemon) with strict sandboxing.
  - Handles timers (one-shot + cron), signal subscriptions (email reply, file change, etc.), and wakeup queuing.
  - All signals cryptographically signed and validated before delivery.
  - On fire: Orchestrator (or new Worker) is woken via snapshot restore + injected Observation.

- **Tiered Memory Store** (new):
  - Encrypted, auditable key-value + vector store (SQLite + local embeddings via Ollama or nomic-embed).
  - Accessed only via proxy tools injected into VMs.

Integration with existing architecture:
- All inter-component communication continues to route through **AegisHub** for ACL enforcement and auditing.
- Governance Court remains mandatory for any new skill, worker role template, or persistent change.
- Builder VM used for code generation in OSS/PR workflows.
- Skill VMs unchanged for final execution of approved capabilities.
- Merkle audit tree logs every delegation, memory write, signal, timer event, and approval.

**Data Flow Example (Research Workflow)**:
1. User → Orchestrator (via CLI/dashboard).
2. Orchestrator analyzes, stores initial memory, spawns Researcher Worker.
3. Worker performs research (using tools/search), returns results.
4. Orchestrator sets completion timer/signal, stores compacted findings.
5. On signal: Orchestrator wakes, summarizes, presents via dashboard/notification, marks done.

## Persistent Memory System

Memory enables continuity across sessions and async wakeups.

- **Storage Model**:
  - Structured (facts, task state) and episodic (ReAct traces, decisions).
  - All entries encrypted at rest, tagged with timestamp, security_level, task_id, and TTL tier.
  - Semantic retrieval via embeddings (agent calls `retrieve_memory(query, k, filters)` first).

- **Tiered Compaction & Retention** (configurable globally or per-tag):
  - 0–90 days: Full fidelity.
  - 91–180 days: Medium summary (70% size reduction).
  - 181–365 days: Heavy compaction (key decisions only).
  - 366 days–2 years: Ultra-summary.
  - Forever: Archive (one-sentence outcome + outcome metadata).
  - Background daemon performs daily compaction; agent can trigger `compact_memory(task_id, target_tier)`.

- **Security**:
  - PII redaction before storage (optional local classifier).
  - GDPR/CCPA right-to-forget via `privacy_delete(query)`.
  - Never store raw secrets (use `manage_secrets`).

- **Retrieval on Wakeup**:
  - Signal/timer delivery auto-injects relevant memory summary as first Observation.

## Asynchronous Primitives

- **Tools** (discovered via `search_tools`, never hardcoded in prompt):
  - `set_timer(name, trigger_at or cron, payload)`
  - `cancel_timer(timer_id)`
  - `subscribe_signal(source, filter)`
  - `unsubscribe_signal(subscription_id)`
  - `store_memory(...)`, `retrieve_memory(...)`
  - `request_human_approval(action, reason, details, expires_in)`

- **Wakeup Flow**:
  - Event Bus fires signed signal → daemon restores Orchestrator/Worker snapshot → injects signal + memory context → ReAct resumes with `Thought: Signal received from [source] for task [id]...`

- **Invariants**:
  - All actions idempotent.
  - Dangling timers/subscriptions auto-pruned after TTL or explicit cancel.
  - Max concurrency limits (e.g., 10–20 pending async items) to prevent resource exhaustion.

## Human Approvals as Tools

Approvals are explicit tools, not implicit in the agent:
- `request_human_approval(...)` pauses the current ReAct loop and notifies via dashboard.
- Destructive actions (git push, PR creation, email send with real content, skill deployment) **must** call this first.
- Governance Court still gates capability creation.

## Specialized Prompts & Role Archetypes

See `docs/agent-prompts.md` (forthcoming) for full templates.

- **Orchestrator Prompt**: ReAct + memory retrieval first + async rules + delegation logic + security invariants. Heavy few-shots for the three Day-1 workflows.
- **Worker Roles** (examples):
  - Researcher: Deep information gathering, source citation.
  - Coder/Implementer: Code generation, local testing, PR drafting.
  - Summarizer: Compaction and digest creation.
- All prompts emphasize strict output formats, security rules, and escalation on uncertainty.

## Web Dashboard Requirements

A local-first web UI (Go + HTMX or lightweight frontend) serving as the single pane of glass:

- Live Agents & Resources (running VMs, GPU/CPU usage).
- Skills & Proposals (status, Court votes, build logs).
- Async Hub (timers, signals, subscriptions with one-click cancel).
- Memory Vault (semantic search, tier view, manual compact/delete).
- Audit Log Browser (Merkle-tree query, export).
- Approvals Queue (context + approve/reject).
- Activity Timeline.
- Real-time updates via Server-Sent Events from the daemon/Event Bus.

Accessibility: Runs on localhost, dark mode, exportable traces.

## Security & Compliance Considerations

- Extends existing principles: every new surface (Event Bus, Memory Store, dashboard) treated as high-risk with Firecracker where possible and proxy injection.
- GDPR/CCPA: Consent logging, deletion endpoints, PII handling.
- Path to SOC 2 Type 1/2: Immutable logs, key management via system keyring, formal controls documentation.
- Threat mitigations: Signal signing/validation, memory critic (optional tiny model), resource guards, no direct VM-to-VM communication.

## Open Questions & Trade-offs

- Snapshot vs. cold-start for Workers: Prioritize snapshots for speed vs. simplicity of ephemeral boots.
- Memory poisoning detection: Periodic critic agent or consistency checks.
- Deeper hierarchy (multi-level supervisors): Defer until needed; start with flat Orchestrator → Workers.
- Multi-user support: Future extension via per-user isolation.

## Decision Log

- **2026-04-02**: Chose hierarchical supervisor-worker pattern for reliability and auditability (aligned with 2026 industry patterns). Persistent Orchestrator + ephemeral Workers fits Firecracker snapshot capabilities.
- **2026-04-02**: Tiered memory compaction with 90/180/365/2yr/forever buckets per user request.
- **2026-04-02**: Human approvals as explicit tools; no implicit autonomy for destructive actions.
- **2026-04-02**: Event Bus as minimal host-level service with vsock/AegisHub integration.

**Next Actions**:
- Create `docs/agent-prompts.md` with full prompt templates and few-shots.
- Draft implementation plan (`docs/implementation-plan.md`) with phases.
- Update `docs/PRD.md` and `docs/architecture.md` to reference this document.
- Governance Court review of this spec before coding begins.

Changes to this document must follow the full Skill/Proposals lifecycle through Governance Court.
