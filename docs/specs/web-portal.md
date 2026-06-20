# Web Portal Application Specification

## Overview
The AegisClaw Web Portal is the primary rich, real-time web interface for users, reviewers, and operators. It runs in a dedicated isolated Web Portal VM (see `web-portal-vm.md`) and provides visibility, interaction, and control across chat, agents, governance (Court/approvals), SDLC flows (proposals, PRs, source, git, build), memory, audit, workspace, and system state.

It is **presentation-only**: no business logic, no persistent state, no secrets. All data and actions flow through the trusted vsock API bridge to the Host Daemon's API surface (mediated by AegisHub where applicable).

**Golden Rule alignment**: Major user journeys (chat, proposal review, Court decisions, SDLC) have or require Playwright E2E coverage (see `web_portal_e2e_sdlc_test.go` and testing-standards.md).

## Current Architecture & Runtime (Implementation)
- **Entry**: `cmd/aegisportal/main.go` — mounts minimal FS, listens vsock:18080, creates `dashboard.Server` with `portalAPIClient` that dials host bridge (vsock port 1030).
- **Core**: `internal/dashboard/server.go` (~3388 LOC) — pure `html/template` + embedded CSS/JS, `http.ServeMux`, `APIClient` abstraction.
  - Real-time: evolving from global SSE `/events` (1s ticker + per-event cursors) + chat hybrid streaming to **STOMP-over-WebSocket topic subscriptions** for efficient, view-specific updates (detailed in new STOMP section below); existing SSE/chat paths retained for transition compatibility.
  - Bridge actions are dispatched via `apiSrv.CallDirect` (some trusted per `isTrustedPortalBridgeAction`).
- **Handlers present** (many wired in `daemon_handlers_extended.go`, git/workspace in `handlers_git.go`, PRs in `dashboard_pr_handlers.go`, events in `tool_events.go`/`thought_events.go`):
  - Core UI + chat + approvals + memory + workers + sandboxes + skills/proposals + async + audit + settings.
  - Phase 2+: Source browser (`git.browse`/`git.branches`), workspace list/edit/read, git history/diff, PR list/detail.
- **Data flow**: Browser → Portal VM (HTTP + WebSocket/STOMP) → vsock bridge → Host API (direct or ControlPlaneProxy → AegisHub → backends like Store/Agent VMs).
- **Styling/JS**: Self-contained GitHub-inspired dark theme. Heavy client-side Markdown renderer + streaming logic for chat/Canvas. No CDNs. STOMP client (lightweight self-contained) added for real-time topics.

**Note on completeness**: UI scaffolding and many full experiences (chat, Canvas, overview, approvals, proposal detail, source/workspace) are implemented in the portal. Some backend action registrations (e.g., full `git.*`, `workspace.*`, `dashboard.*` legacy) and `/api/*` REST surface are partial or delegated via proxy/hub; fallbacks or errors surface clearly. See also `sdlc-web-portal.md` for SDLC vision that drove many of these features (Source, Git, PRs, Workspace, build visibility).

## Look and Feel (Current Implementation)
**Design philosophy** (updated from legacy): Dark, high-contrast "GitHub command center" aesthetic for security/paranoia — clarity, live feedback, state badges prominent, emergency/approval actions safe and visible. Fully self-contained.

**Color Palette** (from `dashboardCSS` in `internal/dashboard/server.go:894`):
- Background: `#0d1117`
- Surfaces / Elevated: `#161b22`, `#21262d`, `#30363d`
- Accent / Primary: `#58a6ff`
- Success / Approved / Running: `#3fb950` (badges `.badge-running`, `.badge-approved`)
- Warning / Pending / Draft: `#d29922`
- Danger / Failed / Rejected / Error: `#f85149`
- Muted / Secondary text: `#8b949e`
- Borders: `#30363d`, `#21262d`
- Code / Logs / Monospace areas: `#0f151d`, `#0b1016`

**Typography**: `system-ui, -apple-system, sans-serif`; monospace `ui-monospace, SFMono-Regular, Menlo, Consolas...`

**Components**:
- Top `<nav>`: logo (🛡️ AegisClaw), links, live status (SSE or STOMP connection indicator).
- `<main>` with `<h1>`, `.section` cards (header + content, hover rows, overflow hidden).
- Tables with badges (dynamic `badge-{{status}}`).
- Forms for decisions (approve/danger buttons), search, chat input.
- Chat: fixed layout, sidebar sessions, bubbles (user right blue, assistant left), `.markdown-bubble` full parser (headings, lists, tasks, tables, code, blockquotes, hr, links sanitized), `.thought-log`, `.tool-log`, model pills, streaming deltas.
- Canvas: grid of agent cards, tool feed, ascii interaction graph, live log — updated via STOMP topics or SSE.
- Responsive (mobile adjustments for chat sidebar/bubbles).
- Empty states, muted text, disclosure `<details>` for tools.

**Navigation (exact current)**:
Overview • Canvas • Chat • Agents • Skills • PRs • Source • Git • Workspace • Async Hub • Memory • Approvals • Audit • Settings

(Header also shows live connection status via real-time script.)

This replaces the older wireframe nav (Conversations/Teams/Court/Monitoring).

## Key Features & Screens (Current)
See also target-state specs in `docs/specs/web-portal/` for the redesign direction; below reflects actual rendered templates + JS.

### 1. Overview (`/`)
Stat cards grid (Running MicroVMs, Active Workers, Pending Approvals, Active Timers, Memory Entries, vCPUs/Mem allocated, Host RAM bar + Load). Tables for Running VMs (with RSS/CPU), Active Workers, Pending Approvals summary. Initial fetch via `system.stats` etc.; **live updates via STOMP subscription to `/topic/overview.stats`** (preferred) or existing SSE bundles. Quick links to other areas.

### 2. Canvas (`/canvas`) — Live Visual Workspace
Agent cards grid (from workers), tool-call feed per agent, "Agent Interaction Graph" (text), Live Tool-Call Log. Uses initial data + **STOMP topics** (e.g. `/topic/canvas.events` or per-worker/tool) or SSE (`tool_start`/`tool_end`, `worker_start`). Real-time collaborative monitoring view. (Implements monitoring + team concepts from journeys #5/#8.)

### 3. Chat (`/chat`) — Full Interactive Sessions
Sidebar: sessions list + New button (server-backed via Store VM through thin `/api/chat/sessions`, `/api/chat/history`, and `/api/chat/sessions/{id}` bridge actions; the previous `aegisclaw.chat.sessions.v1` localStorage silo has been removed so conversations are visible across browsers/devices). Chat turns are sent via `/chat/send` → `chat.message` through the agent chat system; the portal only forwards messages and persists the session thread back to Store.
Main: message stream (user/assistant/error bubbles), rich input textarea.
- Full client Markdown (headings 1-3, task lists [x], ul/ol, tables with align, code blocks/fences, inline code, **bold**, *em*, ~~s~~, blockquotes, hr, sanitized links).
- **Streaming**: `/chat/send` with Accept SSE or `?stream=1` → progressive `content_delta`/`thought_delta`, tool/thought events (hybrid during transition); **preferred path migrates to per-session STOMP topic `/topic/conversation.{sessionId}.updates`** for targeted, low-overhead deltas without global fan-out. Logs: `.tool-log` (calls, payload, duration, status), `.thought-log` (phases: model_thinking etc, model/tool, timestamps).
- Model pills, typing, error handling, history carry-over.
- Supports sessions, history.

### 4. Agents (`/agents`)
Table of workers: ID (truncated), Role, Status (badge), Steps, Task, Spawned. Supports active_only=false.

### 5. Skills & Proposals (`/skills`)
Sections:
- Runtime Skills (name/desc/version/state/sandbox/tools disclosure; links to proposals).
- Built-In Baselines.
- Built-In Templates.
- Proposals table (ID, title, status badge, category, target, link to `/skills/proposals/{id}`).
Uses `dashboard.skills` action (legacy; falls back to error display in template).

### 6. Proposal Detail (`/skills/proposals/{id}`)
Summary table (title, status, category, risk, round, version, author, target, timestamps).
Current Review Status grid (round counts, approvals/rejects/asks/abstains).
Feedback tables: Current Round + Previous Rounds (persona, verdict badge, risk, comments, questions, ts).
Revision & Status History table.
Uses `dashboard.proposal` action.

### 7. PRs (`/pullrequests`, `/pullrequests/detail?id=...`)
List with status filters (open/merged/closed). Calls `pr.list`/`pr.get`. Currently minimal "feature implemented" placeholder (see `sdlc-web-portal.md` + `dashboard_pr_handlers.go` for intended rich dashboard-optimized shapes with court reviews, can_merge, build/security status, etc.). PR auto-create from pipeline supported in design.

### 8. Source (`/source`, `/source/browse?path=...`)
Git branches for "skills" repo. Browse returns JSON (for potential client nav). Phase 2 source code browser per `sdlc-web-portal.md`.

### 9. Workspace (`/workspace`, edit POST)
Lists files (from `workspace.list`). Edit modal (JS) with read via `/api/workspace/read` (POST JSON) then form submit to `/workspace/edit` (`workspace.write`). Supports SOUL/AGENTS/TOOLS/*.SKILL.md per internal/workspace. Audit logged.

### 10. Git (`/git?proposal=...`, `/git/diff?proposal=...`)
Branches + commits for proposal branches. Diff view. Phase 3 git history.

### 11. Async Hub (`/async`)
Active Timers table + Recent Signals table. (event.timers/signals)

### 12. Memory Vault (`/memory?q=...`)
Search form + list/search results (key, value truncated, ttl_tier). `memory.list` / `memory.search`.

### 13. Approvals (`/approvals?all=1`, POST `/approvals/decide`)
Pending (or all) approvals list with risk badges, description, decide form (approve/reject + optional reason). Calls `event.approvals.list` + `.decide`. Core governance UI. **Live updates via STOMP `/topic/approvals.pending`.**

### 14. Audit (`/audit`)
Merkle log info + CLI commands to inspect/verify. (Full explorer future.)

### 15. Settings (`/settings`)
Config file location, key settings table (structured output, memory TTL/PII, quotas, dashboard addr). Privacy/PII redaction section + GDPR note.

Additional: `/health` (ok), real-time endpoints (SSE + STOMP), error pages, truncation helpers, fmt helpers.

## Real-time & Streaming
- Global SSE `/events`: heartbeat + update bundles (active_workers, pending_approvals, tool_events, thought_events, sessions). Emits granular `tool_start`/`tool_end`, `worker_start` for Canvas. **Being supplemented/replaced by STOMP topic subscriptions for targeted delivery.**
- Chat streaming: hybrid (background call + ticker polling events/progress, emit deltas to avoid full re-renders). Suppresses in-flight structured JSON/fences during stream. **Migrating to per-conversation STOMP topic for efficiency.**
- Event buffers (`ToolEventBuffer`, `ThoughtEventBuffer`) with contract tests in `portal_contract_test.go` (exact JSON shape for id, timestamp, tool, phase, success, duration_ms, etc.). These extend to STOMP `MESSAGE` payloads.

## STOMP Topic Subscriptions (New Real-Time Mechanism)

**Rationale**: The current global SSE approach (single ticker pushing bundles to all connected clients) is functional but indiscriminate. As event volume grows (tool calls, thoughts, state changes, multi-agent activity), it transfers and processes more data than necessary. STOMP topic subscriptions allow the browser to declare exactly what it needs and receive only relevant updates, directly reducing network transfer and portal compute while improving responsiveness for different views.

**Architecture**:
- Portal Go server adds WebSocket handler supporting STOMP frame protocol (minimal 1.2 subset: CONNECT, SUBSCRIBE, UNSUBSCRIBE, MESSAGE, RECEIPT, ERROR, heartbeats). Kept lightweight and self-contained to preserve minimal TCB.
- STOMP endpoint exposed alongside HTTP (WS upgrade supported by Host Daemon reverse proxy — see recent networking readiness work in `web-portal-vm.md`).
- Client JS (embedded): lightweight STOMP client connects on dashboard load or view mount, subscribes to topics relevant to current screen, renders incoming MESSAGE frames (reusing existing delta/Markdown rendering logic), unsubscribes on navigation or visibility change.
- Server-side subscriber management: map of topic → active STOMP sessions. On relevant internal event receipt (via bridge or event callbacks), route only to matching subscribers as STOMP MESSAGE frames. Translation layer maps internal event names/payloads to topic destinations.
- Integration with existing bridge: STOMP SUBSCRIBE can trigger `event.subscribe` calls for cross-VM coordination; most UI updates are local to portal's knowledge of backend state.

**Initial Topics** (expand as needed):
- `/topic/overview.stats` for Overview screen live stat cards/tables.
- `/topic/conversation.{sessionId}.updates` for Chat per-session streaming (content, thoughts, tools).
- `/topic/approvals.pending` for Approvals list.
- `/topic/canvas.events` (and granular variants) for Canvas live feeds.

**Client Lifecycle**:
- Mount/view enter: CONNECT (if needed) + SUBSCRIBE to screen-specific topics.
- Receive MESSAGE → update DOM (append deltas, update badges, etc.) with existing sanitization/rendering.
- Navigate away / tab hidden: UNSUBSCRIBE (and DISCONNECT if last).
- Reconnect + resubscribe on connection loss with backoff.

**Performance Wins**:
- Network: only subscribed data crosses the wire to browser (no global bundles or ignored events).
- Compute (portal): no per-client filtering or unnecessary serialisation; targeted fan-out.
- Upstream: fewer wasted bridge calls or event processings for uninterested clients.
- Scales better with concurrent users/tabs and higher event rates.

**Migration**:
- Phase in: Overview and one conversation topic first. Keep SSE paths live and tested.
- E2E/Contract: extend `portal_contract_test.go` and Playwright suites to cover STOMP connect/sub/unsub flows, message delivery, and absence of irrelevant updates.
- Fallback: if STOMP unavailable, degrade to existing SSE gracefully.

**Non-Goals / Constraints**:
- No external message broker or new VM.
- Portal remains strictly presentation-only.
- All real-time traffic proxied and mediated; no direct browser-to-AegisHub or browser-to-agent paths.
- Implementation must stay minimal, auditable, and consistent with paranoid security model (frame validation, size/rate limits, no secrets in portal).

This mechanism directly addresses resource usage concerns while enhancing the real-time feel of the dashboard.

## API for the Web Portal (Design + Current)
The portal follows the design articulated in `docs/specs/sdlc-web-portal.md` (SDLC visibility, proposal→Court→build→PR→deploy transparency), target-state specs in `docs/specs/web-portal/`, `docs/specs/chat-ui-data-flow.md`, E2E contract in `web_portal_e2e_sdlc_test.go`, and trusted bridge list in `dashboard_daemon.go`.

### Consumed Internal Actions (via bridge, many trusted)
- `worker.list` / `worker.status`
- `sandbox.list`
- `skill.list` / `skill.status` / activate/deactivate
- `chat.message` (core + streaming), `chat.summarize`, `chat.tool`, `chat.stream_progress`, `chat.tool_events`, `chat.thought_events`
- `event.approvals.list` / `.decide`, `event.timers.list`, `event.signals.list`
- `memory.list` / `memory.search`
- `sessions.list` / `.history` / `.status` / send/spawn/pause/resume/cancel
- `pr.list` / `pr.get` (and intended `dashboard.pr.list` / `.detail` / `.stats`)
- `git.branches` / `git.browse` / `git.commits` / `git.diff`
- `workspace.list` / `workspace.write` / `workspace.read`
- `system.stats`
- `dashboard.skills` (legacy catalog + proposals), `dashboard.proposal`
- `court.decisions.list` / `.show`
- `team.*`, `autonomy.*`, `proposal.*`, `tasks.*` (for future team/court views)
- Others via proxy (e.g., vault, kernel control for privileged).

See `isTrustedPortalBridgeAction` and `fetchRaw`/`apiClient.Call` usage. Context trusted for portal-originated sensitive actions.

### Public REST / JSON API Surface (exposed by Portal for clients/E2E/programmatic access)
Aligned with E2E test expectations and `sdlc-web-portal.md` "WEB PORTAL:" callouts for direct visibility without CLI. Same origin as HTML UI. Returns JSON; errors as `{ "error": "..." }` or standard HTTP.

**Current / Minimal (to be expanded per design):**
- `GET /health` → "ok"
- `POST /chat/send` (JSON or form; supports stream)
- `POST /approvals/decide`
- `POST /workspace/edit`
- `GET /source/browse` (returns JSON content for path)
- `GET /git/diff?...` (HTML but data-driven)
- `POST /api/workspace/read` (JSON `{filename}` → `{success, data: {content}}` or error; intended to call `workspace.read` action)

**Design (from E2E test + sdlc-web-portal; implement to this contract):**
- `POST /api/proposals` (body: `{ "title": "...", "description": "...", "permissions": [...] }`) → `201 { "id": "prop-..." }`
- `GET /api/proposals/{id}/status` → `{ "phase": "review|build|...", "court_approved": bool, "code_generated": bool, "pr_url": "...", "deployed": bool, "error": "..." }`
- `GET /api/proposals/{id}/audit` → text/markdown audit trail for the proposal SDLC
- Recommended additions for completeness (per design docs):
  - `GET /api/skills`, `GET /api/proposals`, `GET /api/approvals?pending=1`
  - `GET /api/court/decisions?proposal=...`
  - `POST /api/approvals/{id}/decide` (or reuse existing)
  - `GET /api/build/status?proposal=...` (live pipeline logs/SBOM/gates)
  - `GET /api/prs?status=...` (rich per `dashboard.pr.*` shapes: includes court_reviews, build_passed, can_merge, files_changed etc.)
  - `POST /api/prs/{id}/merge`, comment endpoints for threaded reviews.

The shapes for dashboard PRs/Court are defined in `dashboard_pr_handlers.go` (`dashboardPRSummary`, `dashboardPRDetail` with CourtReviews) and `internal/pullrequest`.

All public API calls from browser are untrusted by default (unless marked); sensitive ones require the approval flow or future auth.

**Contract tests**: `portal_contract_test.go` locks event JSON for dashboard consumers. Extend for new STOMP payloads and API responses.

## Testability & E2E
- Stable elements for Playwright (data-testid recommended for future; current uses IDs/classes).
- E2E: `web_portal_e2e_sdlc_test.go` (full autonomous proposal→Court→PR→deploy via portal API + UI). **Extend for STOMP connection, subscribe/unsubscribe, and topic-specific delivery.**
- Unit: server_test.go (stubs), internal tests. Add STOMP frame and subscription manager tests.
- Must handle backend unavailability gracefully (error banners, empty states).
- Per testing-standards: E2E for portal flows required for journey completion.

## Security & Non-Responsibilities
- Same as `web-portal-vm.md`: zero privileges, input untrusted, no secrets, mediated.
- All mutations (edits, decisions, chat) go through validated API + Court where required.
- Sanitization in chat renderer (link/protocol checks, HTML escape).
- STOMP implementation: frame validation, size limits, rate limiting, no secret exposure. All traffic proxied.

## Related Documents & Traceability
- `web-portal-vm.md`, `docs/specs/web-portal/` (target-state modular specs)
- `sdlc-web-portal.md` (SDLC portal vision + gaps that drove Canvas/PRs/Source/Workspace/Git)
- `chat-ui-data-flow.md` (streaming/RAIL requirements; update for STOMP per-conversation path)
- `event-system.md` (now includes STOMP gateway role)
- `architecture.md`, `prd/sdlc-governance.md`, `specs/governance-court.md`
- `cmd/aegisclaw/portal_contract_test.go`, `web_portal_e2e_sdlc_test.go`
- `dashboard_daemon.go` (bridge + trusted actions)
- User journeys #2,3,4,5,6,9 (chat, monitoring, proposals/court, SDLC)

**Driven by**: All user journeys involving visibility/control; Phase 2-4 roadmap items; paranoid transparency requirement; explicit goal to reduce real-time data transfer and compute via topic subscriptions.

## Open / Next (from gaps + code review)
- Wire/register remaining `git.*`/`workspace.*`/`dashboard.skills` (or migrate to canonical `proposal.*`/`pr.*`/`skill.*`).
- Implement full public `/api/proposals*` + rich PR/Court REST per design + E2E.
- **Implement STOMP gateway and topic handlers in dashboard server** (per new STOMP section); extend contract tests and Playwright coverage.
- Add `data-testid` + Playwright coverage for all new screens (Canvas, detailed chat streaming, proposal rounds, workspace edit, source browse).
- Expand Canvas to full team graph + autonomy controls.
- SBOM / gate results / diff viewers in PR/Proposal detail (per `sdlc-web-portal.md` phases 4-5).
- Update `additional-requirements-and-gaps.md` and this spec as wiring completes.

Update `CHANGELOG.md` on major portal milestone completion (including STOMP real-time).