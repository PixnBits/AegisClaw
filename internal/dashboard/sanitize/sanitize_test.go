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
func TestCredentialPatternSeparatorRule(t *testing.T) {
	key := "sk-" + strings.Repeat("a1B2", 6) // 24 alnum after sk-
	aws := "AKIAABCDEFGHIJKLMNOP"
	redacted := []string{
		key,
		"token " + key,
		"Authorization: Bearer " + key,
		"OPENAI_API_KEY=" + key,
		`{"key":"` + key + `"}`,
		"key:" + key + ".",
		"(" + key + ")",
		"aws " + aws,
		"AWS_ACCESS_KEY_ID=" + aws,
	}
	for _, in := range redacted {
		got := Text(ContextChat, in)
		if strings.Contains(got, key) || strings.Contains(got, aws) || !strings.Contains(got, "[REDACTED]") {
			t.Errorf("Text(%q) = %q, want the key redacted", in, got)
		}
	}
	// Glued to a word with '_': still a key, and the prefix stays readable.
	glued := map[string]string{
		"api_key_" + key:         "api_key_[REDACTED]",
		"OPENAI_KEY_" + key:      "OPENAI_KEY_[REDACTED]",
		"AWS_" + aws:             "AWS_[REDACTED]",
		key:                      "[REDACTED]",
		"x=" + key + " y":        "x=[REDACTED] y",
		key + " " + key:          "[REDACTED] [REDACTED]",
		"aws_" + aws + "," + key: "aws_[REDACTED],[REDACTED]",
	}
	for in, want := range glued {
		if got := Text(ContextChat, in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
	kept := []string{
		"task-refactorauthenticationmodule",
		"channel task-refactorauthenticationmodule is ready",
		"desk-reorganizationplanningnotes",
		"risk-assessmentforthequarterlyplan",
	}
	for _, in := range kept {
		if got := Text(ContextChat, in); got != in {
			t.Errorf("Text(%q) = %q, want unchanged (sk- after a letter or digit is not a key)", in, got)
		}
	}
}

// Fake keys in the shapes OpenAI and Anthropic issue. They're built at run
// time so secret scanners don't flag the test file.
func scopedTestKeys() (proj, ant string) {
	proj = "sk-" + "proj-" + "AbC_12-" + strings.Repeat("Xy9_Zq-0", 12) + "T3BlbkFJ"
	ant = "sk-" + "ant-" + "api03-" + strings.Repeat("Qw_E-rT9", 11) + "-AA"
	return proj, ant
}

func TestCredentialPatternScopedKeys(t *testing.T) {
	proj, ant := scopedTestKeys()
	svc := "sk-" + "svcacct-" + strings.Repeat("Lm_N-0p", 6)
	admin := "sk-" + "admin-" + strings.Repeat("aB3-_c", 6)
	for _, key := range []string{proj, ant, svc, admin} {
		cases := map[string]string{
			key:                             "[REDACTED]",
			"api_key_" + key:                "api_key_[REDACTED]",
			"ANTHROPIC_KEY_" + key + " end": "ANTHROPIC_KEY_[REDACTED] end",
			"x=" + key:                      "x=[REDACTED]",
			"key:" + key + ".":              "key:[REDACTED].",
			`"` + key + `"`:                 `"[REDACTED]"`,
			`{"api":"` + key + `","n":1}`:   `{"api":"[REDACTED]","n":1}`,
			"use (" + key + ") here":        "use ([REDACTED]) here",
			key + " " + key:                 "[REDACTED] [REDACTED]",
		}
		for in, want := range cases {
			if got := Text(ContextChat, in); got != want {
				t.Errorf("Text(%q) = %q, want %q", in, got, want)
			}
		}
		// The apiKeyPattern also fires after "Bearer "/"token="; either way
		// no part of the key body may survive.
		for _, in := range []string{"Authorization: Bearer " + key, "OPENAI_API_KEY=" + key, "token " + key} {
			got := Text(ContextChat, in)
			if strings.Contains(got, key[8:20]) || !strings.Contains(got, "[REDACTED]") {
				t.Errorf("Text(%q) = %q, key body leaked", in, got)
			}
		}
	}
}

func TestCredentialPatternScopedKeysLeaveIDsAlone(t *testing.T) {
	kept := []string{
		"sk-short",
		"sk-proj-short",
		"sk-ant-api03-tiny",
		"task-proj-refactorauthenticationmodule-v2",
		"desk-ant-reorganizationplanning_notes",
		"risk-assessmentforthequarterlyplan",
		"task-refactorauthenticationmodule",
		"ask-proj-" + strings.Repeat("x_", 15),
		"the sk- prefix is documented",
	}
	for _, in := range kept {
		if got := Text(ContextChat, in); got != in {
			t.Errorf("Text(%q) = %q, want unchanged", in, got)
		}
	}
}
