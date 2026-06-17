# Member Management Flow Specification

**Status**: Target State

## Overview

Defines frontend patterns and backend requirements for managing channel participants (humans and agent personas).

## Member Categories

1. **Core Court** (7 fixed governance personas)
2. **Project / SDLC Roles** (dynamic specialists)
3. **Humans / Operators**

## Frontend Patterns

- Grouped, collapsible sections in the context panel
- Searchable
- "View Trace" for agents
- Quick remove with confirmation

## Invite Flow

- Separate paths for humans and specialist agents
- PM can suggest appropriate specialists based on current plan
- Court personas require elevated confirmation or governance step

## Remove Flow

- Confirmation explaining impact
- Court personas have restricted removal (requires higher privileges or governance)

## Backend Requirements

- Host enforces permission checks
- Agent personas are spawned by PM when delegated tasks
- All changes are auditable

## Permission Model (Target)

| Action                    | Humans          | Project Specialists | Core Court Personas      |
|---------------------------|-----------------|---------------------|--------------------------|
| Invite to channel         | Yes             | Yes (via PM)        | Restricted / Governance  |
| Remove from channel       | Yes             | Yes                 | Restricted / Governance  |
| View trace                | Yes             | Yes                 | Yes                      |
| Trigger high-privilege actions | Limited    | Limited             | Governed by Court rules  |

Court personas are generally pre-seeded or managed at a higher level. Adding or removing them typically requires explicit governance approval or elevated operator privileges.

## Security Considerations

- All membership changes go through the Host
- Powerful personas require additional safeguards
- Clean termination of agent microVMs on removal

This model supports the collaboration model while maintaining security boundaries.