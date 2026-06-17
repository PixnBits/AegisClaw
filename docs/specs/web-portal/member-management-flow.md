# Member Management Flow Specification

**Status**: Target State

## Overview

This document defines the requirements for managing participants in Channels (humans and agent personas). It covers both the frontend interaction patterns and the backend support needed while maintaining paranoid security and clear role separation.

## Goals

- Provide low-friction, grouped member management (inspired by Slack but adapted to agent personas).
- Maintain clear distinction between human operators and specialized agent personas.
- Ensure all membership changes are auditable and respect governance where appropriate.
- Support the collaboration model (PM orchestration + Court personas + project specialists).

## Member Categories

Members in a channel are grouped into three primary categories:

1. **Core Court** — The seven fixed governance personas (CISO, Security Architect, Architect, Senior Coder, Tester, Efficiency, User Advocate).
2. **Project / SDLC Roles** — Dynamic specialists invited by the PM or user (e.g., researcher, coder, tester for a specific feature).
3. **Humans / Operators** — Real users who can participate in the channel.

## Frontend Interaction Patterns

### Grouped Member List (in Channel Context Panel)

- Collapsible sections for each category.
- Each member entry shows: role/persona name, status (active/idle), last activity or vote (for Court personas), and actions.
- Search across all groups.
- "View Trace" available for agent personas.
- Quick remove action (with confirmation) for removable members.

### Invite Flow

- Prominent "Invite" action in the member management pane.
- Two main paths:
  - Invite Human (search existing users or add by identifier).
  - Invite Specialist Agent (select from available personas or let PM suggest based on current plan).
- For high-privilege agent personas (especially Court roles), additional confirmation or governance step may be required.
- Clear feedback on success and real-time update of the member list.

### Remove Flow

- Remove action available on eligible members.
- Confirmation dialog explaining impact (e.g., "This agent will be terminated and removed from the channel").
- For Court personas, removal may be restricted or require higher privileges.

## Backend Requirements

- The Host must support adding and removing both human users and agent persona instances to/from channels.
- Agent personas are typically spawned on-demand by the PM when delegated a narrow task. They may be added to the channel automatically or explicitly.
- Membership changes should generate auditable events.
- Permission checks must be enforced on the Host side (the portal only renders the UI and sends requests).
- Court personas should have special handling — they are often pre-seeded or managed at a higher governance level.

## Security Considerations

- Only authorized users can invite or remove members.
- Adding powerful agent personas (especially those with broad capabilities) should trigger appropriate governance or confirmation.
- Removal of active agents should cleanly terminate the microVM and clean up state.
- All membership changes are logged for audit purposes.
- The portal must never directly control agent lifecycle — it only requests actions via the bridge.

## Open Areas

- Exact permission model for who can invite which types of members.
- Whether Court personas can be dynamically added/removed per channel or are globally configured.
- How long-running specialist agents are cleaned up when no longer needed.
- UI patterns for bulk invite or role suggestion based on current plan.

This flow supports the collaboration model while keeping member management focused and secure.