package channeldata

import "testing"

func TestNextChannelSeqAndBackfill(t *testing.T) {
	ch := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"from": "user", "content": "hello"},
		},
	}
	BackfillMessageSeqs(ch)
	if seq := MessageSeq(ch["messages"].([]interface{})[0].(map[string]interface{})); seq != 1 {
		t.Fatalf("backfill seq = %d, want 1", seq)
	}
	next := NextChannelSeq(ch)
	if next != 2 {
		t.Fatalf("next seq = %d, want 2", next)
	}
}

func TestEffectiveTurnSettingsDefaults(t *testing.T) {
	s := EffectiveTurnSettings(map[string]interface{}{})
	if s.MentionBoostPositions != 2 || s.StarvationCycles != 3 {
		t.Fatalf("unexpected defaults: %+v", s)
	}
}

func TestEffectiveTurnSettingsOverride(t *testing.T) {
	ch := map[string]interface{}{
		"turn_settings": map[string]interface{}{
			"mention_boost_positions": 3,
			"starvation_cycles":       5,
		},
	}
	s := EffectiveTurnSettings(ch)
	if s.MentionBoostPositions != 3 || s.StarvationCycles != 5 {
		t.Fatalf("override failed: %+v", s)
	}
}

func TestMemberLastSeenSeqAndDefaults(t *testing.T) {
	// Empty member gets defaults for durable turn state (last_seen_seq etc).
	m := map[string]interface{}{"role": "coder"}
	EnsureMemberDefaults(m)
	if MemberLastSeenSeq(m) != 0 {
		t.Fatalf("expected last_seen_seq=0 default, got %d", MemberLastSeenSeq(m))
	}
	if m["cycles_since_turn"] != 0 || m["mention_boosts_left"] != 0 {
		t.Fatalf("expected turn cycle/boost defaults, got %+v", m)
	}

	// Persisted last_seen_seq is readable (Store writes this via member_turn_update).
	m2 := map[string]interface{}{"role": "ciso", "last_seen_seq": 42, "cycles_since_turn": 1}
	EnsureMemberDefaults(m2)
	if MemberLastSeenSeq(m2) != 42 {
		t.Fatalf("expected last_seen_seq=42, got %d", MemberLastSeenSeq(m2))
	}
}