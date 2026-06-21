package channeldata

import (
	"strings"
	"time"
)

// DefaultTurnSettings are global defaults; per-channel overrides live in channel.turn_settings.
var DefaultTurnSettings = TurnSettings{
	MentionBoostPositions:     2,
	MaxMentionBoostsPerCycle:  2,
	StarvationCycles:          3,
	RelevanceWindowMessages:   50,
	RelevanceWindowDuration:   5 * time.Minute,
	MaxRelevanceAnchors:       8,
}

// TurnSettings configures round-robin scheduling and relevance for a channel.
type TurnSettings struct {
	MentionBoostPositions    int           `json:"mention_boost_positions"`
	MaxMentionBoostsPerCycle int           `json:"max_mention_boosts_per_cycle"`
	StarvationCycles         int           `json:"starvation_cycles"`
	RelevanceWindowMessages  int           `json:"relevance_window_messages"`
	RelevanceWindowDuration  time.Duration `json:"-"`
	RelevanceWindowMinutes   int           `json:"relevance_window_minutes,omitempty"`
	MaxRelevanceAnchors      int           `json:"max_relevance_anchors"`
}

// EffectiveTurnSettings merges per-channel overrides with defaults.
func EffectiveTurnSettings(ch map[string]interface{}) TurnSettings {
	out := DefaultTurnSettings
	raw, ok := ch["turn_settings"].(map[string]interface{})
	if !ok {
		return out
	}
	if v, ok := asInt(raw["mention_boost_positions"]); ok && v > 0 {
		out.MentionBoostPositions = v
	}
	if v, ok := asInt(raw["max_mention_boosts_per_cycle"]); ok && v > 0 {
		out.MaxMentionBoostsPerCycle = v
	}
	if v, ok := asInt(raw["starvation_cycles"]); ok && v > 0 {
		out.StarvationCycles = v
	}
	if v, ok := asInt(raw["relevance_window_messages"]); ok && v > 0 {
		out.RelevanceWindowMessages = v
	}
	if v, ok := asInt(raw["relevance_window_minutes"]); ok && v > 0 {
		out.RelevanceWindowDuration = time.Duration(v) * time.Minute
	}
	if v, ok := asInt(raw["max_relevance_anchors"]); ok && v > 0 {
		out.MaxRelevanceAnchors = v
	}
	return out
}

// MemberTurnState is durable per-member channel participation state.
type MemberTurnState struct {
	Role              string `json:"role"`
	LastSeenSeq       int    `json:"last_seen_seq"`
	CyclesSinceTurn   int    `json:"cycles_since_turn,omitempty"`
	MentionBoostsLeft int    `json:"mention_boosts_left,omitempty"`
}

func asInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// MessageSeq extracts the sequence number from a channel message entry.
func MessageSeq(m map[string]interface{}) int {
	if v, ok := asInt(m["seq"]); ok {
		return v
	}
	return 0
}

// MessageFrom extracts the author from a channel message entry.
func MessageFrom(m map[string]interface{}) string {
	if s, ok := m["from"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// MessageContent extracts text content from a channel message entry.
func MessageContent(m map[string]interface{}) string {
	if s, ok := m["content"].(string); ok {
		return s
	}
	if m, ok := m["content"].(map[string]interface{}); ok {
		if s, ok := m["content"].(string); ok {
			return s
		}
	}
	return ""
}

// MessageTimestamp parses RFC3339 ts from a channel message entry.
func MessageTimestamp(m map[string]interface{}) (time.Time, bool) {
	s, _ := m["ts"].(string)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// EnsureMemberDefaults adds last_seen_seq and turn fields to a member map.
func EnsureMemberDefaults(member map[string]interface{}) {
	if _, ok := member["last_seen_seq"]; !ok {
		member["last_seen_seq"] = 0
	}
	if _, ok := member["cycles_since_turn"]; !ok {
		member["cycles_since_turn"] = 0
	}
	if _, ok := member["mention_boosts_left"]; !ok {
		member["mention_boosts_left"] = 0
	}
}

// MemberRole extracts role from a member map.
func MemberRole(m map[string]interface{}) string {
	if s, ok := m["role"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// MemberLastSeenSeq reads durable last_seen_seq from a member map.
func MemberLastSeenSeq(m map[string]interface{}) int {
	if v, ok := asInt(m["last_seen_seq"]); ok {
		return v
	}
	return 0
}

// NextChannelSeq returns and increments the channel's next message sequence.
func NextChannelSeq(ch map[string]interface{}) int {
	seq, _ := asInt(ch["next_seq"])
	if seq <= 0 {
		seq = channelMessageCount(ch) + 1
	}
	ch["next_seq"] = seq + 1
	return seq
}

func channelMessageCount(ch map[string]interface{}) int {
	msgs, ok := ch["messages"].([]interface{})
	if !ok {
		return 0
	}
	max := 0
	for _, item := range msgs {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if s := MessageSeq(m); s > max {
			max = s
		}
	}
	return max
}

// BackfillMessageSeqs assigns seq to legacy messages missing it.
func BackfillMessageSeqs(ch map[string]interface{}) {
	msgs, ok := ch["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		if ch["next_seq"] == nil {
			ch["next_seq"] = 1
		}
		return
	}
	next, _ := asInt(ch["next_seq"])
	if next <= 0 {
		next = 1
	}
	changed := false
	for _, item := range msgs {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if MessageSeq(m) > 0 {
			if MessageSeq(m) >= next {
				next = MessageSeq(m) + 1
			}
			continue
		}
		m["seq"] = next
		next++
		changed = true
	}
	if changed || ch["next_seq"] == nil {
		ch["next_seq"] = next
	}
}

// MessagesSlice returns typed message maps from channel data.
func MessagesSlice(ch map[string]interface{}) []map[string]interface{} {
	raw, ok := ch["messages"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

// MembersSlice returns member maps from channel data.
func MembersSlice(ch map[string]interface{}) []map[string]interface{} {
	raw, ok := ch["members"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]interface{}); ok {
			EnsureMemberDefaults(m)
			out = append(out, m)
		}
	}
	return out
}