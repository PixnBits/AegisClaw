# Turn-Based Message Propagation

**Status**: Initial spec draft

This document captures the technical specification for batched turn-based message propagation in channels.

## Overview

Replace the current human-only `channel.activity` fan-out with a turn-based delivery system. Agents receive coherent batches of new messages since their last turn, along with lightweight relevance anchors. Context reconstruction and handling of large batches is performed inside the receiving agent's existing agentic loop.

## Components

### 1. Channel Facilitator (new thin microVM)

**Responsibilities**:
- Maintain per-channel round-robin turn order.
- Track per-member `last_seen_seq`.
- Apply bounded mention priority boosts.
- Compute cheap relevance anchors using implicit signals from recent messages.
- Build and deliver `channel.turn` payloads.

**Location**: New thin guest binary (or initially absorbed into `project-manager`).
**Registration**: Standard hub registration + ACL rules.
**Lifecycle**: On-demand / pre-warmable, consistent with other roles.

**No responsibilities**:
- Summarisation
- Maintaining rich conversation state
- LLM calls

### 2. Store

**New / extended queries**:
- `get_relevant_since(channel_id, since_seq, anchors, focus?)` — returns curated prior messages.
- Efficient access to recent message metadata for anchor computation (mentions, author, keywords, assignment markers).

Store remains the single source of truth for all message history and membership.

### 3. Agent Runtime (existing)

- On receipt of a turn, the agent's main loop treats it as new observations.
- First micro-step: relevance triage using anchors + optional call to `get_relevant_since`.
- Subsequent steps: decide action, possibly self-summarise, or stay silent.
- Strengthens existing `ShouldRespondToActivity` logic.

### 4. Web Portal / STOMP

- Human view remains real-time full message stream (unchanged).
- Agents receive private turn batches.

## Data Model

### Turn Payload (hub message)

```json
{
  "type": "channel.turn",
  "channel_id": "string",
  "recipient": "string",           // role or specific agent id
  "since_seq": number,
  "new_messages": [...],
  "relevance_anchors": number[],   // seqs/ids
  "mention_boosts_applied": {...}
}
```

### Relevance Anchors

Computed from implicit signals (no explicit threading required):
- Recent messages that @mention the recipient
- Recent messages by the same author as recent activity
- Messages containing assignment language or proposal references
- Topical keyword overlap with the new batch

## Interfaces

### New Tool (agent-callable)

`channel.get_relevant_since(last_seq, anchors, focus?)`

Returns a short list of prior messages/excerpts relevant to the current turn.

### Hub Commands / Routing

- New or extended routing for `channel.turn` delivery.
- ACLs: facilitator → agents (turn delivery), agents → store (relevance queries).

## Security & Isolation

- All turn delivery goes through signed hub messages.
- Facilitator has minimal privileges (read recent channel metadata + deliver turns).
- No new privileged operations in the host daemon.

## Observability

Extend existing `[collab-trace]` instrumentation to cover:
- Turn computation
- Anchor selection
- Relevance tool calls
- Agent triage decisions

## Open Implementation Questions

- Exact algorithm / window size for computing implicit relevance anchors.
- Hard limits on `new_messages` per turn and `relevance_anchors` per turn.
- Whether `last_seen_seq` lives in Store membership or facilitator-local state.
- Performance budget and caching strategy for the relevance tool.

## Related Documents

- `docs/prd/turn-based-message-propagation.md`
- `docs/channel-collab-debugging.md` (will be extended)
- `docs/implementation-plan/collaboration-model.md` (tracking item needed)

---

*Initial spec to drive implementation. Expect iteration as the facilitator and relevance tool are built.*