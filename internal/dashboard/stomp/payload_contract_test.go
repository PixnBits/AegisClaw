// Contract tests: STOMP payload shapes per docs/specs/web-portal/real-time-contracts.md
package stomp_test

import (
	"encoding/json"
	"testing"
	"time"

	"AegisClaw/internal/dashboard/contracts"
)

func TestParseKnownPayloadShapes(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	cases := []struct {
		name string
		body string
		typ  string
	}{
		{"overview", `{"type":"overview.stats","timestamp":"` + now + `","active_agents":{"total":1,"by_role":{}},"background_tasks":{"total":0,"avg_progress":0},"pending_proposals":0}`, contracts.TypeOverviewStats},
		{"channel", `{"type":"channel.activity","channel_id":"main","event":{"kind":"message"},"timestamp":"` + now + `"}`, contracts.TypeChannelActivity},
		{"canvas", `{"type":"canvas.event","persona_task_id":"pt_1","task_id":"task_1","stage":"Execute","progress":10,"timestamp":"` + now + `"}`, contracts.TypeCanvasEvent},
		{"harness plan", `{"type":"harness.plan.created","plan_id":"p1","channel_id":"main","goal":"test","stages":[]}`, contracts.TypeHarnessPlanCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typ, err := contracts.ParsePayload([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if typ != tc.typ {
				t.Fatalf("type %q want %q", typ, tc.typ)
			}
		})
	}
}

func TestUnknownFieldsIgnoredGracefully(t *testing.T) {
	body := `{"type":"channel.activity","channel_id":"main","future_field":42,"event":{},"timestamp":"2026-01-01T00:00:00Z"}`
	var parsed contracts.ChannelActivity
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.ChannelID != "main" {
		t.Fatalf("channel_id: %q", parsed.ChannelID)
	}
}