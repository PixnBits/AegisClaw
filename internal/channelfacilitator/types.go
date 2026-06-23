package channelfacilitator

// TurnPayload is delivered to agents as channel.turn.
type TurnPayload struct {
	ChannelID        string                 `json:"channel_id"`
	Recipient        string                 `json:"recipient"`
	SinceSeq         int                    `json:"since_seq"`
	NewMessages      []interface{}          `json:"new_messages"`
	RelevanceAnchors []int                  `json:"relevance_anchors"`
	MentionBoosts    map[string]interface{} `json:"mention_boosts,omitempty"`
	GeneratedAt      string                 `json:"generated_at"`
}

// ChannelActor owns per-channel scheduling state (single actor per channel).
type ChannelActor struct {
	chID string
	mu   chan struct{} // capacity-1 token for single-actor serialization
}