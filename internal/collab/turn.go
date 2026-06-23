package collab

import (
	"fmt"
	"strings"
)

// TurnData is the parsed channel.turn payload delivered to agents.
type TurnData struct {
	ChannelID        string
	Recipient        string
	SinceSeq         int
	NewMessages      []map[string]interface{}
	RelevanceAnchors []int
}

// ParseTurnPayload extracts turn fields from a hub message payload.
func ParseTurnPayload(payload interface{}) (TurnData, bool) {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return TurnData{}, false
	}
	td := TurnData{
		ChannelID: stringField(m, "channel_id"),
		Recipient: stringField(m, "recipient"),
		SinceSeq:  intField(m["since_seq"]),
	}
	if raw, ok := m["relevance_anchors"].([]interface{}); ok {
		for _, a := range raw {
			td.RelevanceAnchors = append(td.RelevanceAnchors, intField(a))
		}
	}
	if raw, ok := m["new_messages"].([]interface{}); ok {
		for _, item := range raw {
			if msg, ok := item.(map[string]interface{}); ok {
				td.NewMessages = append(td.NewMessages, msg)
			}
		}
	}
	return td, td.ChannelID != ""
}

// FormatTurnMessages builds a readable block from batched new messages.
func FormatTurnMessages(msgs []map[string]interface{}) string {
	var b strings.Builder
	for _, m := range msgs {
		from := ""
		if s, ok := m["from"].(string); ok {
			from = s
		}
		content := PayloadContentString(m["content"])
		if content == "" {
			if s, ok := m["content"].(string); ok {
				content = s
			}
		}
		fmt.Fprintf(&b, "- %s: %s\n", from, content)
	}
	return strings.TrimSpace(b.String())
}

// FormatAnchorContext builds readable context from anchor message maps.
func FormatAnchorContext(anchors []map[string]interface{}) string {
	var b strings.Builder
	for _, m := range anchors {
		from := ""
		if s, ok := m["from"].(string); ok {
			from = s
		}
		content := PayloadContentString(m["content"])
		if content == "" {
			if s, ok := m["content"].(string); ok {
				content = s
			}
		}
		seq := intField(m["seq"])
		fmt.Fprintf(&b, "[seq %d] %s: %s\n", seq, from, content)
	}
	return strings.TrimSpace(b.String())
}

func stringField(m map[string]interface{}, key string) string {
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func intField(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}