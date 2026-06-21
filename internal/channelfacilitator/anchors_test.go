package channelfacilitator

import (
	"testing"
	"time"

	"AegisClaw/internal/channeldata"
)

func TestComputeRelevanceAnchorsMentionAndPM(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	window := []map[string]interface{}{
		{"seq": 1, "from": "user", "content": "plan for @coder and flag ciso concern", "ts": now},
		{"seq": 2, "from": "project-manager-main", "content": "Plan: assign coder to implement feature", "ts": now},
	}
	batch := []map[string]interface{}{
		{"seq": 3, "from": "coder-ch1", "content": "progress update on feature implementation", "ts": now},
	}
	anchors := ComputeRelevanceAnchors("court-persona-ciso", 0, batch, window, channeldata.DefaultTurnSettings)
	if len(anchors) == 0 {
		t.Fatal("expected anchors")
	}
}

func TestSelectNextRecipientRoundRobin(t *testing.T) {
	members := []map[string]interface{}{
		{"role": "project-manager", "last_seen_seq": 0, "cycles_since_turn": 0},
		{"role": "coder", "last_seen_seq": 0, "cycles_since_turn": 0},
		{"role": "court-persona-ciso", "last_seen_seq": 0, "cycles_since_turn": 0},
	}
	r1, idx1, _ := SelectNextRecipient(members, 0, "hello", channeldata.DefaultTurnSettings)
	r2, idx2, _ := SelectNextRecipient(members, idx1, "hello", channeldata.DefaultTurnSettings)
	if r1 == "" || r2 == "" || r1 == r2 {
		t.Fatalf("round robin failed: r1=%s r2=%s", r1, r2)
	}
	if idx1 == idx2 {
		t.Fatalf("index did not advance: %d", idx1)
	}
}