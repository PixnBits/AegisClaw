package portalstomp

import "testing"

func TestParseFrameSubscribe(t *testing.T) {
	raw := "SUBSCRIBE\nid:sub-1\ndestination:/topic/channels.main.messages\n\n\x00"
	cmd, headers, body, ok := ParseFrame(raw)
	if !ok || cmd != "SUBSCRIBE" {
		t.Fatalf("parse failed: %v", cmd)
	}
	if headers["destination"] != "/topic/channels.main.messages" {
		t.Fatalf("destination: %v", headers)
	}
	if body != "" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestHubPublishDeliver(t *testing.T) {
	hub := NewHub()
	sess := NewSession(hub)
	sess.HandleFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-1",
		"destination": "/topic/channels.main.messages",
	}, "")
	hub.Publish("/topic/channels.main.messages", []byte(`{"ok":true}`))
	frame := <-sess.Outbound()
	if len(frame) < 7 || frame[:7] != "MESSAGE" {
		t.Fatalf("bad frame %q", frame)
	}
}
