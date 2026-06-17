# Channels Specification

**Status**: Target State

## Overview

Channels are the primary collaborative workspaces in the AegisClaw Web Portal. They serve as scoped, persistent environments where humans and specialized agents collaborate on goals under governance.

Each channel provides:
- A shared activity feed for human messages and agent updates.
- Visible decomposition of work into narrow tasks assigned to specific agent personas (the harness in action).
- Grouped, low-friction member management.
- Embedded governance through Court proposals that arise naturally from the work.
- Real-time updates and intervention capabilities.

Channels follow Slack-inspired patterns for scannability and low cognitive load while making the multi-agent harness and adversarial review process transparent.

## Goals

- Enable focused collaboration without visual or informational overwhelm.
- Make the PM-orchestrated narrow-task decomposition and parallel execution visible and understandable.
- Support natural human intervention via @mentions and quick input.
- Surface governance events (proposals, votes, rationales) inline where relevant.
- Provide clear paths to deeper views (single-agent traces, Canvas, Court detail).

## Layout & Structure

Channels use a three-zone desktop layout:

- **Left sidebar**: Scannable channel list + quick actions.
- **Main content area**: Channel header, harness/pipeline overview, activity feed, and input.
- **Right context panel**: Channel-specific context, grouped member management, security posture, and quick actions.

The layout is responsive and supports collapsing the right panel on smaller viewports or when focus is needed on the feed.

### Left Sidebar

- List of channels with name, member count or "X active", last activity timestamp, and status indicator (e.g., "Governance active", "Background work").
- Search and filter capability.
- Quick actions: New Channel (with optional goal prefill), New Team, Propose Skill (scoped to current context).

### Main Area

**Channel Header**
- Channel name and short description.
- Status badges.
- Archive button (with confirmation).

**Harness / Pipeline Overview**
A prominent but calm section (strip or card group) that shows:
- The current high-level plan or goal.
- Decomposition into narrow-scope tasks with assigned specialist personas.
- Progress and status per task/stage.
- Visible pipeline stages: Plan → Delegate → Execute → Propose → Court Review → Apply.

This section makes the Cloudflare-inspired harness principles (narrow scope, parallel work, adversarial review) immediately visible without requiring the user to open separate views.

**Activity Feed**
The core of the channel. Real-time feed containing:
- Human messages.
- Proactive agent updates and status reports.
- Tool calls and results (sanitized).
- Inter-agent hand-offs and collaboration events.
- Proposal and Court decision events (with vote summaries and links).

Features:
- Threading for focused discussion.
- @mention autocomplete for both humans and agent roles/personas (project-manager, ciso-persona, researcher, etc.).
- Visual distinction for proactive/agent-generated updates.
- Streaming support for live agent output (Markdown deltas, thought logs, tool logs).
- Search and filter within the feed (by participant, type, date).

**Quick Input Bar**
Natural language input at the bottom of the feed.
- Supports @mentions.
- High-privilege or high-risk actions trigger clear approval prompts before execution.
- Send action routes through the PM or relevant agent as appropriate.

### Right Context Panel

**Channel Context**
- Pending proposals count with direct link to Court.
- Recent decisions summary.
- Shared artifacts or memory context relevant to the channel.

**Grouped Member Management**
Focused, searchable interface with collapsible sections:

- **Core Court** (7 personas): Role, last activity or vote, "View trace" link.
- **Project / SDLC Roles**: Unique project-manager and specialists with current status.
- **Humans / Operators**: Current user and other human participants with easy add/remove.

Management actions (invite human or specialist agent, remove, view profile/trace) are available inline or via a clean modal. No flat, overwhelming lists of every persona by default.

**Security Posture**
Channel or context-specific security indicators (consistent with global posture but scoped).

**Quick Actions**
Contextual buttons such as:
- Invite specialist
- Propose change to this channel
- Review pending Court decisions
- Open in Canvas

## Real-Time Behavior

- Per-channel STOMP topics for activity feed updates, member status changes, and proposal events.
- Subscription managed on channel view mount/unmount.
- Graceful fallback to SSE if STOMP is unavailable.
- Live indicators for active agents/personas in the member section.

## Interaction Patterns & Edge Cases

- **Empty or new channel**: Helpful guidance such as "Give the PM a goal to get started" or template suggestions that trigger plan creation.
- **High volume of activity**: Good default filtering (e.g., show decisions + proactive updates first) and threading.
- **Many members**: Sections are collapsed by default; search works across all groups.
- **Proposal arising from work**: Appears inline in the activity feed with clear status and link to full Court detail.
- **Agent spin-up time**: Graceful loading states that mask microVM startup while showing the harness is working.

## Persona Considerations

- **Alex Rivera**: Easy access to traces from the member list and prominent Court rationales in the feed.
- **Jordan Hale / Sam Chen**: Clear role visibility, @mention power, and export paths from proposals.
- **Dr. Lena Moreau**: Governance status and proposal visibility at the channel level.

## Open Areas

- Exact visual treatment of the pipeline overview (cards vs horizontal strip vs progress indicators).
- Member invite flow details (modal vs drawer).
- Thread vs flat feed preference (user testing).
- Performance targets for very long activity histories.

This specification defines the target behavior for Channels. Implementation should prioritize scannability, harness visibility, and low-friction collaboration.