# Dashboard & Monitoring Specification

**Status**: Target State

## Overview

The Dashboard provides a global, at-a-glance view of all active work across the system, along with monitoring, intervention, and high-level metrics. It is the primary surface for operators and leads who need to understand system-wide state without being inside a specific channel.

It complements Home (which focuses on immediate productivity and contextual suggestions) by owning global metrics and active work oversight.

## Goals

- Deliver clear visibility into running agents, background tasks, and pending governance work.
- Enable quick drill-down into detailed views (Single-Agent Trace, Canvas, Court, Channel).
- Support intervention (pause, resume, cancel) with appropriate safeguards.
- Show the health of the harness (parallel narrow tasks, adversarial review activity) at a system level.
- Keep metrics and monitoring scoped here so Home remains focused on action.

## Layout

- Top: Search + view filters (All, By Channel, By Persona/Role, Background Tasks only, Pending Proposals only).
- Prominent but calm metrics row.
- Main area: Grouped or tabbed lists/cards of active work.
- Optional right or bottom panel: Aggregate security posture, harness health indicators, and recent global activity.

### Metrics Row

- Active Agents (total + breakdown by role or channel)
- Background Tasks (count + average progress)
- Pending Proposals (count + oldest or highest urgency)
- Optional: Running MicroVMs, resource usage summary (if relevant and not noisy)

These are live-updating and clickable where appropriate to filter the main lists.

### Active Work Views

Main content area shows active agents, tasks, and proposals in card or list form. Each item includes:
- Narrow scope or task description
- Assigned persona(s) or role
- Current stage / progress indicator
- Last update timestamp
- Originating channel (with link)
- Quick actions (Pause, Watch in Canvas, View Trace, Review Proposal)

Grouping options:
- By Channel
- By Persona / Role
- By Stage (Plan, Execute, Court Review, etc.)
- Flat with strong filtering

### Intervention Controls

Direct controls for running work:
- Pause / Resume / Cancel on agents or background tasks
- Confirmation dialogs for high-impact actions
- Clear explanation of consequences (e.g., "This will stop the current narrow task and notify the PM")

"Watch live" or "Open in Canvas" buttons for visual monitoring of multi-agent work.

## Real-Time Behavior

Heavy use of STOMP topic subscriptions for:
- Live metric updates
- New or updated active work items
- Proposal status changes
- Agent state transitions

Subscriptions are view-specific and cleaned up on navigation away.

## Edge Cases & Empty States

- No active work: Helpful guidance linking back to Home command bar or quick-start templates.
- Very high activity: Strong default filters and grouping to prevent overload.
- Long-running background tasks: Clear progress indicators and estimated completion where available.

## Persona Considerations

- **Sam Chen** (Tech Lead): Team-wide oversight, easy filtering by channel or role, quick intervention.
- **Alex Rivera**: Easy path from global view to individual agent traces.
- **Dr. Lena Moreau**: Visibility into pending governance work and security posture at scale.

## Open Areas

- Exact visual design of the metrics row and work cards.
- Default filter and grouping preferences.
- How resource usage (if shown) is presented without creating noise.

This specification defines the target Dashboard & Monitoring experience.