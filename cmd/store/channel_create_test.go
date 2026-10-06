package main

import (
	"strings"
	"testing"
)

func TestHandleChannelCreateIDRule(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	ts := "2026-10-05T00:00:00Z"
	reject := []string{"MyProj", "q4_plan", "v1.2", strings.Repeat("a", 46), ""}
	for _, id := range reject {
		channels := map[string]interface{}{}
		resp := handleChannelCreate(map[string]interface{}{"id": id}, channels, ts)
		if resp.Command != "error" {
			t.Fatalf("id %q: command %q, want error", id, resp.Command)
		}
		msg, _ := resp.Payload.(string)
		if !strings.Contains(msg, "invalid channel id") || !strings.Contains(msg, "<= 45 chars") {
			t.Fatalf("id %q: payload %q", id, msg)
		}
		if len(channels) != 0 {
			t.Fatalf("id %q stored: %#v", id, channels)
		}
	}

	// A name is not an id and is not slugified.
	channels := map[string]interface{}{}
	resp := handleChannelCreate(map[string]interface{}{"name": "My Project"}, channels, ts)
	if resp.Command != "error" || len(channels) != 0 {
		t.Fatalf("name-only create: command %q channels %#v", resp.Command, channels)
	}

	accept := []string{"main", "plan-demo", strings.Repeat("a", 45)}
	for _, id := range accept {
		channels := map[string]interface{}{}
		resp := handleChannelCreate(map[string]interface{}{"id": id}, channels, ts)
		if resp.Command != "channel.created" {
			t.Fatalf("id %q: command %q payload %v", id, resp.Command, resp.Payload)
		}
		if _, ok := channels[id]; !ok {
			t.Fatalf("id %q not stored under that key: %#v", id, channels)
		}
	}
}
