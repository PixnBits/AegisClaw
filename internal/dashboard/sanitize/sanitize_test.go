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

// Redaction must never be weaker than main's pattern. A key glued to a
// letter, a digit, an escaped "\n", a %3D or another key is still a key.
// Channel ids are protected one layer up (sanitizeChannelList), not by
// weakening this pattern.
func TestCredentialPatternNeverWeakerThanMain(t *testing.T) {
	key := "sk-" + strings.Repeat("a1B2", 6) // 24 alnum after sk-
	aws := "AKIAABCDEFGHIJKLMNOP"
	aws2 := "AKIAQRSTUVWXYZ234567"
	cases := []string{
		key,
		"token " + key,
		"Authorization: Bearer " + key,
		"OPENAI_API_KEY=" + key,
		`{"key":"` + key + `"}`,
		"api_key_" + key,
		"OPENAI_KEY_" + key,
		"AWS_" + aws,
		`{"log":"retrying\n` + key + `"}`, // raw JSON text: backslash, n, key
		`msg\n` + key,
		"https://x.example/cb?k%3D" + key,
		"plan2" + key,
		"x" + key,
		"1" + key,
		"id1" + key,
		aws + aws2,
		"aws " + aws + " " + aws2,
	}
	for _, in := range cases {
		got := Text(ContextChat, in)
		for _, secret := range []string{key[len(key)-16:], aws[4:], aws2[4:]} {
			if strings.Contains(in, secret) && strings.Contains(got, secret) {
				t.Errorf("Text(%q) = %q, leaks %q", in, got, secret)
			}
		}
	}
	// The glued AKIA pair: both keys go.
	if got := Text(ContextChat, aws+aws2); got != "[REDACTED][REDACTED]" {
		t.Errorf("glued AKIA pair = %q", got)
	}
	// Main's behaviour on text: "sk-" plus 20 letters inside a word is
	// redacted too. Valid channel ids get their raw id back in the channel
	// list instead (see internal/dashboard sanitizeChannelList).
	if got := Text(ContextChat, "channel task-refactorauthenticationmodule"); got != "channel ta[REDACTED]" {
		t.Errorf("Text redaction changed for sk- inside a word: %q", got)
	}
}
