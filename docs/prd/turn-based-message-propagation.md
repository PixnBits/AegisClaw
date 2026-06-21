# Turn-Based Message Propagation

This document defines the turn-based message propagation model for AegisClaw channels. It replaces the previous per-message reactive fan-out (limited to human posts) with a more efficient, context-aware batched turn system that enables proper multi-agent collaboration.

## Motivation

The original implementation restricted `channel.activity` fan-out to human-originating messages only. Agents could not react to each other in a structured way (e.g. PM assignments triggering Coder work, or Coder updates prompting CISO review). This limited collaborative capability while still risking high LLM usage on busy channels.

The new model delivers batches of new messages since each agent's last turn, provides lightweight relevance context, and keeps the orchestrating component thin.

## Key Decisions

- `last_seen_seq` is stored durably in the Store as part of channel membership state.
- The turn system fully replaces the old human-only fan-out path for agents.
- Mention priority boosts are configurable per channel (with global defaults in the Settings page).
- The Channel Facilitator is a logically separate component.
- Human participants continue to receive the full real-time message stream via STOMP.
- Agents have both a relevance tool and a direct `channel.get_messages` tool, and may persist relevance judgments in their own memory between turns.
- Round-robin state and `last_seen_seq` values are observable in v1 (CLI + planned web portal support).

## Model Overview

### Turn Delivery

Agents receive **batched turns** containing new messages since their last seen sequence. Turns follow a round-robin order per channel, with bounded priority boosts for recently mentioned agents.

Human posts act as strong triggers for immediate turn consideration.

### Turn Payload & Relevance

Each turn includes the new messages plus a small set of `relevance_anchors` (prior message seqs selected via cheap implicit signals such as @mentions, author continuity, assignment language, and topical overlap).

Agents reconstruct necessary prior context by using the supplied anchors and calling the `channel.get_relevant_since` tool from their agentic loop. A separate `channel.get_messages` tool is also available when an agent prefers to perform its own relevance analysis.

### Facilitator

A thin Channel Facilitator microVM (or initially co-located logic) owns round-robin scheduling, anchor computation, and turn delivery. It remains deliberately dumb — it does not perform summarisation or maintain rich conversation state.

## Deferred

- Explicit `reply_to` threading primitives.
- Facilitator-side summarisation.

## Testing

The primary validation scenario is:

"PM posts a plan assigning work to Coder and flagging a concern for CISO. Coder posts a progress update. CISO receives a batched turn with relevant anchors, correctly surfaces prior context, and posts a push-back."

This exercises bounded delivery, mention priority, relevance tools, and correct Store/portal visibility.

## Related Documents

- `docs/specs/turn-based-message-propagation.md` — Detailed technical specification (authoritative for implementation).
- `docs/implementation-plan/collaboration-model.md` — Implementation tracking.

---

*This model preserves the paranoid security model, keeps the daemon minimal, and gives agents the structured collaboration capabilities they need.*