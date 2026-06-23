package collab

import "testing"

func TestParseTurnPayload(t *testing.T) {
	p := map[string]interface{}{
		"channel_id":        "ch1",
		"recipient":         "coder",
		"since_seq":         5,
		"new_messages":      []interface{}{map[string]interface{}{"seq": 6, "from": "pm", "content": "do it"}},
		"relevance_anchors": []interface{}{4, 3},
	}
	td, ok := ParseTurnPayload(p)
	if !ok || td.ChannelID != "ch1" || td.SinceSeq != 5 || len(td.NewMessages) != 1 || len(td.RelevanceAnchors) != 2 {
		t.Fatalf("bad parse: %+v", td)
	}
}

func TestFormatTurnMessagesAndAnchors(t *testing.T) {
	msgs := []map[string]interface{}{
		{"from": "coder", "content": "done"},
	}
	s := FormatTurnMessages(msgs)
	if s == "" || !contains(s, "coder") {
		t.Fatalf("format messages: %s", s)
	}
	anchors := []map[string]interface{}{
		{"seq": 42, "from": "pm", "content": "plan"},
	}
	a := FormatAnchorContext(anchors)
	if a == "" || !contains(a, "42") {
		t.Fatalf("format anchors: %s", a)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
