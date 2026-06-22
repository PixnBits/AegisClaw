// Package channelfacilitator implements turn-based message propagation for channels.
package channelfacilitator

// Hub component id for the Channel Facilitator (logically separate; may be co-located in daemon).
const ComponentID = "channel-facilitator"

// Hub commands for turn-based channel propagation.
// These define the wire protocol message types for the turn-based model
// (see docs/specs/turn-based-message-propagation.md).
//
// Turn delivery:
//   channel.turn  -> agent/role VMs with TurnPayload:
//     { channel_id, recipient, since_seq, new_messages, relevance_anchors, mention_boosts, generated_at }
//
// Store notifications to facilitator:
//   channel.updated  (from store on every channel.post)
//
// Store tools callable by agents (via get_relevant_since / get_messages):
//   channel.get_relevant_since  -> returns anchors + new_messages since seq
//   channel.get_messages        -> direct fetch with optional filter
//
// Facilitator <-> Store state:
//   channel.member_turn_update  (persist last_seen_seq, cycles, rr index, boosts)
//   channel.turn_state / .data  (observability)
const (
	CmdTurn              = "channel.turn"
	CmdUpdated           = "channel.updated"
	CmdGetRelevantSince  = "channel.get_relevant_since"
	CmdGetMessages       = "channel.get_messages"
	CmdMemberTurnUpdate  = "channel.member_turn_update"
	CmdTurnState         = "channel.turn_state"
	CmdTurnStateData     = "channel.turn_state.data"
)