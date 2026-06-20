package collab

import "testing"

func TestNormalizeChannelLLMReply(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantSkip bool
	}{
		{"empty", "", "", true},
		{"exact", "NO_REPLY", "", true},
		{"exact case", "no_reply", "", true},
		{"first line only", "NO_REPLY\n\nExplanation here.", "", true},
		{"trailing line", "Dear team,\n\nPlease collaborate.\n\nNO_REPLY", "Dear team,\n\nPlease collaborate.", false},
		{"trailing only whitespace", "Hello world\n\n  NO_REPLY  \n", "Hello world", false},
		{"prose only", "We should sync daily.", "We should sync daily.", false},
		{"multiple trailing", "Plan A\nNO_REPLY\nNO_REPLY", "Plan A", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, skip := NormalizeChannelLLMReply(tt.raw)
			if skip != tt.wantSkip {
				t.Fatalf("skip=%v want %v", skip, tt.wantSkip)
			}
			if got != tt.want {
				t.Fatalf("content=%q want %q", got, tt.want)
			}
		})
	}
}
