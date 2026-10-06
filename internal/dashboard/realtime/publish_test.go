package realtime

import (
	"encoding/json"
	"strings"
	"testing"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/portalstomp"
)

func TestPublishLLMUsageEmitsGlobalAndAgentTopic(t *testing.T) {
	hub := portalstomp.NewHub()
	global := portalstomp.NewSession(hub)
	global.HandleFrame("SUBSCRIBE", map[string]string{
		"id":          "g",
		"destination": contracts.TopicLLMUsagePrefix,
	}, "")
	agent := portalstomp.NewSession(hub)
	agent.HandleFrame("SUBSCRIBE", map[string]string{
		"id":          "a",
		"destination": contracts.LLMUsageTopic("coder-1"),
	}, "")

	NewPublisher(hub).PublishLLMUsage("coder-1", map[string]interface{}{
		"model":             "qwen",
		"timestamp":         "2026-10-06T00:00:00Z",
		"tokens_prompt":     float64(3),
		"tokens_completion": float64(4),
		"duration_ms":       float64(5),
		"success":           false,
	})

	g := readUsageFrame(t, global)
	a := readUsageFrame(t, agent)
	for _, ev := range []map[string]interface{}{g, a} {
		if ev["type"] != contracts.TypeLLMUsage || ev["agent_id"] != "coder-1" {
			t.Fatalf("shape %+v", ev)
		}
		if ev["model"] != "qwen" || ev["tokens_prompt"].(float64) != 3 || ev["tokens_completion"].(float64) != 4 || ev["duration_ms"].(float64) != 5 {
			t.Fatalf("usage %+v", ev)
		}
		if ev["success"] != false || ev["timestamp"] != "2026-10-06T00:00:00Z" {
			t.Fatalf("flags %+v", ev)
		}
	}
}

func readUsageFrame(t *testing.T, sess *portalstomp.Session) map[string]interface{} {
	t.Helper()
	select {
	case frame := <-sess.Outbound():
		idx := strings.Index(frame, "\n\n")
		if idx < 0 {
			t.Fatalf("frame %q", frame)
		}
		body := strings.TrimSuffix(frame[idx+2:], "\x00")
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatal(err)
		}
		return m
	default:
		t.Fatal("no frame")
	}
	return nil
}
