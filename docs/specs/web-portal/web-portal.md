# Web Portal Specification

**Status**: Target State  
**Owner**: AegisClaw Team  

## Overview

The AegisClaw Web Portal is the primary rich, real-time web interface for users, reviewers, and operators. It provides visibility, interaction, and control across collaboration, governance, agent activity, and system state.

The portal runs in a dedicated isolated Web Portal VM and communicates exclusively through a trusted vsock API bridge to the Host Daemon. It is strictly presentation-only: no business logic, no persistent state, and no secrets reside in the portal.

The design makes the system's core harness visible and actionable:
- PM-orchestrated decomposition of goals into narrow-scope tasks for specialist agents.
- Parallel execution of those tasks across isolated microVMs.
- Adversarial multi-persona review through the Court.
- Structured pipeline stages with full observability and feedback loops.

This creates a calm, immediately productive command center that feels both powerful and trustworthy.

## Design Principles

1. **Immediate Productivity** — Users who know what they want can act instantly via a prominent command surface. Others receive gentle, contextual, dismissible guidance.
2. **Harness Visibility** — The decomposition into narrow tasks, parallel agent work, adversarial Court review, and pipeline stages are first-class, scannable elements in the interface.
3. **Low-Friction Collaboration** — Channels function like lightweight, scannable workspaces. Member management is focused and grouped by role. Activity feeds prioritize decisions and progress over noise.
4. **Progressive Trust & Disclosure** — The interface starts conservative and reveals depth (traces, rationales, controls) as users engage. Defaults are persona-aware.
5. **Clear Mental Model** — Users always understand what agents are doing, what capabilities exist, and what a new action requires in terms of governance.
6. **Paranoid Transparency without Overwhelm** — Security posture, isolation guarantees, and governance status are calm, persistent, and non-alarming.
7. **Efficient Real-Time Observability** — Targeted updates via STOMP topic subscriptions deliver responsiveness without excessive network or compute cost.
8. **Metrics Belong on the Dashboard** — The primary Home surface stays focused on action and relevant context. Global metrics and monitoring live on the Dashboard.

## Personas & Supported Journeys

The portal supports the following primary personas:

- **Alex Rivera** (Security-Conscious Solo Hobbyist) — Requires strong visibility into single-agent traces, plain-English Court rationales, easy deferral, and the feeling of personal sign-off on governance decisions.
- **Jordan Hale** (Indie Developer / Small-Team Founder) — Needs rapid iteration with exportable, investor-grade audit trails from the Court and the ability to simulate stricter review modes.
- **Sam Chen** (Mid-Market Tech Lead) — Needs team-wide oversight of multiple channels and agents, shared governance history, and velocity without requiring extra governance staff.
- **Dr. Lena Moreau** (Enterprise CISO) — Needs policy injection, consistent governance across instances, structured exports (SBOM, regulatory mapping), and a single framework that scales from laptop to production.

Key journeys supported:
- Starting a new task or conversation.
- Collaborative task execution with proactive agent updates and human intervention.
- Monitoring agent activity with drill-down and intervention.
- Reviewing and acting on Court decisions.
- Creating and iterating on skills under governance.
- Multi-agent team workflows.

## Information Architecture

**Primary Navigation**:
Home | Channels | Dashboard | Court | Agents | Skills | Audit | Settings

**Persistent Elements**:
- Top header with logo, system status, connection indicator, notifications, and operator menu.
- Collapsible right context panel (Operator / Channel / Security Posture + Harness overview).
- Left sidebar with quick navigation, recent channels, and quick actions.

## Home (Productive Command Center)

Home is the primary entry point and productivity surface.

**Purpose**:
Allow users who know their intent to act immediately while providing relevant, non-intrusive context and suggestions for others.

**Key Elements**:
- **Prominent Command Bar** — Large, always-visible input for natural language goals. Subtle helper text explains that the PM will decompose the goal into narrow tasks for specialists with Court review. Submission triggers a plan preview then transitions to the relevant Channel or Canvas.
- **Contextual Suggestions** — Dismissible or collapsible cards showing relevant next actions based on recent channel activity or opted-in external signals. Never obstruct the command bar.
- **Quick Starts** (for blank slate or first-time users) — Optional, helpful entry points such as "Research a topic", "Start a feature channel", "Audit security posture", or "Propose a custom skill".
- **Subtle Live Pulse** — Minimal, non-dominant row showing active agents (by role), pending proposals, and background task progress. Clickable to Dashboard.
- **Recent Activity Preview** — Small number of scannable items with direct links to Channels, traces, or proposals.

**Right Context Panel**:
Security posture summary, operator controls (Safe Mode, autonomy level), and a "Harness view" teaser that links to deeper pipeline visibility.

**Behavior**:
Real-time updates for active counts and recent activity via STOMP. New users see stronger quick-start emphasis; experienced users with active work see more contextual suggestions.

## Channels (Collaborative Workspaces)

Channels are the primary collaborative workspaces where humans and agents work together on scoped goals.

**Purpose**:
Provide a focused, low-noise environment for inter-agent collaboration, human intervention, and embedded governance.

**Layout**:
Three-zone structure:
- Left: Scannable list of channels (name, active member count, last activity, status) with search and quick actions (New Channel, New Team, Propose Skill).
- Main: Channel header, harness/pipeline overview, activity feed, and quick input.
- Right: Channel context, grouped member management, security posture, and contextual actions.

**Harness / Pipeline Overview**:
A calm, prominent strip or set of cards showing the current plan decomposed into narrow tasks, assigned specialist personas, and progress. Visible stages (Plan → Delegate → Execute → Propose → Court Review → Apply) with status per stage. This makes the parallel narrow work and governance path immediately understandable.

**Activity Feed**:
Real-time feed of human messages, proactive agent updates, tool calls, inter-agent hand-offs, and proposal events. Threads keep focused discussion clean. @mention autocomplete supports both humans and agent roles/personas. Proactive updates are visually distinct. Streaming support for live agent output.

**Quick Input**:
Natural language input with @mention support. High-privilege actions surface clear approval prompts.

**Grouped Member Management**:
Focused, searchable pane or modal with collapsible sections:
- Core Court (the seven personas with role, last activity/vote, and "View trace" links).
- Project / SDLC Roles (unique PM and specialists with status).
- Humans / Operators (current user and collaborators with easy add/remove).

No flat, overwhelming lists of every persona. Management actions (invite, remove, view trace) are role-aware and low-friction.

**Real-time**:
Per-channel STOMP topics for activity, member status changes, and proposal updates.

## Dashboard & Monitoring

The Dashboard provides global visibility and control over active work.

**Purpose**:
Give operators and leads an at-a-glance view of everything running across channels, with quick drill-down and intervention capabilities.

**Key Sections**:
- Filterable search and view toggles (All, By Channel, By Persona, Background only).
- Metrics row: Active Agents (with role breakdown), Background Tasks (count + average progress), Pending Proposals (count + urgency).
- Active Work lists or cards: Each item shows narrow scope/task, assigned persona(s), current stage/progress, last update, and originating channel. Clicking drills into Single-Agent Trace, Canvas, or Court detail.
- Live global activity stream (filterable).
- Intervention controls (Pause / Resume / Cancel) with appropriate confirmation for high-impact actions.
- Harness health indicators and aggregate security posture.

**Real-time**:
Strong use of STOMP subscriptions for stats, task progress, and new proposals.

## Court / Governance Hub

Court is the dedicated surface for reviewing and acting on governance decisions.

**Purpose**:
Make adversarial multi-persona review transparent, actionable, and exportable.

**Features**:
- Filterable list of proposals (status, originating channel, vote summary, security gate results, age).
- Detail view showing structured pipeline stages reached, per-persona vote cards with short rationales and timestamps, diffs/artifacts, and security scan results.
- Clear actions: Approve, Reject, Defer (with optional note), Comment. Batch actions supported where appropriate.
- Prominent export options for structured reports, SBOMs, and regulatory mappings (critical for compliance and diligence needs).
- Direct links back to the originating Channel or Canvas.

**Real-time**:
Live updates when votes arrive or new proposals appear.

## Canvas (Inter-Agent Pipeline View)

Canvas provides a visual workspace for watching multiple agents collaborate on a goal.

**Purpose**:
Make parallel narrow-scope work and hand-offs visible and understandable at a glance.

**Features**:
- Grid or card layout of active agents/roles showing their assigned narrow task, status, and progress.
- Visual pipeline or timeline representation of stages and dependencies.
- Shared artifacts panel (research notes, diffs, plans).
- Easy drill-down to Single-Agent Trace, originating Channel, or pending Court proposal.
- Real-time updates via dedicated STOMP topics.

## Single-Agent Trace View

The trace view gives deep, actionable visibility into an individual agent's reasoning and actions.

**Purpose**:
Support debugging, trust-building, and detailed understanding of agent behavior.

**Features**:
- Clean ReAct-style timeline (Observe → Think → Plan → Act → Judge).
- Expandable details per phase including sanitized tool inputs/outputs, duration, success/failure, and relevant memory context.
- Links to originating task/plan, channel, and any related Court proposal.
- Real-time streaming when the agent is active.
- Intervention controls (Pause / Resume / Cancel) with context.
- Direct link to full audit log for the session.

**Behavior**:
Long traces support good collapsing, search, and filtering. This view is especially valuable for building confidence in agent actions and for detailed auditing.

## Real-Time Architecture

The portal uses targeted STOMP-over-WebSocket topic subscriptions for efficient, view-specific updates. This replaces indiscriminate global SSE bundles for most screens while maintaining graceful fallback.

Key topics include:
- Per-conversation updates for Chat.
- Per-channel activity and proposal updates.
- Canvas events.
- Overview / Dashboard statistics.
- Approvals / Court pending.

Subscriptions are managed on view mount/unmount. The implementation remains minimal, self-contained, and consistent with the paranoid security model (frame validation, size/rate limits, no secret exposure).

## Security & Isolation Model

The Web Portal VM is strictly isolated. All actions are mediated through the vsock bridge. The browser has no direct access to secrets, external resources, or unvalidated actions. Security posture indicators ("Browser isolated", "Stable selectors", "No external resources") are calm and persistent. All governance-relevant changes flow through the Court with full auditability.

## Open Areas for Further Specification

- Exact component library and design tokens.
- Detailed payload shapes for all STOMP topics.
- Interaction states and loading behaviors for the harness pipeline visualization.
- Member management modal vs inline pane patterns.
- Export formats and compliance mapping details.
- Full E2E test coverage matrix for the new flows.

These areas will be expanded in focused follow-on specification documents within this folder.

## Related Documents

- `docs/prd/personas.md` and `docs/prd/user-personas.md`
- `docs/prd/collaboration-model.md`
- `docs/specs/user-journeys/` (particularly collaborative task execution, monitoring, and Court review journeys)
- `docs/specs/web-portal-vm.md`
- `docs/specs/event-system.md`

This specification defines the target experience. Implementation should align to these behaviors and interaction patterns.