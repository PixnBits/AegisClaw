# Agent Customization Specification

## Overview
Users can customize agent behavior and provide skills through the `~/.aegis/agents/` directory structure. This supports both shared configuration and per-agent customization.

## Directory Structure

```
~/.aegis/agents/
├── shared/
│   ├── TOOLS.md
│   └── skills/
├── default/
│   ├── SOUL.md
│   ├── AGENTS.md
│   ├── SETTINGS.yaml
│   └── skills/
├── researcher/
│   ├── SOUL.md
│   ├── AGENTS.md
│   ├── SETTINGS.yaml
│   └── skills/
└── analyst/
    ├── SOUL.md
│   ├── AGENTS.md
│   ├── SETTINGS.yaml
│   └── skills/
```

### Skills Format (agentskills.io style)
Each skill lives in its own folder inside `skills/` (e.g. `web-research/`) with at least `SKILL.md` containing YAML frontmatter.

## Loading Rules
- `shared/` files apply to all agents
- Per-agent files override shared ones
- Skills in an agent's `skills/` folder are only available to that agent
- `SETTINGS.yaml` (if present) provides structured overrides for model, sampling params, tool scopes, and autonomy hints

## Per-Agent Settings

Beyond SOUL (system prompt) and skills, each agent supports structured configuration for precise control over inference behavior and resource use.

**Recommended file per agent:** `SETTINGS.yaml` (YAML for easy parsing + human editing).

### Settings Schema (draft)

```yaml
# ~/.aegis/agents/<name>/SETTINGS.yaml
model: "qwen2.5-coder:32b"   # pinned model or "inherit" / "default"
temperature: 0.7
top_p: 0.95
max_tokens: 8192
presence_penalty: 0.0
frequency_penalty: 0.0

# Autonomy & scoping
autonomy_level: 1                 # 0=passive, 1=proactive, 2=independent
auto_initiate: false
enabled_tools: ["web_search", "code_execution", "file_read"]
disabled_skills: []

# Additional persistent guidance (appended or merged with SOUL)
extra_system_instructions: |
  You are particularly careful with financial calculations.
  Always show your work for code changes.

# Observability preferences (future)
log_level: info
metrics_granularity: high
```

**Loading & Precedence:**
- Per-agent SETTINGS.yaml overrides shared/default.
- Runtime merges with SOUL.md (SOUL remains the primary system prompt; extra_instructions appended cleanly).
- Changes to running agents should support hot-reload where safe (control plane notification, no full restart for prompt-only changes).

**Validation on write:**
- Schema validation + security linting (no dangerous tool grants for low-autonomy agents).
- Atomic write + backup of previous version.
- For agents at autonomy Level 2+, significant setting changes may route through Court review (future extension).

## LLM Usage Metrics & Observability

Local users need transparent visibility into inference usage (tokens as proxy for compute and energy). Tracking must be accurate, isolated, and exposed without compromising agent security boundaries.

### What to Track (per inference call)
- `agent_id` / persona
- `timestamp` (RFC3339, high resolution)
- `model` (exact identifier used)
- `tokens_prompt`
- `tokens_completion`
- `duration_ms` (wall time for the call)
- `success` + optional `error_type`
- `correlation` (channel_id, plan_id, trace_id, or request_id for linking to Single-Agent Trace)
- Optional: tool context or phase (Observe/Think/Act)

### Storage Strategy
- **Primary:** Lightweight append-only or time-bucketed records in the Store VM (or a minimal dedicated metrics component with tiny TCB).
- High-resolution recent data (last hour) kept hot (in-memory ring buffer or fast key-value).
- Daily + monthly rollups persisted for efficient aggregate queries.
- Retention: user-configurable (default 90 days); fully local, no external transmission.
- Tamper-evident where possible (hash chaining or integration with existing audit log).

### Required Aggregates
The system must compute and expose:
- **Grand total** (all-time tokens in + out, calls, by model)
- **Last hour**
- **Today** (and Month-to-Date style windows)
- **Custom time range** (user selectable in portal)
- Breakdowns: by agent, by model, by day/hour, success vs error

These must be queryable quickly for the web portal without scanning raw logs every time.

### Collection Points
- Every LLM invocation boundary (inside guest runtime or controlled bridge to Ollama/local model server).
- Must emit structured usage events **outside** the agent's own context and memory to preserve isolation.
- No token counting logic inside untrusted guest code.

### Portal & Real-time Exposure
- Metrics streamed live via STOMP (topic patterns such as `metrics.llm-usage` and `metrics.llm-usage.<agent-id>`).
- Individual agent pages and Dashboard consume these for live cards and charts.
- Historical queries via REST/portalbridge with appropriate time bucketing.

## Web Portal: Individual Agents Page

Users manage and observe agents through a dedicated individual agents view (list + detail). This is the primary surface for the customization and metrics requirements.

### List View
- Searchable, filterable grid or card list of all configured agents.
- Per card: name + persona/role, current autonomy level, quick status, sparkline of recent token usage, last active time.
- Quick actions: Open detail, Pause/Resume, View Trace.

### Detail / Individual Agent Page
- **Header**: Agent identity, originating channel/plan (links), current narrow scope, live status indicator.
- **Tabs or sections**:
  - **Configuration** (primary editable surface):
    - SOUL.md editor (multi-line text with preview or syntax highlighting; save = atomic file write).
    - SETTINGS.yaml form (typed controls for model, temperature, max_tokens, autonomy toggles + raw YAML fallback editor).
    - Skills & tools matrix (enable/disable with clear security warnings for powerful tools).
    - Save button with diff preview + confirmation; success triggers hot-reload notification where applicable.
  - **Metrics & Observability**:
    - Prominent summary cards: Grand Total, Last Hour, Today, MTD (tokens in/out, calls, avg tokens/call).
    - Model breakdown (pie or stacked bar).
    - Time-series chart (tokens or calls over selected window; zoom/pan, granularity auto or user-controlled).
    - Recent activity table (last N calls with model, tokens, duration, correlation link to trace).
    - Export (CSV/JSON) for the current view.
    - Live updating via STOMP when agent is active.
  - **Activity / Trace**: Embed or deep-link to Single-Agent Trace view.
- **Actions sidebar**: Pause/Resume/Cancel, Reset metrics (with confirmation), Delete agent (guarded), View full audit log.

### Real-time Behavior
- Metric cards and chart update in near real-time without page refresh.
- Configuration changes reflect immediately in running agents where hot-reload is implemented.
- Optimistic UI updates with rollback on failure.

### Edge Cases & Polish
- No agents configured: helpful empty state with "Create first agent" guidance linking to onboarding.
- Very long metric history: virtualized table + server-side bucketing.
- High-frequency updates: debounce + efficient diffing.
- Accessibility: full keyboard navigation, ARIA labels on charts and editors.

### Persona Fit
- **Alex Rivera** (Power User): Deep customization + honest usage visibility builds trust and control.
- **Sam Chen** (Tech Lead): Quick oversight of team agent spend and settings drift.
- **Dr. Lena Moreau** (Security/Governance): Audit trail of setting changes and usage patterns.

## Implementation Considerations (Target State)
- Backend: Agent config service (or extension of portalbridge) for validated read/write of per-agent files. Metrics emitter integrated at inference boundary. Aggregate engine in Store VM or lightweight sidecar.
- Frontend: New or extended route/component under `/agents` or agent detail; reuse existing design tokens, STOMP client, and chart primitives.
- Security: All writes validated and audited. Settings changes for Level 2+ agents may require Court proposal in future. Metrics never leak across isolation boundaries.
- Performance: Aggregates pre-computed; portal queries are O(1) or O(log) for common windows.

## Traceability
**Driven by:**
- Multi-agent team workflows (Journey #8)
- Strong user customization needs
- Local compute observability and trust-building (users deserve clear visibility into their own token usage and energy implications)
- Web portal individual agents page requirements
- Existing filesystem customization foundation (`SETTINGS.yaml` + SOUL per agent)