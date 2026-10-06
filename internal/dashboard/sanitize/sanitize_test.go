package sanitize

import (
	"encoding/json"
	"regexp"
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

// Fake keys in the shapes OpenAI and Anthropic issue. They're built at run
// time so secret scanners don't flag the test file.
func scopedTestKeys() (proj, ant, svc, admin string) {
	proj = "sk-" + "proj-" + "AbC_12-" + strings.Repeat("Xy9_Zq-0", 12) + "T3BlbkFJ"
	ant = "sk-" + "ant-" + "api03-" + strings.Repeat("Qw_E-rT9", 11) + "-AA"
	svc = "sk-" + "svcacct-" + strings.Repeat("Lm_N-0p", 6)
	admin = "sk-" + "admin-" + strings.Repeat("aB3-_c", 6)
	return
}

func TestCredentialPatternScopedKeys(t *testing.T) {
	proj, ant, svc, admin := scopedTestKeys()
	for _, key := range []string{proj, ant, svc, admin} {
		cases := map[string]string{
			key:                           "[REDACTED]",
			"api_key_" + key:              "api_key_[REDACTED]",
			"ANTHROPIC_KEY_" + key + " x": "ANTHROPIC_KEY_[REDACTED] x",
			"x=" + key:                    "x=[REDACTED]",
			"key:" + key + ".":            "key:[REDACTED].",
			`"` + key + `"`:               `"[REDACTED]"`,
			`{"api":"` + key + `","n":1}`: `{"api":"[REDACTED]","n":1}`,
			"use (" + key + ") here":      "use ([REDACTED]) here",
			key + " " + key:               "[REDACTED] [REDACTED]",
			`log\n` + key:                 `log\n[REDACTED]`,
			"k%3D" + key:                  "k%3D[REDACTED]",
			"plan2" + key:                 "plan2[REDACTED]",
		}
		for in, want := range cases {
			if got := Text(ContextChat, in); got != want {
				t.Errorf("Text(%q) = %q, want %q", in, got, want)
			}
		}
		for _, in := range []string{"Authorization: Bearer " + key, "OPENAI_API_KEY=" + key, "token " + key} {
			got := Text(ContextChat, in)
			if strings.Contains(got, key[len(key)-16:]) || !strings.Contains(got, "[REDACTED]") {
				t.Errorf("Text(%q) = %q, key body leaked", in, got)
			}
		}
	}
}

// Plain slugs and words are left alone, including ones with a scoped
// prefix, because their bodies are single-case.
func TestCredentialPatternScopedKeysLeaveSlugsAlone(t *testing.T) {
	kept := []string{
		"sk-learn-pipeline-config",
		"sk-learn-pipeline-config-for-the-quarterly-forecast",
		"sk-short",
		"sk-proj-short",
		"sk-ant-api03-tiny",
		"task-proj-refactorauthenticationmodule-v2",
		"TASK-PROJ-REFACTORAUTHENTICATIONMODULE-V2",
		"desk-ant-reorganization_planning_notes",
		"risk-admin-assessment-for-the-quarterly-plan",
		"see sk-proj-roadmap-planning-notes-q4 for details",
		"the sk- prefix is documented",
	}
	for _, in := range kept {
		if got := Text(ContextChat, in); got != in {
			t.Errorf("Text(%q) = %q, want unchanged", in, got)
		}
	}
}

// mainText is Text's credential handling exactly as on main (aec8b51):
// the API-key pattern, then main's credential pattern. The rest of Text is
// unchanged by this PR.
var mainCredentialPattern = regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16}|sk-[a-zA-Z0-9]{20,})`)

func mainText(s string) string {
	s = apiKeyPattern.ReplaceAllString(s, "$1: "+redacted)
	return mainCredentialPattern.ReplaceAllString(s, redacted)
}

// Tester's matrix style: every key shape in every context. Each cell is
// "leaks" when the last 16 characters of a key survive. This PR must never
// leak where main didn't.
func TestCredentialPatternMatrixNeverWeakerThanMain(t *testing.T) {
	proj, ant, svc, admin := scopedTestKeys()
	keys := map[string]string{
		"sk":      "sk-" + strings.Repeat("a1B2", 6),
		"SK":      "SK-" + strings.Repeat("A1B2", 6),
		"sk-lc":   "sk-" + strings.Repeat("a1b2", 6),
		"AKIA":    "AKIAABCDEFGHIJKLMNOP",
		"proj":    proj,
		"ant":     ant,
		"svcacct": svc,
		"admin":   admin,
	}
	contexts := []func(k string) string{
		func(k string) string { return k },
		func(k string) string { return "x " + k },
		func(k string) string { return k + " trailing" },
		func(k string) string { return "api_key_" + k },
		func(k string) string { return "OPENAI_KEY_" + k },
		func(k string) string { return "AWS_" + k },
		func(k string) string { return "x" + k },
		func(k string) string { return "X" + k },
		func(k string) string { return "1" + k },
		func(k string) string { return "id1" + k },
		func(k string) string { return "plan2" + k },
		func(k string) string { return `msg\n` + k },
		func(k string) string { return `{"log":"retrying\n` + k + `"}` },
		func(k string) string { return "line1\r\n" + k },
		func(k string) string { return "k%3D" + k },
		func(k string) string { return "https://x.example/cb?key=" + k + "&a=1" },
		func(k string) string { return "key=" + k },
		func(k string) string { return "key: " + k },
		func(k string) string { return "k:" + k },
		func(k string) string { return `"` + k + `"` },
		func(k string) string { return "'" + k + "'" },
		func(k string) string { return "(" + k + ")" },
		func(k string) string { return "<" + k + ">" },
		func(k string) string { return `{"key":"` + k + `"}` },
		func(k string) string { return "Authorization: Bearer " + k },
		func(k string) string { return "token " + k },
		func(k string) string { return "OPENAI_API_KEY=" + k },
		func(k string) string { return "first\n" + k + "\n" + k },
		func(k string) string { return k + " " + k },
		func(k string) string { return k + "," + k },
		func(k string) string { return k + "_" + k },
		func(k string) string { return k + "-" + k },
		func(k string) string { return k + "." + k },
		func(k string) string { return k + k },
		func(k string) string { return "AKIAABCDEFGHIJKLMNOP" + k },
		func(k string) string { return "sk-" + strings.Repeat("q", 18) + k },
		func(k string) string { return "\t" + k + ";" },
	}
	cells, weaker, stronger := 0, 0, 0
	for name, key := range keys {
		frag := key[len(key)-16:]
		for ci, ctx := range contexts {
			in := ctx(key)
			cells++
			oldLeak := strings.Contains(mainText(in), frag)
			newLeak := strings.Contains(Text(ContextChat, in), frag)
			if newLeak && !oldLeak {
				weaker++
				t.Errorf("weaker than main: %s ctx %d: %q -> %q (main: %q)", name, ci, in, Text(ContextChat, in), mainText(in))
			}
			if oldLeak && !newLeak {
				stronger++
			}
		}
	}
	t.Logf("%d cells: %d weaker than main, %d stronger", cells, weaker, stronger)
	if cells != len(keys)*len(contexts) || weaker != 0 {
		t.Fatalf("cells=%d weaker=%d", cells, weaker)
	}
	// The scoped shapes are the point of this change: none may leak now.
	for _, name := range []string{"proj", "ant", "svcacct", "admin"} {
		key := keys[name]
		for ci, ctx := range contexts {
			if in := ctx(key); strings.Contains(Text(ContextChat, in), key[len(key)-16:]) {
				t.Errorf("%s still leaks in ctx %d: %q", name, ci, Text(ContextChat, in))
			}
		}
	}
}

// The scoped pass runs after main's pattern. Run first, it would replace a
// scoped key glued after an sk- key with "[REDACTED]", cutting the sk- key
// below 20 characters so main's pattern no longer matches it.
func TestScopedPassRunsAfterMainPattern(t *testing.T) {
	proj, _, _, _ := scopedTestKeys()
	short := "sk-" + strings.Repeat("a1B2", 4) + "cD" // 18 alnum; main matches it plus the next "sk"
	in := short + proj
	got := Text(ContextChat, in)
	if strings.Contains(got, short[3:]) || strings.Contains(got, proj[len(proj)-16:]) {
		t.Fatalf("Text(%q) = %q, leaks", in, got)
	}
	if strings.Contains(mainText(in), short[3:]) {
		t.Fatalf("fixture no longer exercises the ordering: main leaks too")
	}
}
