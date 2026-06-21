# Turn-Based Message Propagation

**Status**: Active proposal (moved from proposals/ into main PRD area)

This document describes the shift from per-message reactive fan-out to batched, turn-based message propagation in AegisClaw channels. It enables proper multi-agent collaboration while controlling LLM cost and preserving conversational context.

## Motivation

Current implementation limits `channel.activity` fan-out to human posts only (see `IsHumanPoster` + `portalChannelFanout` in `cmd/aegis/portal_bridge.go` and the explicit note in `channel-collab-debugging.md`). Agents cannot natively react to each other (PM → Coder assignment, Coder update → CISO review, etc.).

We need a model that:
- Supports true inter-agent collaboration.
- Avoids quadratic or excessive LLM usage.
- Keeps the daemon minimal (strong permissions boundary).
- Leverages the existing agentic loop, Store authority, and hub routing.

## Goals

- Agents receive **batches** of new messages since their last turn (not every individual message).
- Batches arrive with enough context to understand what discussion they advance.
- Turn scheduling is fair (round-robin) with limited priority boosts for mentions.
- Context reconstruction and large-batch handling are performed by the receiving agent's loop using a dedicated tool.
- The orchestrating component (facilitator) stays thin and dumb.

## Model

### Turn Delivery

- A **turn** is a batched delivery of new messages since an agent's last seen sequence number.
- Turns are delivered in **round-robin** order per channel.
- Agents mentioned in recent activity receive a **bounded priority boost** in the queue (prevents starvation of other members).
- Human posts remain a strong immediate trigger for the first turn(s).

### Turn Payload

```json
{
  "channel_id": "...",
  "since_seq": 42,
  "new_messages": [...],
  "relevance_anchors": [38, 39, 41],
  "mention_boosts": {...},
  "hints": {...}
}
```

`relevance_anchors` are a small list of prior message IDs/seqs identified via cheap implicit signals (recent @mentions of recipient, author continuity, assignment language, topical keyword overlap).

No rich pre-computed "entering state" summary and no explicit `reply_to` threading at this stage.

### Relevance & Context Reconstruction

New messages arrive "in media res". The agent determines relevant prior context by:

1. Using the supplied `relevance_anchors`.
2. Calling the tool `channel.get_relevant_since(last_seq, anchors, focus?)` from its agentic loop as the first micro-step.
3. The tool returns a short, curated set of prior messages/excerpts.

This mechanism also handles large batches gracefully — the agent's triage step decides how much additional context to pull.

The agent may then decide to self-summarise or post a concise state note back to the channel.

### Facilitator MicroVM

- Thin dedicated component (initially may be absorbed into Project Manager responsibilities).
- Registered on AegisHub with appropriate ACLs.
- Owns per-channel round-robin state and limited mention-boost counters.
- Computes cheap implicit-signal anchors by querying Store.
- Delivers turns via existing hub patterns.
- No summarisation logic.
- Pre-warmable and consistent with <1s on-demand targets.

## Deferred / Future

- Explicit threading primitives (`reply_to`).
- Facilitator-side summarisation.
- Rich pre-computed conversation state in every turn.

## Testing

**Driving E2E use case**:

"PM posts a plan that assigns work to Coder and flags a security concern for CISO. Coder posts a progress update. CISO receives a single batched turn containing both messages plus relevance anchors. CISO's triage step correctly surfaces the relevant prior assignment via tool and posts a push-back comment."

Verify:
- Bounded delivery
- Correct mention priority without starving other agents
- Store + portal visibility
- No unnecessary LLM calls on irrelevant history

Extend existing `make test-e2e-portal-channel`, `test-e2e-llm`, and collab tracing.

## Open Questions

- Exact set of implicit signals used to compute relevance anchors.
- Hard bounds on batch size and number of anchors per turn.
- Long-term ownership of the facilitator (separate thin VM vs. evolved PM role).
- Performance characteristics of the relevance tool under sustained load.

---

*This change preserves the paranoid security model, keeps the daemon minimal, and gives agents the tools they need to collaborate effectively.*