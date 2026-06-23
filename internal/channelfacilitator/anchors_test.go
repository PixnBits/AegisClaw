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

func TestComputeRelevanceAnchorsSignals(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	// Window (prior context): PM plan with assignment + mention of ciso, plus another topical post.
	window := []map[string]interface{}{
		{"seq": 10, "from": "project-manager-main", "content": "Plan: @coder implement hello; flag security concern for @ciso", "ts": now},
		{"seq": 11, "from": "user", "content": "Please review risk on auth change", "ts": now},
		{"seq": 12, "from": "project-manager-main", "content": "Monitoring: coder should start", "ts": now},
	}
	// New batch (the turn recipient's new messages)
	batch := []map[string]interface{}{
		{"seq": 13, "from": "coder-ch1", "content": "working on hello feature", "ts": now},
	}
	// For CISO: direct mention (seq10) + PM post (seq12) + assignment phrase + topical "security"/"risk" should score high.
	anchors := ComputeRelevanceAnchors("court-persona-ciso", 9, batch, window, channeldata.DefaultTurnSettings)
	found := map[int]bool{}
	for _, a := range anchors {
		found[a] = true
	}
	if !found[10] {
		t.Fatalf("expected anchor 10 (PM plan with @ciso mention), got %v", anchors)
	}
	// seq11 has topical overlap ("review risk" ~ security concern), may rank.
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