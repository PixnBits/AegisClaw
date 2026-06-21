// Package channelfacilitator implements turn-based message propagation for channels.
package channelfacilitator

// Hub component id for the Channel Facilitator (logically separate; may be co-located in daemon).
const ComponentID = "channel-facilitator"

// Hub commands for turn-based channel propagation.
const (
	CmdTurn              = "channel.turn"
	CmdUpdated           = "channel.updated"
	CmdGetRelevantSince  = "channel.get_relevant_since"
	CmdGetMessages       = "channel.get_messages"
	CmdMemberTurnUpdate  = "channel.member_turn_update"
	CmdTurnState         = "channel.turn_state"
	CmdTurnStateData     = "channel.turn_state.data"
)