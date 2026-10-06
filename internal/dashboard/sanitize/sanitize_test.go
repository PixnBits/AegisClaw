package sanitize

import (
	"encoding/json"
	"math/rand"
	"os"
	"regexp"
	"strconv"
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

// noneTestKey is the legacy OpenAI shape: sk-None- and a mixed-case body.
func noneTestKey() string {
	return "sk-" + "None-" + strings.Repeat("aZ3kQ9", 8)
}

func TestCredentialPatternScopedKeys(t *testing.T) {
	proj, ant, svc, admin := scopedTestKeys()
	upperProj := "SK-" + "PROJ-" + proj[len("sk-proj-"):]
	mixedAnt := "Sk-" + "Ant-" + ant[len("sk-ant-"):]
	for _, key := range []string{proj, ant, svc, admin, noneTestKey(), upperProj, mixedAnt} {
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
		// Mixed-case bodies under 20 characters are notes, not keys.
		"sk-ant-MyNotes",
		"sk-proj-ReleasePlanQ4",
		"sk-None-ShortNote",
		// The case check is on the body only: an upper-case slug body after
		// a lower-case prefix is still single-case.
		"sk-proj-ROADMAP-PLANNING-NOTES-FOR-Q4",
		"SK-NONE-RELEASE-PLANNING-NOTES-FOR-Q4",
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
		"none":    noneTestKey(),
		"PROJ":    "SK-" + "PROJ-" + proj[len("sk-proj-"):],
		// Scoped keys whose body contains a run main's pattern matches.
		"proj-embedded-sk":  "sk-" + "proj-" + "Ab_9-" + strings.Repeat("Zq_W-", 6) + "sk-" + strings.Repeat("Rt5Yu7", 4) + "-" + strings.Repeat("Pl_0-", 8),
		"ant-embedded-akia": "sk-" + "ant-" + "api03-" + strings.Repeat("Nm-8_", 6) + "AKIA" + "QWERTYUIOP123456" + strings.Repeat("_Hj-K", 8) + "AA",
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
		func(k string) string { return "AKIA" + "ABCDEFGHIJKLMN" + k },
		func(k string) string { return "AKIA" + k },
		func(k string) string { return k + "sk-" + strings.Repeat("Q", 18) + k },
		func(k string) string { return "\t" + k + ";" },
	}
	cells, weaker, stronger := 0, 0, 0
	for name, key := range keys {
		for ci, ctx := range contexts {
			in := ctx(key)
			cells++
			oldLeak := leaksWindow(mainText(in), keyBody(key))
			newLeak := leaksWindow(Text(ContextChat, in), keyBody(key))
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
	// No shape leaks in any context now, including keys glued after another
	// match (where main leaks).
	for name, key := range keys {
		for ci, ctx := range contexts {
			if in := ctx(key); leaksWindow(Text(ContextChat, in), keyBody(key)) {
				t.Errorf("%s still leaks in ctx %d: %q", name, ci, Text(ContextChat, in))
			}
		}
	}
}

// keyBody is the secret part of a test key: everything after the sk-…-
// or AKIA prefix.
func keyBody(key string) string {
	if m := scopedKeyPattern.FindStringSubmatchIndex(key); m != nil && m[0] == 0 {
		return key[m[2]:]
	}
	if len(key) > 4 && strings.EqualFold(key[:4], "akia") {
		return key[4:]
	}
	if i := strings.Index(key, "-"); i >= 0 {
		return key[i+1:]
	}
	return key
}

// leaksWindow reports whether any 8-character window of secret appears in out.
func leaksWindow(out, secret string) bool {
	for i := 0; i+8 <= len(secret); i++ {
		if strings.Contains(out, secret[i:i+8]) {
			return true
		}
	}
	return false
}

// Both patterns are matched on the same input and the union of their spans
// is redacted: a run main's pattern matches inside a scoped key body must
// not split the key and leave its tail visible, and a short sk- run glued in
// front of a scoped key is still covered.
func TestCredentialSpansUnion(t *testing.T) {
	proj, _, _, _ := scopedTestKeys()
	embeddedSK := "sk-" + "proj-" + "Ab_9-" + strings.Repeat("Zq_W-", 6) + "sk-" + strings.Repeat("Rt5Yu7", 4) + "-" + strings.Repeat("Pl_0-", 8)
	embeddedAKIA := "sk-" + "ant-" + "api03-" + strings.Repeat("Nm-8_", 6) + "AKIA" + "QWERTYUIOP123456" + strings.Repeat("_Hj-K", 8) + "AA"
	for _, key := range []string{embeddedSK, embeddedAKIA} {
		if !credentialPattern.MatchString(keyBody(key)) {
			t.Fatalf("fixture %q has no run main's pattern matches", key)
		}
		for in, want := range map[string]string{
			key:                   "[REDACTED]",
			"x=" + key + ";":      "x=[REDACTED];",
			`{"k":"` + key + `"}`: `{"k":"[REDACTED]"}`,
		} {
			if got := Text(ContextChat, in); got != want {
				t.Errorf("Text(%q) = %q, want %q", in, got, want)
			}
		}
	}
	short := "sk-" + strings.Repeat("a1B2", 4) + "cD" // 18 alnum; main matches it plus the next "sk"
	in := short + proj
	got := Text(ContextChat, in)
	if leaksWindow(got, short[3:]) || leaksWindow(got, keyBody(proj)) {
		t.Fatalf("Text(%q) = %q, leaks", in, got)
	}
	if got != "[REDACTED]" {
		t.Fatalf("Text(glued) = %q, want one marker", got)
	}
	// With no scoped key present the output is exactly main's.
	for _, in := range []string{
		"AKIAABCDEFGHIJKLMNOPAKIAABCDEFGHIJKLMNOP",
		"a sk-" + strings.Repeat("a1B2", 6) + " b sk-" + strings.Repeat("Zz9", 8),
		"sk-proj-roadmap-planning-notes-q4 sk-" + strings.Repeat("a1B2", 6),
	} {
		if got, want := Text(ContextChat, in), mainText(in); got != want {
			t.Errorf("Text(%q) = %q, want main's %q", in, got, want)
		}
	}
}

// A key glued right after another credential match is redacted from its own
// start: after an sk- run of 18 characters (main's match takes the next
// key's "sk" and stops at its "-"), after another sk- key, and after an AKIA
// prefix whose 16 characters run into the next key.
func TestCredentialSpansGluedKeys(t *testing.T) {
	key := "sk-" + strings.Repeat("a1B2", 6)
	key2 := "sk-" + strings.Repeat("Zx9", 9)
	aws := "AKIAABCDEFGHIJKLMNOP"
	for in, want := range map[string]string{
		"sk-" + strings.Repeat("q", 18) + key:   "[REDACTED]",
		key + key2:                              "[REDACTED]",
		"x=" + key + key2 + ";":                 "x=[REDACTED];",
		"AKIA" + "ABCDEFGHIJKLMN" + key:         "[REDACTED]",
		"AKIA" + aws:                            "[REDACTED]",
		key + " and " + key2:                    "[REDACTED] and [REDACTED]",
		"sk-" + strings.Repeat("q", 18) + "-ok": "sk-" + strings.Repeat("q", 18) + "-ok",
	} {
		if got := Text(ContextChat, in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
	// Prefixes cut off at the end of the input are left alone (and must not
	// read past it).
	for _, in := range []string{"a", "ak", "aki", "Akia", "bak", "taki", "s", "sk", "sk-", "risk-", "ask", "AKIA123"} {
		if got := Text(ContextChat, in); got != in {
			t.Errorf("Text(%q) = %q, want unchanged", in, got)
		}
	}
	// Main leaks these; this is the case the extra spans exist for.
	for _, in := range []string{"sk-" + strings.Repeat("q", 18) + key, key + key2, "AKIA" + "ABCDEFGHIJKLMN" + key} {
		if !leaksWindow(mainText(in), keyBody(key)) && !leaksWindow(mainText(in), keyBody(key2)) {
			t.Errorf("fixture %q doesn't leak on main; it doesn't exercise the glued case", in)
		}
	}
}

// OpenRouter keys: sk-or-v1- and 64 hex characters. Shorter hex is left
// alone.
func TestHexKeyShape(t *testing.T) {
	hex := strings.Repeat("0123456789abcdef", 4)
	key := "sk-" + "or-v1-" + hex
	for in, want := range map[string]string{
		key:                                     "[REDACTED]",
		"OPENROUTER_KEY_" + key + " x":          "OPENROUTER_KEY_[REDACTED] x",
		`{"k":"` + key + `"}`:                   `{"k":"[REDACTED]"}`,
		"SK-" + "OR-V1-" + strings.ToUpper(hex): "[REDACTED]",
		key + "abc":                             "[REDACTED]",
		key + "-tail":                           "[REDACTED]-tail",
		"sk-" + "or-v1-" + hex[:63]:             "sk-" + "or-v1-" + hex[:63],
		"sk-" + "or-v1-deadbeef":                "sk-" + "or-v1-deadbeef",
		"sk-" + "or-v2-" + hex:                  "sk-" + "or-v2-" + hex,
	} {
		if got := Text(ContextChat, in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

// apiKeyPattern runs before the credential spans. A key glued in front of
// "token=value" would otherwise take the "token" label into its span, and the
// value after it would be shown.
func TestAPIKeyPassRunsFirst(t *testing.T) {
	proj, _, _, _ := scopedTestKeys()
	plain := "sk-" + strings.Repeat("a1B2", 6)
	value := "hunter2" + "Hunter2" + "zz"
	for _, in := range []string{
		proj + "_token=" + value,
		plain + "token=" + value,
		"x " + proj + "_password: " + value,
	} {
		got := Text(ContextChat, in)
		if strings.Contains(got, value) || leaksWindow(got, keyBody(proj)) || leaksWindow(got, keyBody(plain)) {
			t.Errorf("Text(%q) = %q, leaks", in, got)
		}
	}
}

// Seeded random keys in the issued shapes. No 8-character window of a key
// body may survive Text or Value. SANITIZE_PROPERTY_KEYS sets the count per
// shape (default 20000; the long run uses 200000).
func TestScopedKeyProperty(t *testing.T) {
	n := 20000
	if v := os.Getenv("SANITIZE_PROPERTY_KEYS"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x > 0 {
			n = x
		}
	}
	const b64url = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	const b62 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	shapes := []struct {
		prefix, alphabet, suffix string
		n                        int
	}{
		{"sk-" + "proj-", b64url, "", 156},
		{"sk-" + "ant-" + "api03-", b64url, "AA", 93},
		{"sk-" + "svcacct-", b64url, "", 156},
		{"sk-" + "admin-", b64url, "", 156},
		{"sk-" + "None-", b62, "", 48},
		{"sk-", b62, "", 48},
		{"sk-" + "or-v1-", "0123456789abcdef", "", 64},
	}
	rng := rand.New(rand.NewSource(20261006))
	embedded := 0
	for _, sh := range shapes {
		for i := 0; i < n; i++ {
			buf := make([]byte, sh.n)
			for j := range buf {
				buf[j] = sh.alphabet[rng.Intn(len(sh.alphabet))]
			}
			body := string(buf) + sh.suffix
			key := sh.prefix + body
			if credentialPattern.MatchString(body) {
				embedded++
			}
			var in string
			switch i % 5 {
			case 0:
				in = key
			case 1:
				in = "OPENAI_API_KEY=" + key + "\n"
			case 2:
				in = "call failed for " + key + ", retrying"
			case 3:
				in = "sk-" + strings.Repeat("q", 18) + key
			default:
				in = key + key
			}
			if got := Text(ContextChat, in); leaksWindow(got, body) {
				t.Fatalf("%s key %d leaks: %q -> %q", sh.prefix, i, in, got)
			}
			// Text's apiKeyPattern pass hides the OPENAI_API_KEY= value before
			// the span pass sees it, so check the span pass on its own too.
			if got := redactCredentials(in); leaksWindow(got, body) {
				t.Fatalf("%s key %d leaks through redactCredentials: %q -> %q", sh.prefix, i, in, got)
			}
			if i%50 == 0 {
				out, _ := json.Marshal(Value(ContextChat, map[string]interface{}{"msg": in}))
				if leaksWindow(string(out), body) {
					t.Fatalf("%s key %d leaks through Value: %s", sh.prefix, i, out)
				}
			}
		}
	}
	t.Logf("%d keys per shape, %d with a run main's pattern matches inside the body", n, embedded)
	if n >= 20000 && embedded == 0 {
		t.Fatal("no generated key exercised the overlapping-pattern case")
	}
}
