

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

**Next implementation steps**:
1. Facilitator skeleton + hub registration + ACLs
2. Persist `last_seen_seq` via Store
3. Implement round-robin + mention boost logic (configurable)
4. Add `get_relevant_since` and `get_messages` to Store
5. Wire turn delivery
6. Add E2E test using the driving collaboration scenario
7. Observability (CLI + traces)

See the detailed spec for payload formats, implicit signals, tool interfaces, and error handling.

