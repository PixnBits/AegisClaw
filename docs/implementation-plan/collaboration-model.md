

## Turn-Based Message Propagation (New Feature Branch)

**Status**: Specification complete on `feat/channel-turn-based-propagation`. Ready for implementation.

**Key files**:
- `docs/prd/turn-based-message-propagation.md`
- `docs/specs/turn-based-message-propagation.md`

**Core decisions locked**:
- `last_seen_seq` stored durably in Store
- Turn system fully replaces old human-only fan-out for agents
- Mention boosts configurable per channel + global defaults in Settings
- Channel Facilitator as separate logical component
- Humans keep full real-time stream
- Two tools: `get_relevant_since` + `get_messages`
- Round-robin / turn state observable in v1

**Next implementation steps** (updated for enhanced spec):
- Observability foundations (member outcomes, CLI, #agents page merge of turn state)
- Channel status lines + visible error notes (system posts)
- Strengthened scheduling (multi-recipient, fairness/catch-up after mentions)
- Delivery resilience + agent outcome reporting (turn_result)
- Tests + driving E2E (PM plan with multiple roles receives turns + visibility)

See spec for multi-recipient, fairness/catch-up, per-agent activity, error semantics.

See the detailed spec for payload formats, implicit signals, tool interfaces, and error handling.

