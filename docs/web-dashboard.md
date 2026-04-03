# Web Dashboard – Local Control Plane for AegisClaw

**Document Status**: Draft v0.1  
**Last Updated**: 2026-04-02  
**Owner**: Project Lead (Governance Court review required before implementation)  
**Related Documents**:  
- `docs/agentic-evolution.md` (Hierarchical multi-agent system, async primitives, memory system)  
- `docs/event-bus-and-async.md` (Timers, signals, wakeup flows)  
- `docs/memory-store.md` (Tiered memory, retrieval, compaction)  
- `docs/agent-prompts.md` (Orchestrator & Worker prompts)  
- `docs/PRD.md` (Paranoid-by-design, transparency, auditability, user control)  
- `docs/architecture.md` (Firecracker isolation, AegisHub, Merkle audit)  

This document specifies the **local web dashboard** — the primary user interface for interacting with, monitoring, and controlling the hierarchical multi-agent system in AegisClaw.

## Vision & Goals

The dashboard serves as the **single pane of glass** for paranoid users who want full visibility and control without sacrificing security. It replaces scattered CLI commands with an intuitive, real-time interface while keeping everything 100% local (no cloud, no telemetry).

Core principles:
- Zero trust: All data comes from audited sources (Merkle tree, Event Bus, Memory Store).
- Full transparency: Every action, timer, memory entry, and agent step is visible.
- One-click control: Approve, cancel, delete, or inspect anything.
- Responsive & lightweight: Runs comfortably on 32 GB machines alongside the agents.
- Accessibility: Works on desktop and laptop (mobile-friendly as secondary).

## Core Pages / Sections

### 1. Overview / Home
- System health summary (running VMs, GPU/CPU/RAM usage, active Orchestrator).
- Recent activity timeline (last 10 actions across all components).
- Quick stats: Pending approvals, active timers, memory store size by tier.
- Global search bar (semantic across tasks, memories, logs).

### 2. Live Agents
- List of all running microVMs (Orchestrator + any active Workers).
- Real-time resource usage (CPU, RAM, GPU layers offloaded).
- Current ReAct step / last thought for the Orchestrator.
- One-click pause / snapshot / terminate (with confirmation).

### 3. Skills & Proposals
- Table of all skills (name, version, status, security level).
- Pending proposals with Court vote progress and logs.
- Build status for new skills.
- One-click view full proposal + audit trail.

### 4. Async Hub (Timers & Signals)
- **Timers**: List of active / expired / cancelled timers with details (name, next trigger, payload, task_id).
  - One-click: Cancel, manual trigger (for testing), edit (limited).
- **Subscriptions**: Active signal subscriptions (source, filter, linked task).
  - One-click unsubscribe.
- **Signal History**: Recent signals received with source and outcome.
- Filters: by task_id, source, status.

### 5. Memory Vault
- Semantic search interface (natural language query + filters: tier, tags, date range, task_id).
- Results displayed with tier badge, creation date, and preview (click to expand full entry).
- Actions per memory: View raw, compact now, delete (with reason for audit), export.
- Tier distribution chart (visual breakdown of storage usage).
- Manual compaction button for selected items or whole store.

### 6. Approvals Queue
- Pending human approvals with full context (action, reason, details, expires_in).
- One-click Approve / Reject with optional comment.
- History of past approvals/rejections linked to task audit.

### 7. Audit Log Explorer
- Merkle-tree powered search (by task_id, timestamp, actor, action type).
- Timeline view and raw JSON export.
- Diff view for memory compaction events.
- Export full task trace as Markdown or PDF.

### 8. Settings & Policies
- Memory policy configuration (default tiers, max store size).
- Model selection (Orchestrator vs. Worker models).
- Privacy controls (PII redaction toggle, deletion requests).
- Notification preferences (how summaries and completions are delivered: dashboard toast, system notification, email bridge, etc.).
- Snapshot settings (frequency for Orchestrator).

## Technical Implementation

**Stack** (lightweight & local-first):
- **Backend**: Go (same binary as main daemon) with embedded HTTP server.
- **Frontend**: HTMX + Tailwind CSS (minimal JS, server-rendered where possible) for simplicity and security.  
  Alternative: SvelteKit if richer interactivity is desired (still fully local).
- **Real-time updates**: Server-Sent Events (SSE) from the daemon for live agent steps, timer firings, signal arrivals, and approval requests.
- **Authentication**: None required (runs on `localhost:port` only; optional mTLS or simple token for advanced users).
- **Data sources**:
  - All reads go through audited proxies (Event Bus, Memory Store, Merkle tree).
  - No direct database access from frontend.

**Performance Targets**:
- Page loads < 300 ms on typical hardware.
- SSE latency < 1 s for live updates.
- Handles 50+ concurrent timers/subscriptions gracefully.

**Security**:
- All API endpoints protected by AegisHub-style ACLs.
- No external scripts or CDNs.
- Content-Security-Policy (CSP) enforced.
- Sensitive data (secrets, raw payloads) never rendered; only summaries or redacted views.
- Dashboard itself can be disabled or run in “read-only” mode via config.

## User Workflows Enabled

- **Background Research**: User starts task via CLI or dashboard → watches progress in Live Agents → receives completion notification → reads summary in Activity + Memory Vault.
- **OSS Issue to PR**: Monitors proposal status, reviews options in Approvals Queue, approves implementation → tracks Worker activity → views final PR link in activity feed.
- **Recurring Summaries**: Views and manages the daily timer in Async Hub → reviews past summaries in Memory Vault (with tier indicators) → exports monthly digest.

## Integration Points

- **Event Bus**: Pushes timer/signal events via SSE.
- **Memory Store**: Powers semantic search and tier visualizations.
- **Orchestrator**: Exposes current ReAct state and allows manual intervention (e.g., force clarification).
- **Merkle Audit**: All views are backed by tamper-evident logs.
- **CLI parity**: Every dashboard action has an equivalent `aegisclaw` CLI command (and vice versa).

## Open Questions & Trade-offs

- HTMX vs. SvelteKit: Prioritize extreme simplicity and small attack surface (HTMX) or richer UX (Svelte)?
- Mobile support: Full responsive design or desktop-first with progressive enhancement?
- Notification delivery: Should dashboard push system notifications (via native APIs) or rely on bridges?
- Read-only mode for high-security users.

## Decision Log

- **2026-04-02**: Dashboard is local-first, runs in the main daemon, uses SSE for real-time updates.
- **2026-04-02**: HTMX + Tailwind chosen as baseline for minimal dependencies and security.
- **2026-04-02**: All destructive actions (cancel timer, delete memory, approve) require explicit user confirmation and are auditable.
- **2026-04-02**: Semantic search in Memory Vault uses the same embedding model as the agent memory system.

**Next Actions**:
- Prototype the Overview and Async Hub pages.
- Define exact API contracts between dashboard and daemon.
- Add dashboard launch command (`aegisclaw dashboard`).
- Governance Court review of UI/UX flows for approval and deletion actions.
- Integrate with web server setup in the main binary.

Any significant changes to dashboard functionality, technology choices, or exposed controls must be proposed through the standard Governance Court process and documented in this file.

Changes to this document follow the same review requirements as other architecture specs.
