# Canvas (Inter-Agent Pipeline View) Specification

**Status**: Target State

## Overview

Canvas provides a visual, high-level workspace for watching multiple specialized agents collaborate on a goal in parallel. It makes the harness (narrow-scope task decomposition, parallel execution, and stage progression) visible and understandable at a glance.

It complements the Channels activity feed (which is more message-oriented) by offering a structured, pipeline-oriented view of inter-agent work.

## Goals

- Show parallel narrow tasks and their progress in context of the overall plan.
- Make hand-offs, dependencies, and stage progression visible.
- Provide quick drill-down to Single-Agent Trace, originating Channel, or Court proposals.
- Support monitoring of multi-agent collaboration without requiring the user to read every message.
- Reinforce the harness principles (narrow scope + parallel work + structured stages).

## Layout

- Top: Context header showing the current goal/plan and originating channel (with link).
- Main area: Visual representation of active agents/tasks and pipeline stages.
- Side or bottom panels: Shared artifacts, dependency overview, and quick actions.

### Agent / Task Cards or Grid

Each active agent or narrow task is represented with:
- Assigned persona / role
- Current narrow scope or sub-task
- Progress indicator and current stage
- Status (active, waiting, completed, blocked)
- Recent key action or output summary
- Quick links to trace or channel

Cards can be arranged in a grid, kanban-style columns by stage, or a lightweight flow visualization.

### Pipeline / Stage Visualization

A clear representation of the overall pipeline stages (Plan → Delegate → Execute → Propose → Court Review → Apply) with status per stage and connections between parallel tasks where relevant.

This makes the structured, validated flow of work immediately apparent.

### Shared Artifacts Panel

- Research notes, diffs, plans, or other artifacts produced by the agents.
- Links to relevant proposals or traces.

## Real-Time Behavior

Live updates via STOMP topics for:
- Agent/task status and progress changes
- New narrow tasks being delegated
- Stage transitions
- New artifacts or proposals

The view stays current as agents work in parallel.

## Interaction Patterns

- Clicking an agent card or task opens the Single-Agent Trace view.
- Clicking a stage or proposal link opens the relevant Court detail.
- "Open in Channel" returns to the full activity feed context.
- Ability to pause/resume individual narrow tasks or the overall work from this view.

## Edge Cases

- Few active agents: Graceful empty or minimal state with link back to the originating channel.
- Many parallel tasks: Good grouping, filtering, and visual hierarchy to avoid overload.
- Long-running work: Clear progress indicators and time context.

## Persona Considerations

- **Sam Chen / Jordan Hale**: Excellent for monitoring team or feature work with multiple agents collaborating.
- **Alex Rivera**: Quick path from visual overview to deep individual traces.

## Open Areas

- Preferred visualization style (cards + pipeline strip vs. interactive graph vs. kanban).
- Level of detail shown on cards by default.

This specification defines the target Canvas experience.