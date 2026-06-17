# Single-Agent Trace View Specification

**Status**: Target State

## Overview

The Single-Agent Trace view provides deep, structured visibility into an individual agent’s reasoning and actions. It is a core observability surface that supports debugging, trust-building, auditing, and understanding why specific decisions were made.

It directly supports the need for clear mental models and progressive trust by making the agent’s ReAct-style loop (Observe → Think → Plan → Act → Judge) transparent and explorable.

## Goals

- Show the full sequence of an agent’s reasoning and tool use in a clean, navigable timeline.
- Allow expansion of tool calls with sanitized inputs, outputs, and results.
- Link reasoning steps to broader context (plan, channel, Court proposals).
- Support real-time streaming when an agent is actively working.
- Enable intervention from within the trace view when needed.

## Layout

- Header: Agent identity (persona/role), current task/narrow scope, originating channel or plan (with links), and overall status.
- Main area: Chronological or phase-grouped timeline of the agent’s execution.
- Side panel: Context summary, memory snippets, and quick actions.

### Timeline / ReAct View

A clean, expandable timeline showing phases such as:
- Observe
- Think
- Plan
- Act
- Judge

Each phase entry includes:
- Timestamp and duration
- Key output or decision summary
- Expandable details (especially for Act phases showing tool calls)

**Tool Call Details** (when expanded):
- Tool name and purpose
- Sanitized inputs
- Sanitized outputs or results
- Duration and success/failure status
- Links to related artifacts or logs

## Real-Time Behavior

When the agent is active, the trace view streams new phases and tool results in real time via STOMP. The timeline updates live without requiring manual refresh.

## Interaction Patterns

- Expand/collapse individual phases or tool calls.
- Search or filter within the trace (by phase, tool, keyword).
- Direct links to:
  - Originating channel and activity
  - Related Court proposal (if any)
  - Full audit log for the session
- Intervention controls (Pause, Resume, Cancel) with appropriate context and confirmation.

## Edge Cases

- Very long traces: Strong collapsing, search, filtering, and virtual scrolling.
- Agent idle or completed: Clear final state and links to outcomes (proposal, artifact, etc.).
- Error states: Prominent but calm error display with relevant context for debugging.

## Persona Considerations

- **Alex Rivera**: Primary surface for building confidence through full transparency of agent reasoning and actions.
- All personas benefit when debugging unexpected behavior or auditing specific decisions.

## Open Areas

- Exact visual treatment of the timeline (vertical list vs. horizontal stepper vs. grouped phases).
- How memory context is shown without overwhelming the view.
- Sanitization rules and display format for tool I/O.

This specification defines the target Single-Agent Trace experience.