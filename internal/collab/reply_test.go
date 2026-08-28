package collab

import "testing"

func TestNormalizeChannelLLMReply(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		want     string
		wantSkip bool
	}{
		{"empty", "", "", true},
		{"exact", "NO_REPLY", "", true},
		{"exact case", "no_reply", "", true},
		{"first line only", "NO_REPLY\n\nExplanation here.", "", true},
		{"trailing line missing speak", "Dear team,\n\nPlease collaborate.\n\nNO_REPLY", "", true},
		{"prose only missing token", "We should sync daily.", "", true},
		{"multiple trailing missing speak", "Plan A\nNO_REPLY\nNO_REPLY", "", true},
		{"pass exact", "PASS", "", true},
		{"pass first line", "PASS\nI have nothing to add about CSS.", "", true},
		{"pass markdown", "**PASS**", "", true},
		{"pass decision prefix", "DECISION: PASS", "", true},
		{"speak then body", "SPEAK\nDo not put the PAT in the prompt.", "Do not put the PAT in the prompt.", false},
		{"speak inline", "SPEAK: Rotate the leaked key and use Secrets VM.", "Rotate the leaked key and use Secrets VM.", false},
		{"speak empty", "SPEAK", "", true},
		{"think then pass", "<think>should I talk?</think>\nPASS", "", true},
		{"think then speak", "<think>risk is real</think>\nSPEAK\nBlock the egress until Court reviews it.", "Block the egress until Court reviews it.", false},
		{"silent token", "SILENT", "", true},
		{"missing token is pass", "Secrets in .env are bad.", "", true},
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
