package main

import "testing"

func TestPortalChannelFanout(t *testing.T) {
	got, err := portalChannelFanout(map[string]interface{}{
		"channel_id": "main",
		"from":       "user",
		"content":    "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", got)
	}
	if m["status"] != "fanout_started" {
		t.Fatalf("status=%v", m["status"])
	}

	skipped, err := portalChannelFanout(map[string]interface{}{
		"channel_id": "main",
		"from":       "project-manager-main",
		"content":    "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sm, _ := skipped.(map[string]interface{})
	if sm["status"] != "skipped" {
		t.Fatalf("expected skipped for agent poster, got %v", sm["status"])
	}
}

func TestCollectSecurityPostureForPortal(t *testing.T) {
	posture := collectSecurityPostureForPortal()
	if posture["indicators"] == nil {
		t.Fatal("expected indicators")
	}
	if posture["updated_at"] == nil {
		t.Fatal("expected updated_at")
	}
}
