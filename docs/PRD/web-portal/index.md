# Web Portal Redesign PRD

**Status**: Draft / In Review  
**Branch**: feat/web-portal-prd-redesign  
**Owner**: PixnBits  
**Date**: 2026-06-17  

## Executive Summary

This PRD defines the redesigned AegisClaw Web Portal as a calm, immediately productive, paranoid-yet-empowering command center. It makes the core "harness" of the system (PM-orchestrated narrow-scope specialist agents + adversarial multi-persona Court review + structured pipeline + full observability) feel tangible, trustworthy, and delightful.

The design draws from:
- Detailed user personas (Alex Rivera, Jordan Hale, Sam Chen, Dr. Lena Moreau) and their journeys.
- Existing specs (collaboration-model, user-journeys, `docs/specs/web-portal/` including target-state `web-portal.md`, `implementation-current.md`, `sdlc-web-portal.md`).
- Cloudflare harness lessons (narrow scope + rich context, parallel tasks + deduplication, adversarial review, structured pipeline stages, observability/feedback loops).
- Slack-inspired channel and member management patterns for low-friction collaboration without visual overwhelm.
- Real-time STOMP/WebSocket architecture already in progress.

**Key Outcomes**:
- Users who know what they want are unblocked immediately (prominent command input on Home).
- The harness (decomposition, parallel narrow work, Court validation) is visible and actionable without being noisy.
- Channels feel like lightweight, scannable collaborative workspaces (Slack-like) with rich inter-agent activity, grouped member management, and embedded governance.
- Strong support for single-agent deep traces and global monitoring.
- Progressive disclosure and persona-aware defaults.
- Metrics live primarily on Dashboard; Home stays focused on productivity.

This is a full redesign direction. The current implementation (localhost:8080/#/channels screenshot and existing portal code) serves as a valuable prototype. We are open to significant evolution of navigation, layouts, and components to achieve the optimal experience.

## Goals & Success Metrics

- **Immediate Productivity**: First-time user completes a meaningful action (start task that triggers PM plan + specialists + Court path) in < 10 minutes with minimal friction.
- **Harness Transparency**: Users can see the decomposition into narrow tasks, parallel agent progress, and adversarial Court rationales without digging.
- **Low Cognitive Load on Channels**: Member lists are grouped/collapsible; activity feeds focus on decisions and work, not noise. (Inspired by Slack channel UX.)
- **Trust & Observability**: Alex-type users feel they have personally reviewed and signed off via clear traces and Court summaries. Lena-type users get exportable structured governance artifacts.
- **Real-time Responsiveness**: Updates feel live without excessive network/compute (STOMP topic subscriptions).
- **Scalable to Teams**: Sam/Jordan can oversee multiple channels/agents and export audit trails easily.

## Design Principles

1. **Radical Simplicity + Clear Mental Model** (from UX principles): Users never need to understand the full microVM/isolation architecture to be effective. The harness makes the "how it works safely" obvious.
2. **Harness Visibility** (Cloudflare lessons): Make narrow-scope decomposition, parallel agent work, adversarial review (Court personas in deliberate disagreement), structured pipeline stages (Plan → Delegate → Execute → Propose → Court Review → Apply), and feedback loops first-class in the UI.
3. **Slack-Inspired Channel UX**: Scannable channel lists, focused member management (grouped by role: Core Court, Project Roles, Humans), low-friction invite/remove, threads/activity focus to avoid overwhelm.
4. **Progressive Trust & Disclosure**: Start conservative; surface more depth (traces, full rationales) as users engage. Persona-aware defaults.
5. **Zero Friction for Common Tasks + Contextual Help**: Prominent command input on Home. Non-intrusive, dismissible suggestions based on recent activity or external signals. Never blocks power users.
6. **Paranoid Transparency without Overwhelm**: Security posture, isolation indicators, and governance status are calm and always visible but not alarming. Full auditability.
7. **Real-time Observability with Efficiency**: STOMP topic subscriptions for targeted updates (per-channel, per-conversation, per-view). Structured outputs where possible.
8. **Metrics on Dashboard, Productivity on Home**: Home is for action and context. Dashboard owns global stats, active work overview, and monitoring.

## User Personas & Journey Segments

**Primary Personas** (from docs/prd/personas.md and user-personas.md):
- **Alex Rivera** (Security-Conscious Solo Hobbyist): Needs instant confidence, easy single-agent traces, plain-English Court rationales, deferral options, and visible "I signed off" feeling.
- **Jordan Hale** (Indie Developer / Small-Team Founder): Needs rapid iteration, exportable investor-grade audit trails from Court, and "simulate full bank review" mode.
- **Sam Chen** (Mid-Market Tech Lead): Needs team oversight, shared governance history, sub-agent role assignment, and velocity without extra governance staff.
- **Dr. Lena Moreau** (Enterprise CISO): Needs policy injection, fleet consistency, structured exports (SBOM, regulatory mapping), and one framework from laptop to production.

**Key Journey Segments Supported**:
- Starting new conversation/task (user-journey 02).
- Collaborative task execution with proactive agent updates and human intervention (user-journey 03).
- Monitoring agent activity, intervening, auditing (user-journey 05).
- Reviewing Court decisions and applying outcomes (user-journey 06).
- Creating/iterating skills with governance (user-journey 04).
- Multi-agent team workflows and inter-agent collaboration.

The redesign ensures every major journey makes the harness (narrow parallel specialists + adversarial Court + observability) feel natural and empowering.

## Information Architecture & Navigation

**Proposed Top-Level Navigation** (refined from current and legacy wireframes):
Home (productive command) | Channels | Dashboard / Monitoring | Court / Governance | Agents | Skills | Audit | Settings

**Persistent Elements**:
- Top header: Logo, system status (Daemon | Firecracker | active agents), Conn SSE indicator, Notifications, Operator dropdown (About Me, Settings, Agent Customization, Profile).
- Right collapsible context panel: Operator / Channel / Security Posture + Harness view teaser.
- Left sidebar: Quick nav + Recent Channels (scannable) + Quick Actions.

**Folder Structure for Docs**:
`docs/PRD/web-portal/` (this document and sub-pages as they are detailed).

## Detailed Page Specifications

### Home (Productive Command Center / Initial View)

**Journey Segments**: Onboarding/first use, starting new task, resuming recent work, lightweight global pulse.

**What Users Need to Do**:
- Express goal in natural language; see PM decomposition into narrow tasks + Court path immediately.
- Receive relevant, non-intrusive next-action suggestions (recent activity, external signals).
- Access optional quick starts when blank-slate.
- Get calm high-level pulse without metrics overload.
- Jump directly to deeper views if they know where they're going.

**How It Functions** (Key Interaction Patterns):
- **Hero Command Bar** (always prominent): Large "What do you want to accomplish?" input. Placeholder examples include harness language. On submit: triggers plan preview (narrow scoped tasks + assigned personas) then transitions to Channels or Canvas.
- **Contextual Suggestions** (below input, collapsible/dismissible): Based on recent channel activity or external signals (e.g., new Zig release → research task). Never blocks the command bar.
- **First-time Helpers** (optional section): Quick-start cards (Research a topic | Start a feature channel | Audit security posture | Propose custom skill).
- **Subtle Live Pulse Row**: Minimal stats (Active Agents by role, Pending Proposals, Background progress) — clickable to Dashboard.
- **Minimal Recent Activity Preview**: 2–3 scannable items with deep links.
- **Left Sidebar**: Quick nav + Recent Channels list (name + member count/active + last activity + status). Quick Actions (New Channel, New Task/Goal, Propose Skill).
- **Right Context Panel**: Your Context, Security Posture (Browser isolated, All stable, No external), Operator toggles (Safe Mode with tooltip, Autonomy level), "Harness view" teaser linking to deeper pipeline visibility.

**Real-time**: SSE/STOMP for active counts, pending proposals, recent activity.
**Edge Cases**: New user → stronger quick-start emphasis. Power user with active work → more contextual "next in #feature-auth" suggestions.
**Persona Considerations**: Alex sees strong Court/trace hints. Jordan/Sam see team-relevant activity. Lena sees posture + policy signals.

**Visual Reference**: See generated mock in conversation history (calm, spacious, command-focused, harness-aware without preachiness).

### Channels (Collaborative Workspaces)

**Journey Segments**: Collaborative task execution (PM creates channel, invites specialists, inter-agent work + proactive updates + proposals), human intervention, reviewing embedded Court decisions, scoped ongoing work.

**What Users Need to Do**:
- Scannable channel list and quick entry.
- Understand current harness state (plan, narrow tasks in progress, Court status).
- Watch focused inter-agent + human activity without noise.
- Manage participants (humans + agent personas) with low friction and clear roles (Slack-style grouped management).
- Intervene (@mention, reply to proactive updates, approve high-privilege).
- Surface/dig into proposals originating here.

**How It Functions**:
- **Left**: Scannable Channels list (name, member count or "X active", last activity, status). Search. Quick actions (New Channel with optional goal prefill, New Team, Propose Skill scoped to channel).
- **Main**:
  - Channel header with description, status, Archive.
  - **Harness / Pipeline Overview** (prominent calm strip or cards): Current plan or active narrow tasks with assigned persona and status. Visible stages (Plan → Delegate → Execute → Propose → Court Review → Apply). Progress per stage/task. Makes Cloudflare-style decomposition and parallel work tangible.
  - **Activity Feed** (core, real-time): Chronological/grouped (human messages, proactive agent updates, tool calls, inter-agent hand-offs, proposal events). Threads for focus. @mention autocomplete for humans + roles/personas. Streaming support. "Proactive update" visual distinction.
  - Quick input bar: Natural language + @mentions. High-privilege actions surface approval prompt.
- **Right Context Panel**:
  - Channel Context (pending proposals with link, recent decisions, shared artifacts).
  - **Grouped Members / Participants** (Slack-inspired focused management): Collapsible sections — Core Court (7 personas with role + last activity/vote + "View trace" link), Project/SDLC Roles (unique PM + specialists, status), Humans/Operators (easy add/remove). Searchable. "Manage members" opens clean modal/pane for invite/remove with role context. No flat duplicate lists.
  - Security Posture + Quick actions (Invite specialist, Propose change, Review pending Court, Open in Canvas).

**Real-time**: Per-channel STOMP topics for activity, member status, proposals.
**Edge Cases**: Empty channel → helpful "Give the PM a goal" or template suggestions. Many members → grouped + collapsed by default.
**Persona Considerations**: Alex gets easy trace links from members and prominent Court events. Teams see clear role visibility and @mention power. Lena sees governance status at channel level.

### Dashboard & Monitoring

**Journey Segments**: Global monitoring of agent activity, intervening (pause/resume/cancel), seeing harness state across work, team oversight.

**What Users Need to Do**:
- At-a-glance view of everything running (agents by role/channel, tasks, proposals).
- Quick drill-down to detail (single-agent trace, channel, Canvas, proposal).
- Intervene on running work.
- See metrics and trends (without polluting Home).
- Filter/search specific work.

**How It Functions**:
- Top: Filterable search + view toggles (All / By Channel / By Persona / Background).
- **Metrics / Pulse Section**: Active Agents (breakdown), Background Tasks (count + avg progress), Pending Proposals (count + urgency). Links to full views.
- **Active Work Lists/Cards**: Grouped/tabbed. Each shows narrow scope/task, assigned persona(s), progress/stage, last update, channel link. Click → Single-Agent Trace or Canvas or Court.
- Live timeline or global activity stream (filterable).
- Controls: Pause/Resume/Cancel with confirmation for high-impact.
- "Watch live" buttons.
- Right/bottom: Aggregate security posture or alerts. Harness health hint.

**Real-time**: Strong SSE/STOMP for stats, task progress, new proposals.
**Edge Cases**: Nothing running → helpful "Start new task from Home" guidance. High activity → excellent filtering/grouping.
**Persona Considerations**: Sam gets team-wide overview. Alex gets easy path to individual traces. Lena gets posture + proposal urgency.

### Court / Governance Hub

**Journey Segments**: Reviewing Court decisions, approvals/interventions, compliance oversight, exporting structured artifacts.

**What Users Need to Do**:
- See pending/recent proposals with status, per-persona votes + short rationales, security gates results.
- Understand why approved/rejected and review diffs/artifacts.
- Act (approve/reject/defer/comment) individually or batch.
- Export structured reports/audit trails (critical for Jordan investor diligence and Lena compliance).
- See full harness validation pipeline for a change.

**How It Functions**:
- Filters: Status, Channel, Persona, Date, Urgency.
- **Proposal List**: Rows/cards with title, originating channel, votes summary, gates status, age. Click → detail.
- **Detail View** (side/modal): Metadata, structured stages reached Court, per-persona vote cards with rationale + ts, diff/links to artifacts, security scan results. Actions: Approve/Reject/Defer (with note)/Comment. Batch actions.
- Export buttons (PDF/structured, SBOM mapping, regulatory). Especially valuable for Jordan and Lena.
- Links back to originating Channel or Canvas.

**Real-time**: Vote and new proposal updates.
**Edge Cases**: No pending → calm "All changes governed and applied" + recent history link. High volume → strong filters.
**Persona Considerations**: Alex sees plain-English rationales and easy defer. Lena sees exports + policy alignment. All see adversarial/multi-perspective value clearly.

### Canvas (Inter-Agent Pipeline View)

**Journey Segments**: Watching multiple agents working in concert on sub-tasks, visual progress on parallel narrow work, team collaboration monitoring.

**What Users Need to Do**:
- See visual or card-based overview of agents working in parallel/sequence.
- Understand dependencies, progress per narrow task/role, shared state/artifacts.
- Drill into single-agent detail or proposal.
- Get harness pipeline visibility at a glance.

**How It Functions** (builds on legacy Canvas + Team Workspace live timeline from specs):
- Grid or cards of active agents/roles with status, progress on their narrow scope, recent tool/thought summary.
- Visual pipeline or timeline showing stages and hand-offs.
- Shared artifacts panel (research notes, diffs, plans).
- Dependency graph or simple flow (text or lightweight viz) between sub-tasks.
- Links to Single-Agent Trace, originating Channel, or pending Court proposal.
- Real-time updates via STOMP `/topic/canvas.events` (or granular).

**Real-time**: High (agent status, tool calls, stage changes).
**Persona Considerations**: Sam/Jordan love the "agents in concert" visibility. Alex can drill quickly to traces.

### Single-Agent Trace / Deep Activity View

**Journey Segments**: Debugging, trust-building (Alex watching every step), detailed monitoring, understanding why an agent made a decision.

**What Users Need to Do**:
- See full ReAct-style timeline (Observe → Think → Plan → Act → Judge) for a specific agent.
- Expand tool calls with sanitized I/O, timings, memory context.
- Understand decisions and link to proposals/Court if applicable.
- Intervene or pause from here if needed.

**How It Functions**:
- Clean timeline or stepper UI.
- Expandable sections per phase with rich details (tool inputs/outputs sanitized, duration, success/failure).
- Memory snippets or context where relevant.
- Links to originating task/plan, channel, or Court proposal.
- Real-time streaming when agent is active.
- Controls: Pause/Resume/Cancel (with context why this matters).
- Audit log link for the agent/session.

**Real-time**: Per-agent updates when active.
**Edge Cases**: Long traces → good collapsing + search/filter within trace.
**Persona Considerations**: Core for Alex's confidence. Useful for all when debugging or auditing.

## Harness Integration (Cloudflare Lessons Embodied)

The redesign makes the existing architectural harness visible and controllable:
- **Narrow Scope + Rich Context**: Command input and task creation surfaces decomposition. Easy attachment of architecture docs, trust boundaries, prior findings.
- **Parallel Narrow Tasks + Deduplication**: Canvas and Channel activity/pipeline views show multiple specialists working in parallel with clear progress and grouping.
- **Adversarial Review**: Court personas in deliberate disagreement with per-persona rationales and votes prominently surfaced (not buried).
- **Structured Pipeline Stages**: Visible Plan → Delegate → Execute → Propose → Court Review → Apply stages in Channels, Canvas, and Court. Structured outputs (plans, proposals, traces) preferred over pure prose.
- **Observability & Feedback Loops**: Full single-agent traces + aggregate harness health. Feedback from Court/validation loops back visibly.
- **Noise Reduction**: Filters, grouping, confidence signals, "proven vs hedged" distinctions, staged validation.

This directly addresses generic agent failure modes (wandering, noise, inconsistent refusals, low coverage) while leveraging AegisClaw's strengths (PM orchestration, microVM isolation, Memory VM traces, mandatory Court).

## Channel & Member Management (Slack Patterns)

- Scannable channel list with name + member count/active + last activity + status.
- Focused member management pane/modal: Grouped by role (Core Court, Project Roles, Humans), searchable, status dots, "View trace", easy invite/remove with role context.
- No overwhelming flat lists of every persona.
- Per-channel notification settings and threads to keep main activity clean.
- Quick "Create channel from goal/template" flow.

## Real-time Architecture & Technical Notes

Builds directly on existing `docs/specs/web-portal/implementation-current.md` (STOMP topic subscriptions, presentation-only isolated VM, vsock bridge, self-contained dark theme, GitHub-inspired aesthetic).

Key evolutions:
- Per-view and per-channel STOMP topics for efficiency (already in progress).
- Stronger emphasis on structured pipeline stages and harness visibility in templates/JS.
- Grouped member components with role-aware rendering.
- Enhanced empty states and progressive disclosure components.
- Data-testid + Playwright coverage for all new interactive elements (command bar, pipeline views, grouped members, Court actions).

Non-responsibilities remain: Portal is strictly presentation-only. All mutations go through validated API + Court where required. No secrets, no external resources.

## Security, Isolation & Compliance

- Same paranoid model as existing specs: isolated Web Portal VM, vsock-mediated actions only, input untrusted, full audit logging.
- Security posture indicators (Browser isolated, Stable selectors, No external resources) calm and persistent.
- Court outputs and traces provide the compliance artifacts Lena and Jordan need.
- Progressive autonomy controls (Safe Mode, sliders) with clear explanations.

## Open Questions & Next Steps

- Confirm exact navigation labels and default landing (Home vs Channels).
- Detail interaction specs for Canvas pipeline visualization (graph vs cards vs timeline).
- Member management modal vs inline pane — user testing preference.
- How external signals (news, stock, etc.) are sourced and opted-in without adding attack surface.
- Prioritize which page to fully wireframe/mock next (Channels with harness pipeline + grouped members is high impact).
- Keep `docs/specs/web-portal/` target-state specs aligned with this PRD; `implementation-current.md` tracks what ships today.
- E2E test coverage expansion for new flows (command bar → plan preview → channel with visible pipeline).

## Related Documents

- `docs/prd/personas.md`, `docs/prd/user-personas.md`, `docs/prd/user-experience-principles.md`, `docs/prd/collaboration-model.md`
- `docs/specs/user-journeys/` (especially 02, 03, 05, 06)
- `docs/specs/web-portal/` (target-state modular specs — supersedes legacy `web-portal-screens.md`)
- `docs/specs/web-portal/sdlc-web-portal.md` (SDLC visibility: proposal → Court → build → PR → deploy)
- `docs/specs/web-portal/implementation-current.md` (implementation-current snapshot)
- `docs/specs/web-portal/web-portal-vm.md`
- Cloudflare harness post (https://blog.cloudflare.com/cyber-frontier-models/#what-a-harness-actually-fixes) — key inspiration for UI visibility of narrow/adversarial/parallel patterns.

**This PRD establishes the vision and functional foundation. Detailed component specs, wireframes, and implementation tasks will follow in subsequent documents or issues.**

---

*Generated and committed via AegisClaw development workflow with GitHub connector assistance.*
