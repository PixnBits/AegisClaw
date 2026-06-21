# Turn-Based Message Propagation for Channel Collaboration

**Status**: Draft proposal on feature branch `feat/channel-turn-based-propagation`

**Intended for**: Addition to `docs/prd/collaboration-model.md` (Section 3) and tracking in the implementation plan.

---

## Motivation

The current message fan-out in web portal channels is limited to messages from the user (see `portalChannelFanout` + `IsHumanPoster` gate and the explicit note in `channel-collab-debugging.md`). This prevents true collaborative work between agents (PM assigning to Coder, Coder update triggering CISO review, etc.).

We need a propagation model that:
- Does not drastically increase LLM usage.
- Allows agents to work collaboratively with proper context.
- Keeps the daemon minimal (permissions boundary).
- Leverages the existing agentic loop and Store as source of truth.

## Design

### Core Model

- **Batched turns** instead of per-message reactivity.
- New messages since an agent's last turn are delivered together, in context of prior conversation.
- Turn order is generally round-robin, with limited priority boosts for @mentioned agents (to avoid starvation).
- A thin **channel-facilitator** microVM (or initially part of PM) owns the turn queue and delivery.

### Turn Payload

A turn includes:
- Bounded new messages since last seen.
- **Relevance anchors**: small list of prior message IDs identified via cheap implicit signals (mentions, author continuity, assignment language, topical overlap).
- Mention/boost metadata.

No rich pre-computed "entering state" digest. No explicit threading at this stage.

### Relevance & Context

Agents determine relevant prior messages using:
- The supplied anchors.
- A new tool `channel.get_relevant_since(last_seq, anchors, focus?)` called from the agentic loop as the first micro-step.
- This same mechanism handles large batches by filtering.

The agentic loop performs triage → optional retrieval → decision (act / summarise / stay quiet).

### Facilitator

- Thin microVM registered on AegisHub.
- Maintains round-robin state + bounded mention boost counters.
- Computes cheap implicit-signal anchors.
- No summarisation logic.

## Deferred

- Explicit `reply_to` threading.
- Facilitator-side summarisation.

## Testing

Driving E2E use case:

"PM posts a plan assigning work to Coder and flagging a concern for CISO. Coder posts a progress update. CISO receives a batched turn with relevant anchors, correctly surfaces prior context via tool, and posts a push-back."

Extend existing channel E2E tests and collab tracing.

## Open Questions

- Exact implicit signals for anchors.
- Bounds on batch size and anchors per turn.
- Long-term owner of the facilitator (separate thin VM vs PM).

---

*Preserves paranoid model, daemon minimal, Store authority, and agentic loop strengths.*