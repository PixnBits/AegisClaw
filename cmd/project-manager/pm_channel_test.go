package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"AegisClaw/internal/agent"
	"AegisClaw/internal/bootargs"
	"AegisClaw/internal/collab"
)

type pmMsg struct {
	From, Content string
}

type pmCheckpoint struct {
	After       int
	ExpectReply bool
	Reason      string
	Form        string
}

type pmConversation struct {
	ID, Title, Channel string
	Messages           []pmMsg
	Checkpoints        []pmCheckpoint
}

func pmck(after int, speak bool, reason, form string) pmCheckpoint {
	return pmCheckpoint{After: after, ExpectReply: speak, Reason: reason, Form: form}
}

func pmconv(id, title, channel, script string, checks ...pmCheckpoint) pmConversation {
	aliases := map[string]string{
		"user":    "user",
		"pm":      "project-manager",
		"secarch": "court-persona-security-architect",
		"arch":    "court-persona-architect",
		"coder":   "court-persona-senior-coder",
		"tester":  "court-persona-tester",
		"eff":     "court-persona-efficiency",
		"ux":      "court-persona-user-advocate",
		"ciso":    "court-persona-ciso",
	}
	var msgs []pmMsg
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		from, content, ok := strings.Cut(line, ":")
		if !ok {
			panic(id + ": bad line: " + line)
		}
		from = strings.TrimSpace(from)
		if a, ok := aliases[from]; ok {
			from = a
		}
		msgs = append(msgs, pmMsg{From: from, Content: strings.TrimSpace(content)})
	}
	return pmConversation{ID: id, Title: title, Channel: channel, Messages: msgs, Checkpoints: checks}
}

func pmFormatBatch(msgs []pmMsg) string {
	maps := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range msgs {
		maps = append(maps, map[string]interface{}{"from": m.From, "content": m.Content})
	}
	return collab.FormatTurnMessages(maps)
}

func pmChannelConversations() []pmConversation {
	return []pmConversation{
		pmconv("css-specialists-talking", "Specialists already have a CSS plan", "main", `
coder: I'll bump the button padding 2px.
tester: I'll re-snapshot the login card.
ux: Looks aligned on desktop.
eff: No perf impact.
`,
			pmck(4, false, "plan and owners already exist", "PASS")),
		pmconv("recap-after-secret", "PM must not recap a handled secret", "ops", `
ciso: Rotate immediately. Secrets VM only. Court if needed.
secarch: Treat the paste as compromised.
tester: I'll grep traces for the key.
coder: Working tree is clean besides the paste.
user: thanks
coder: rebase done
tester: traces clean
`,
			pmck(7, false, "secret already owned; thanks/status only", "PASS")),
		pmconv("pm-mentioned", "Direct @ProjectManager ask", "main", `
coder: CSS PR is up.
tester: snapshots green.
user: @ProjectManager are we done with the login padding or is something still open?
`,
			pmck(3, true, "direct @ProjectManager", "SPEAK — yes/no on remaining work, do not recap the whole thread")),
		pmconv("specialist-asks-pm", "Specialist @ProjectManager with a blocker", "auth", `
coder: @ProjectManager egress to accounts.google.com is denied and we cannot proceed.
tester: Live IdP is blocked.
`,
			pmck(2, true, "specialist @ProjectManager with a blocker", "SPEAK — next step is Court proposal for IdP egress")),
		pmconv("birthday-chatter", "Social request with no engineering work", "main", `
user: I would like to plan a birthday party for a to-be seven year old boy.
ciso: No security fact here.
tester: Not a test plan either.
ux: Cute theme is the user's call.
`,
			pmck(4, false, "off-topic social; no owners needed", "PASS")),
		pmconv("thanks-status", "Thanks after owners already assigned", "main", `
coder: pushed.
tester: snapshots green.
user: thanks, that looks good
`,
			pmck(3, false, "thanks only", "PASS")),
		pmconv("court-already-running", "Court already owns the secret", "ops", `
ciso: Rotate the leaked key. Do not keep using it.
secarch: Isolation is unchanged; this is a credential incident.
arch: No module-boundary change.
`,
			pmck(3, false, "Court already covering; PM would only recap", "PASS")),
		pmconv("implementation-nits", "Implementation chatter needs no PM", "main", `
coder: Renaming the helper.
tester: No new failures on that push.
ux: Copy still reads fine.
`,
			pmck(3, false, "implementation nits; owners already coding", "PASS")),
	}
}

func TestPMChannelConversationFixtures(t *testing.T) {
	convs := pmChannelConversations()
	if len(convs) < 8 {
		t.Fatalf("expected several PM conversations, got %d", len(convs))
	}
	ids := map[string]bool{}
	speak, pass := 0, 0
	for _, c := range convs {
		if c.ID == "" || ids[c.ID] {
			t.Fatalf("bad id %q", c.ID)
		}
		ids[c.ID] = true
		if len(c.Messages) < 1 {
			t.Errorf("%s too short", c.ID)
		}
		if len(c.Checkpoints) < 1 {
			t.Errorf("%s missing checkpoints", c.ID)
		}
		prev := 0
		for _, cp := range c.Checkpoints {
			if cp.After <= prev || cp.After > len(c.Messages) {
				t.Errorf("%s checkpoint after=%d invalid", c.ID, cp.After)
			}
			if cp.ExpectReply {
				speak++
			} else {
				pass++
			}
			prev = cp.After
		}
	}
	if speak < 2 || pass < 4 {
		t.Fatalf("need mixed SPEAK/PASS coverage, speak=%d pass=%d", speak, pass)
	}
}

func TestPMPlanPromptForbidsEcho(t *testing.T) {
	p := getPMPlanPrompt()
	for _, needle := range []string{"Output ONLY the plan", "Never repeat", "Assign only the roles", "@Coder", "@CISO"} {
		if !strings.Contains(p, needle) {
			t.Errorf("plan prompt missing %q", needle)
		}
	}
	for _, leak := range []string{"paranoid-isolated", "You are the Project Manager in AegisClaw", "EnsureRoleAgent", "First line MUST be PASS"} {
		if strings.Contains(p, leak) {
			t.Errorf("plan prompt must not contain %q", leak)
		}
	}
	ch := getPMChannelPrompt()
	if !strings.Contains(ch, "PASS") || !strings.Contains(ch, "SPEAK") {
		t.Fatal("channel prompt must teach PASS/SPEAK")
	}
	if strings.Contains(ch, "paranoid-isolated") || strings.Contains(ch, "Firecracker") {
		t.Fatal("channel prompt must not dump architecture")
	}
}

func assertPMPostClean(t *testing.T, label, posted string) {
	t.Helper()
	if posted == "" {
		t.Fatalf("%s: empty post", label)
	}
	if looksLikePromptEcho(posted) {
		t.Fatalf("%s: prompt echo leaked into post: %s", label, posted)
	}
	first := strings.ToUpper(strings.TrimSpace(strings.Split(posted, "\n")[0]))
	first = strings.TrimRight(strings.Trim(first, "`*_ "), ".!:")
	if first == "SPEAK" || first == "PASS" || first == "NO_REPLY" {
		t.Fatalf("%s: control token left in post: %s", label, posted)
	}
}

func TestSanitizePMPostCatchesRegurgitationShapes(t *testing.T) {
	fb := generatePlan("css padding", "main")
	echoes := []string{
		getPMPrompt(),
		getPMPlanPrompt() + "\nPlan: do the thing",
		"You are the Project Manager in AegisClaw's paranoid-isolated system. Untrusted components run in dedicated Firecracker microVM sandboxes.\nPlan: 1. Analyze",
		"SPEAK\n" + getPMPrompt(),
		strings.Repeat("You orchestrate via ensure.role and channel plans. ", 40),
		"Role: Project Manager. Task: write the plan that will be posted in the channel.\nRules:\n- Output ONLY the plan",
	}
	for i, raw := range echoes {
		got := sanitizePMPost(raw, fb)
		if looksLikePromptEcho(got) {
			t.Errorf("echo %d still leaked: %s", i, got)
		}
		assertPMPostClean(t, fmt.Sprintf("echo-%d", i), got)
	}
}

func TestSanitizePMChannelReplyDropsEcho(t *testing.T) {
	content, skip := sanitizePMChannelReply("PASS")
	if !skip || content != "" {
		t.Fatalf("PASS should skip, got skip=%v content=%q", skip, content)
	}
	content, skip = sanitizePMChannelReply("SPEAK\n@Coder owns the padding.")
	if skip || !strings.Contains(content, "@Coder") {
		t.Fatalf("SPEAK body should post, got skip=%v content=%q", skip, content)
	}
	_, skip = sanitizePMChannelReply(getPMPrompt())
	if !skip {
		t.Fatal("architecture dump must not post on a channel turn")
	}
	_, skip = sanitizePMChannelReply("You are the Project Manager in AegisClaw's paranoid-isolated system. Untrusted components run in dedicated Firecracker microVM sandboxes.")
	if !skip {
		t.Fatal("live #main regurgitation shape must be dropped")
	}
}

func TestPMChannelConversationsLive(t *testing.T) {
	if os.Getenv("AEGIS_LIVE_LLM") != "1" && os.Getenv("AEGIS_PM_LIVE") != "1" {
		t.Skip("set AEGIS_LIVE_LLM=1 to run PM live Ollama tests")
	}
	model := bootargs.PMModel(agent.DefaultPMModel)
	if err := pmOllamaReady(model); err != nil {
		t.Fatalf("Ollama not ready: %v", err)
	}

	fail := 0
	for _, c := range pmChannelConversations() {
		prev := 0
		for _, cp := range c.Checkpoints {
			batch := c.Messages[prev:cp.After]
			batchText := pmFormatBatch(batch)
			mentioned := collab.IsMentioned("project-manager", batchText)
			prompt := getPMChannelPrompt() + "\n\nChannel turn in " + c.Channel + ":\n" + batchText
			expect := cp.ExpectReply
			reason := cp.Reason
			if mentioned {
				prompt += "\n\nYou were directly @mentioned. First line MUST be SPEAK. One sentence is enough if no new plan is needed."
				expect = true
				reason = "direct @ProjectManager in this batch"
			} else {
				prompt += "\n\nYou were not @mentioned. PASS is the default. SPEAK if a new goal still needs owners, work is blocked with no next step, or Court escalation is missing. If PASS, output only PASS."
			}
			raw, err := pmCallOllama(model, prompt)
			if err != nil {
				t.Errorf("%s after=%d ollama: %v", c.ID, cp.After, err)
				fail++
				prev = cp.After
				continue
			}
			content, skip := sanitizePMChannelReply(raw)
			if looksLikePromptEcho(raw) {
				t.Errorf("%s after=%d regurgitated prompt: %q", c.ID, cp.After, raw)
				fail++
			}
			if expect && skip {
				t.Errorf("%s after=%d expected SPEAK (%s) got PASS raw=%q", c.ID, cp.After, reason, raw)
				fail++
			}
			if !expect && !skip {
				t.Errorf("%s after=%d expected PASS (%s) posted %q", c.ID, cp.After, reason, content)
				fail++
			}
			if !skip {
				assertPMPostClean(t, c.ID, content)
			}
			t.Logf("[%s after=%d expect=%v skip=%v] %s raw=%q post=%q", c.ID, cp.After, expect, skip, reason, raw, content)
			prev = cp.After
		}
	}
	if fail > 0 {
		t.Fatalf("PM channel live failures: %d", fail)
	}
}

func TestPMPlanGenerationLive(t *testing.T) {
	if os.Getenv("AEGIS_LIVE_LLM") != "1" && os.Getenv("AEGIS_PM_LIVE") != "1" {
		t.Skip("set AEGIS_LIVE_LLM=1 to run PM live Ollama tests")
	}
	model := bootargs.PMModel(agent.DefaultPMModel)
	if err := pmOllamaReady(model); err != nil {
		t.Fatalf("Ollama not ready: %v", err)
	}

	cases := []struct {
		id      string
		goal    string
		must    []string
		mustNot []string
	}{
		{
			id:      "css-padding",
			goal:    "The login button padding is 2px off. Please tweak CSS only — no security work.",
			must:    []string{"coder"},
			mustNot: []string{"court scribe", "unanimous", "firecracker", "you are the project manager", "paranoid"},
		},
		{
			id:      "leaked-key",
			goal:    "Debugging OAuth. Here is the live key that fails: sk-live-39f8c1d0e7ab99deadbeef1234 — can we keep using it?",
			must:    []string{"rotat"},
			mustNot: []string{"fine to keep", "keep using it as-is", "firecracker", "you are the project manager", "paranoid"},
		},
		{
			id:      "birthday",
			goal:    "I would like to plan a birthday party for a to-be seven year old boy.",
			mustNot: []string{"court scribe", "firecracker", "you are the project manager", "ensure.role", "network boundary", "coder", "tester"},
		},
		{
			id:      "thanks-only",
			goal:    "Thanks, that looks good.",
			mustNot: []string{"court scribe", "firecracker", "you are the project manager", "ensure.role", "@coder", "@tester"},
		},
		{
			id:      "oauth-feature",
			goal:    "Add Google OAuth login to the portal.",
			must:    []string{"coder"},
			mustNot: []string{"firecracker", "you are the project manager", "paranoid-isolated"},
		},
		{
			id:      "stuck-oauth",
			goal:    "We are stuck. OAuth callback is ready but egress to accounts.google.com is denied.",
			must:    []string{"court"},
			mustNot: []string{"firecracker", "you are the project manager", "paranoid-isolated"},
		},
	}

	fail := 0
	for _, tc := range cases {
		prompt := getPMPlanPrompt() + "\n\nUser goal: " + tc.goal + "\n\nChannel: main\n\nPlan:"
		raw, err := pmCallOllama(model, prompt)
		if err != nil {
			t.Errorf("%s: ollama: %v", tc.id, err)
			fail++
			continue
		}
		posted := sanitizePMPost(raw, generatePlan(tc.goal, "main"))
		t.Logf("[%s] raw=%q\nposted=%q", tc.id, raw, posted)
		if looksLikePromptEcho(raw) {
			t.Errorf("%s: model regurgitated the plan prompt: %s", tc.id, raw)
			fail++
		}
		assertPMPostClean(t, tc.id, posted)
		lower := strings.ToLower(posted)
		for _, n := range tc.must {
			if !strings.Contains(lower, n) {
				t.Errorf("%s: posted plan missing %q: %s", tc.id, n, posted)
				fail++
			}
		}
		for _, n := range tc.mustNot {
			if strings.Contains(lower, n) {
				t.Errorf("%s: posted plan contains forbidden %q: %s", tc.id, n, posted)
				fail++
			}
		}
		for _, role := range extractRolesFromText(posted) {
			if tc.id == "birthday" || tc.id == "thanks-only" {
				t.Errorf("%s: should not ensure role %s from %s", tc.id, role, posted)
				fail++
			}
			if tc.id == "css-padding" && role != "coder" && role != "tester" {
				t.Errorf("%s: unexpected role %s from %s", tc.id, role, posted)
				fail++
			}
		}
	}
	if fail > 0 {
		t.Fatalf("PM plan live failures: %d", fail)
	}
}

func pmOllamaReady(model string) error {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("http://127.0.0.1:11434/api/tags")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("tags %d: %s", resp.StatusCode, body)
	}
	if model != "" && !strings.Contains(string(body), strings.Split(model, ":")[0]) {
		return fmt.Errorf("model %s not listed", model)
	}
	return nil
}

func pmCallOllama(model, prompt string) (string, error) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
		"think":  false,
		"options": map[string]interface{}{
			"temperature": 0.2,
			"num_predict": 280,
		},
	})
	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Post("http://127.0.0.1:11434/api/generate", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("generate %d: %s", resp.StatusCode, body)
	}
	var parsed struct {
		Response string `json:"response"`
		Thinking string `json:"thinking"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	text := strings.TrimSpace(parsed.Response)
	if text == "" {
		text = strings.TrimSpace(parsed.Thinking)
	}
	return text, nil
}
