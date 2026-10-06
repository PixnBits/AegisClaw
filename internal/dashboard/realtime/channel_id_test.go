package realtime

import (
	"strings"
	"testing"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/portalstomp"
)

// Fake key material for tests; not real credentials.
var rtSecret = "sk-" + strings.Repeat("Qw7", 8)

func subscribe(hub *portalstomp.Hub, topic string) *portalstomp.Session {
	s := portalstomp.NewSession(hub)
	s.HandleFrame("SUBSCRIBE", map[string]string{"id": "s", "destination": topic}, "")
	return s
}

// The SPA routes channel.activity on channel_id, so a valid id is sent raw;
// the content stays redacted.
func TestPublishChannelActivityValidIDRaw(t *testing.T) {
	for _, id := range []string{"task-refactorauthenticationmodule", "sk-abcdefghijklmnopqrstu", "plan2sk-abcdefghijklmnopqrstuvw", "plan-demo"} {
		hub := portalstomp.NewHub()
		canon := subscribe(hub, contracts.ChannelActivityTopic(id))
		legacy := subscribe(hub, contracts.LegacyChannelMessagesTopic(id))
		NewPublisher(hub).PublishChannelActivity(id, "user", "hello "+rtSecret)
		for _, sess := range []*portalstomp.Session{canon, legacy} {
			ev := readUsageFrame(t, sess)
			if ev["channel_id"] != id {
				t.Errorf("channel_id = %v, want %q", ev["channel_id"], id)
			}
			inner, _ := ev["event"].(string)
			if inner == "" {
				if b, ok := ev["event"].(map[string]interface{}); ok {
					inner, _ = b["content"].(string)
				}
			}
			if strings.Contains(inner, rtSecret[3:]) || !strings.Contains(inner, "[REDACTED]") {
				t.Errorf("event for %q = %v, want content redacted", id, ev["event"])
			}
		}
	}
}

func TestPublishChannelActivityInvalidIDRedacted(t *testing.T) {
	id := "SK-ABCDEFGHIJKLMNOPQRSTU"
	hub := portalstomp.NewHub()
	canon := subscribe(hub, contracts.ChannelActivityTopic(id))
	NewPublisher(hub).PublishChannelActivity(id, "user", "x")
	ev := readUsageFrame(t, canon)
	if ev["channel_id"] != "[REDACTED]" {
		t.Fatalf("invalid channel_id = %v, want [REDACTED]", ev["channel_id"])
	}
}

// Harness plan events carry channel_id at the top level; the SPA keys the
// harness on it.
func TestPublishHarnessValidChannelIDRaw(t *testing.T) {
	id := "task-refactorauthenticationmodule"
	hub := portalstomp.NewHub()
	plan := subscribe(hub, contracts.HarnessUpdatesTopic("p1"))
	NewPublisher(hub).PublishHarness("p1", id, map[string]interface{}{
		"type":       contracts.TypeHarnessPlanCreated,
		"plan_id":    "p1",
		"channel_id": id,
		"goal":       "ship " + rtSecret,
	})
	ev := readUsageFrame(t, plan)
	if ev["channel_id"] != id {
		t.Errorf("harness channel_id = %v, want %q", ev["channel_id"], id)
	}
	if g, _ := ev["goal"].(string); strings.Contains(g, rtSecret[3:]) {
		t.Errorf("harness goal leaked: %q", g)
	}
}
