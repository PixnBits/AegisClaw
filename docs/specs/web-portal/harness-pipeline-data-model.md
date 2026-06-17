# Harness Pipeline Data Model Specification

**Status**: Target State

## Overview

This document defines how the harness (PM-orchestrated narrow tasks, parallel execution, and structured pipeline stages) is represented in data and surfaced in the Web Portal. A clear, consistent data model is essential for making the harness visible and understandable across Home, Channels, Canvas, Dashboard, and Court.

## Core Concepts

- **Goal / Plan**: The high-level intent provided by the user (or derived by the PM).
- **Narrow Task**: A decomposed, scoped unit of work assigned to a specific specialist agent persona.
- **Pipeline Stage**: One of: Plan → Delegate → Execute → Propose → Court Review → Apply.
- **Agent Instance**: A running microVM executing a narrow task.
- **Progress**: Status of a task or stage.

## Data Model (Target)

### Plan / Goal
```json
{
  "plan_id": "plan_abc123",
  "channel_id": "chan_main",
  "goal": "Research and compare Zig vs Rust for home lab scripts",
  "created_at": "2026-06-17T...",
  "status": "active",
  "stages": [
    {"name": "Plan", "status": "completed"},
    {"name": "Delegate", "status": "completed"},
    {"name": "Execute", "status": "in_progress"},
    {"name": "Propose", "status": "pending"},
    {"name": "Court Review", "status": "pending"},
    {"name": "Apply", "status": "pending"}
  ]
}
```

### Narrow Task
```json
{
  "task_id": "task_xyz789",
  "plan_id": "plan_abc123",
  "agent_persona": "researcher",
  "scope": "Research Zig language features with focus on security model",
  "status": "active",
  "current_stage": "Execute",
  "progress": 65,
  "last_update": "2026-06-17T...",
  "agent_instance_id": "agent_vm_456"   // Internal only - do not expose to browser
}
```

## Event Types & Payloads (Real-time Updates)

The following events are pushed via STOMP (see `real-time-contracts.md`) to keep the UI in sync:

### `harness.plan.created`
```json
{
  "type": "harness.plan.created",
  "plan_id": "plan_abc123",
  "channel_id": "chan_main",
  "goal": "...",
  "stages": [...]
}
```

### `harness.task.assigned`
```json
{
  "type": "harness.task.assigned",
  "task_id": "task_xyz789",
  "plan_id": "plan_abc123",
  "agent_persona": "researcher",
  "scope": "...",
  "current_stage": "Execute"
}
```

### `harness.task.progress`
```json
{
  "type": "harness.task.progress",
  "task_id": "task_xyz789",
  "progress": 65,
  "current_stage": "Execute",
  "summary": "Found 12 relevant papers on Zig security model"
}
```

### `harness.stage.transition`
```json
{
  "type": "harness.stage.transition",
  "plan_id": "plan_abc123",
  "stage": "Propose",
  "status": "in_progress",
  "related_task_ids": ["task_xyz789"]
}
```

### `harness.proposal.created`
```json
{
  "type": "harness.proposal.created",
  "plan_id": "plan_abc123",
  "task_id": "task_xyz789",
  "proposal_id": "prop_def456",
  "stage": "Court Review"
}
```

**Important**: The portal should treat `agent_instance_id` as internal and never display or log it.

## Visibility in UI Surfaces

- **Channels**: Shows current plan + active narrow tasks + stage progress strip.
- **Canvas**: Visual cards for tasks + pipeline stages.
- **Dashboard**: Aggregated active tasks filterable by stage/persona.
- **Court**: Proposals linked back to originating plan/task.

## Security Considerations

- Never expose `agent_instance_id` or raw internal identifiers to the browser.
- Scope descriptions should be sanitized.
- Access to plan/task data must respect channel membership.

## Implementation Notes

- Use shared types between Host and Portal where possible.
- Prefer delta updates over full snapshots for real-time efficiency.
- The portal must treat harness state as read-only.