# Harness Pipeline Data Model Specification

**Status**: Target State

## Overview

This document defines how the harness (PM-orchestrated narrow tasks, parallel execution, and structured pipeline stages) is represented in data and surfaced in the Web Portal. A clear, consistent data model is essential for making the harness visible and understandable across Home, Channels, Canvas, Dashboard, and Court.

## Core Concepts

- **Goal / Plan**: The high-level intent provided by the user (or derived by the PM).
- **Narrow Task**: A decomposed, scoped unit of work assigned to a specific specialist agent persona (e.g., "Research Zig language features with security model focus").
- **Pipeline Stage**: One of the defined stages in the structured flow: Plan → Delegate → Execute → Propose → Court Review → Apply.
- **Agent Instance**: A running microVM agent executing a narrow task.
- **Progress**: Quantitative or qualitative status of a narrow task or stage.

## Data Model (Target)

### Plan / Goal
```json
{
  "plan_id": "...",
  "channel_id": "...",
  "goal": "Research and compare Zig vs Rust for home lab scripts",
  "created_at": "...",
  "status": "active",
  "stages": [
    { "name": "Plan", "status": "completed" },
    { "name": "Delegate", "status": "completed" },
    { "name": "Execute", "status": "in_progress" },
    ...
  ]
}
```

### Narrow Task
```json
{
  "task_id": "...",
  "plan_id": "...",
  "agent_persona": "researcher",
  "scope": "Research Zig language features with focus on security model and performance",
  "status": "active",
  "current_stage": "Execute",
  "progress": 65,
  "last_update": "...",
  "agent_instance_id": "..."
}
```

### Pipeline Stage Status
Each stage should carry:
- Status (pending, in_progress, completed, blocked, failed)
- Optional summary or key output
- Link to related artifacts or proposals (especially for Court Review stage)

## Update Mechanisms

- The PM is the primary source of truth for plan decomposition and task assignment.
- Progress and stage transitions are pushed via real-time events (STOMP topics defined in `real-time-contracts.md`).
- The portal observes these events and updates the UI without polling.
- When a narrow task produces a proposal, the task links to the proposal and the stage advances to Court Review.

## Visibility in UI Surfaces

- **Channels**: Harness / Pipeline Overview section shows current plan, active narrow tasks, and stage progress.
- **Canvas**: Visual representation of tasks in parallel, with stage indicators and dependencies.
- **Dashboard**: Aggregated view of active tasks across channels, filterable by stage or persona.
- **Court**: Proposals are linked back to the originating plan and narrow task(s) that triggered them.

## Security Considerations

- The data model must not expose internal microVM identifiers, raw memory contents, or sensitive planning details to the browser.
- Only user-relevant scope descriptions and progress are shown.
- Links between tasks and proposals must respect access control (channel membership).

## Implementation Notes

- The data model should be defined in shared types (possibly in a common package) so both the Host and Portal can use consistent structures.
- Real-time updates should carry deltas where possible to reduce payload size.
- The portal should treat the harness state as read-only; mutations go through the PM via the bridge.

This model makes the narrow-scope + parallel + adversarial review nature of the harness first-class and observable in the UI.