# Turn-Based Message Propagation

**Status**: Detailed specification (v1)

This is the authoritative technical specification for implementing batched turn-based message propagation. It is intentionally specific to minimise surprises and ad-hoc decisions during implementation.

## 1. Goals & Non-Goals

**Goals**
- Enable agents to collaborate by receiving coherent batches of new messages since their own last turn.
- Provide lightweight relevance context so agents can understand what discussion the batch advances.
- Keep LLM token usage bounded even on long or bursty channels.
- Keep the host daemon minimal (no new privileged logic).
- Preserve existing security model (signed hub messages, ACLs, isolation).

**Non-Goals (for v1)**
- Explicit `reply_to` threading.
- Facilitator performing summarisation.
- Rich pre-computed conversation state in every turn.
- Cross-channel turn coordination.

## 2. High-Level Architecture

- **Channel Facilitator**: New thin microVM (or initially part of `project-manager`). Owns turn scheduling and delivery.
- **Store**: Source of truth for messages + new `get_relevant_since` query.
- **Agents**: Consume turns via their existing agentic loop + new relevance tool.
- **Web Portal / Humans**: Continue to receive full real-time stream via STOMP (unchanged).

All inter-component communication uses existing signed hubclient patterns.

## 3. Turn Scheduling & Delivery

### 3.1 Turn Order

- Per-channel round-robin queue of current members.
- After a human post or significant agent post, the facilitator advances the queue and delivers the next turn(s).
- **Mention boost**: An agent mentioned in the most recent activity receives a +2 position boost (maximum 2 boosts per full cycle). Boosts decay after use.
- Starvation guard: Any member whose last turn is older than N full cycles is forced to the front on the next cycle.

### 3.2 Turn Payload (Wire Format)

Hub command: `channel.turn`

```json
{
  "channel_id": "string",
  "recipient_role_or_id": "string",     // e.g. "project-manager" or "court-persona-ciso"
  "since_seq": 42,
  "new_messages": [ Message, ... ],
  "relevance_anchors": [38, 39, 41],   // seq numbers
  "mention_boosts": { "court-persona-ciso": 1 },
  "generated_at": "2026-06-21T..."
}
```

`Message` shape is the existing channel message format used by Store.

`relevance_anchors` is a small ordered list (max 8 in v1) of prior message seqs that the facilitator determined are likely relevant via implicit signals.

### 3.3 When Turns Are Delivered

Primary triggers:
- Human post to channel (immediate consideration for turn delivery).
- Agent post that is a broadcast, assignment, or contains strong signals.
- Time-based / idle catch-up (configurable, default off in v1).

The facilitator decides the recipient list for each turn based on current channel membership + round-robin state.

## 4. Relevance Anchors (Implicit Signals)

The facilitator computes `relevance_anchors` using only cheap, local signals from the recent message window (last 50 messages or last 5 minutes, whichever is smaller).

Signals (in priority order):
1. Messages that directly @mention the recipient role/id.
2. Messages authored by the same agent as the most recent activity in the batch.
3. Messages containing explicit assignment language ("assign to", "@Coder", "your task").
4. Recent PM monitoring or plan posts.
5. Topical keyword overlap (simple token match on key nouns from the new batch).

The facilitator returns the top 8 distinct seqs. No scoring beyond ordering; simple recency + signal strength tie-break.

**No LLM is used** for anchor selection in v1.

## 5. Relevance Tool (Agent-Facing)

New agent-callable tool exposed via hub:

**Command**: `channel.get_relevant_since`

**Request**:
```json
{
  "channel_id": "string",
  "since_seq": number,
  "anchors": number[],
  "focus": "string?"          // optional: "assignments", "security", "my_tasks", ...
  "max_results": number      // default 12, max 20 in v1
}
```

**Response**:
```json
{
  "messages": [ Message, ... ],
  "total_available": number
}
```

The tool must return messages that help the agent understand the context of the current turn. It may use the supplied `anchors` as strong seeds and `focus` as a filter hint.

Implementation note for v1: Simple recency + anchor overlap + optional keyword filter is acceptable. More sophisticated retrieval can be added later without changing the interface.

Error cases: Channel not found, seq out of range → clear error with `since_seq` suggestion.

## 6. Facilitator MicroVM Detailed Responsibilities

### State
- Per-channel: current round-robin position, per-member `last_seen_seq`, mention boost counters (decay after use).
- `last_seen_seq` may be stored in facilitator-local state or written back to Store membership record (decision: local state in v1 for simplicity; can move to Store later).

### Startup
- Registers on AegisHub as `channel-facilitator` (or `project-manager` if absorbed).
- Receives `channel.*` events it cares about via hub subscription.

### On Receiving Activity
1. Update internal last-seen for the poster.
2. Compute relevance anchors for affected recipients.
3. For each recipient due for a turn: build payload and send `channel.turn`.
4. Update last-seen for recipients after successful delivery.

### Concurrency
- Single goroutine / actor per channel to avoid races on round-robin state.
- Use existing ephemeral hub client pattern for delivery (already hardened).

## 7. ACL & Security Requirements

New or extended ACL rules (in `config/acls.yaml`):

```yaml
- source: channel-facilitator
  destination: "*"
  commands: ["channel.turn"]

- source: "*"
  destination: channel-facilitator
  commands: ["channel.activity", "channel.updated"]

- source: agent-*
  destination: store
  commands: ["channel.get_relevant_since"]
```

All messages remain signed. Facilitator has read-only access to recent channel metadata.

## 8. Observability & Debugging

Extend existing `[collab-trace]` with stages:
- `facilitator.turn.compute`
- `facilitator.anchor.select`
- `agent.turn.received`
- `agent.relevance.triage`
- `agent.relevance.tool_call`

Add `AEGIS_TURN_TRACE=1` env var (propagated to facilitator and agents).

## 9. Error Handling & Resilience

- Failed turn delivery → retry with backoff (max 3 attempts, then log and continue).
- Store query failure for anchors → fall back to most recent N messages.
- Agent fails to process turn → facilitator still advances last_seen (prevents repeated delivery of same batch).

## 10. Performance & Sizing (v1 Targets)

- Turn payload size: < 50 messages + 8 anchors.
- Relevance tool latency: < 200 ms p99 on warm Store.
- Facilitator memory: < 50 MB per active channel (soft target).
- No impact on cold-start or pre-warm budgets for other roles.

## 11. Implementation Order Recommendation

1. Define hub message types and ACLs.
2. Implement facilitator skeleton + round-robin state.
3. Add `get_relevant_since` to Store (simple version).
4. Wire turn delivery from facilitator.
5. Add relevance tool call + triage in a test agent.
6. Add E2E test with the driving use case.
7. Harden, trace, and measure.

## 12. Open Questions (to be resolved before or during implementation)

- Exact storage location for `last_seen_seq` (facilitator vs Store).
- Whether mention boost logic lives only in facilitator or is also visible to Store for query hints.
- v1 limit values (max messages per turn, max anchors, boost amount).

---

*This spec is intentionally detailed. Implementors should raise questions here rather than making local decisions.*