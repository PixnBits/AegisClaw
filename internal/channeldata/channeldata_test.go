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