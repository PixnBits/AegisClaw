package sanitize

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTextRedactsSecretsAndPaths(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"api_key: sk-live-abc123def456ghi789", "api_key: [REDACTED]"},
		{"read /etc/passwd ok", "[REDACTED] ok"},
		{"host 10.0.0.5 unreachable", "host [REDACTED] unreachable"},
		{"tool: read_file, path: /etc/shadow", "tool: read_file, path: [REDACTED]"},
	}
	for _, tc := range cases {
		got := Text(ContextTrace, tc.in)
		if !strings.Contains(got, "[REDACTED]") && tc.want != got {
			t.Errorf("Text(%q) = %q, want redaction", tc.in, got)
		}
	}
}

func TestTextChatStripsScriptTags(t *testing.T) {
	in := `<script>alert(1)</script> hello`
	got := Text(ContextChat, in)
	if strings.Contains(got, "<script") {
		t.Fatalf("script tag not escaped: %q", got)
	}
}

func TestJSONMapStripsInternalFields(t *testing.T) {
	m := map[string]interface{}{
		"task_id":           "task_1",
		"agent_instance_id": "vm-secret-99",
		"scope":             "Research Zig",
		"nested": map[string]interface{}{
			"api_key": "sk-abcdefghijklmnopqrst",
			"note":    "ok",
		},
	}
	out := JSONMap(ContextTrace, m)
	if _, ok := out["agent_instance_id"]; ok {
		t.Fatal("agent_instance_id must be stripped")
	}
	if out["scope"] != "Research Zig" {
		t.Fatalf("scope: %v", out["scope"])
	}
	nested, ok := out["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("nested missing")
	}
	key, _ := nested["api_key"].(string)
	if !strings.Contains(key, "[REDACTED]") {
		t.Fatalf("api_key not redacted: %v", nested["api_key"])
	}
}

func TestValueSanitizesNestedMaps(t *testing.T) {
	in := map[string]interface{}{
		"content": "password: hunter2 path /etc/shadow",
	}
	out, ok := Value(ContextChat, in).(map[string]interface{})
	if !ok {
		t.Fatal("expected map")
	}
	content, _ := out["content"].(string)
	if !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("content not redacted: %q", content)
	}
}

func TestJSONBytesRoundTrip(t *testing.T) {
	raw := []byte(`{"type":"channel.activity","event":{"from":"user","content":"token=abc123"}}`)
	clean, err := JSONBytes(ContextChat, raw)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(clean, &m); err != nil {
		t.Fatal(err)
	}
	event, ok := m["event"].(map[string]interface{})
	if !ok {
		t.Fatal("event missing")
	}
	content, _ := event["content"].(string)
	if !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("content not sanitized: %q", content)
	}
}