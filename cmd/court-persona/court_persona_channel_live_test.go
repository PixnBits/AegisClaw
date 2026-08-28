package main

import (
	"os"
	"strings"
	"testing"

	"AegisClaw/internal/agent"
	"AegisClaw/internal/bootargs"
	"AegisClaw/internal/collab"
)

func TestCourtPersonaChannelSpeakPassLive(t *testing.T) {
	if os.Getenv("AEGIS_LIVE_LLM") != "1" && os.Getenv("AEGIS_CISO_LIVE") != "1" {
		t.Skip("set AEGIS_LIVE_LLM=1 to run Court persona live Ollama channel tests")
	}
	model := bootargs.DefaultModel(agent.DefaultLLMModel)
	if err := ollamaReady(model); err != nil {
		t.Fatalf("Ollama not ready for model %s: %v", model, err)
	}

	cases := []struct {
		persona string
		batch   string
		speak   bool
	}{
		{"security-architect", "- coder: two agents share one Memory VM socket to save RAM.", true},
		{"security-architect", "- ux: the empty-state copy feels cold.", false},
		{"architect", "- user: can every agent share one Memory VM as a group brain?", true},
		{"architect", "- tester: I'll regenerate Playwright snapshots.", false},
		{"senior-coder", "- coder: encrypt localStorage with a hardcoded key so we can ship Friday.", true},
		{"senior-coder", "- ux: checkbox label should say Keep me signed in.", false},
		{"tester", "- user: drop Firefox from visual polish so CI stays green.", true},
		{"tester", "- pm: standup, no new goals today.", false},
		{"efficiency", "- eff: cut Tester to 192MiB after a 20-review soak with 0 OOM.", true},
		{"efficiency", "- ux: tooltip padding is 1px off.", false},
		{"user-advocate", "- ux: empty state says No activity, which feels like a failure.", true},
		{"user-advocate", "- secarch: cid=3 is probably the hub bridge.", false},
	}

	fail := 0
	for _, tc := range cases {
		src := "court-persona-" + tc.persona
		prompt := buildChannelTurnPrompt(tc.persona, src, "main", tc.batch, "")
		raw, err := callOllamaGenerate(model, prompt)
		if err != nil {
			t.Errorf("%s: ollama error: %v", tc.persona, err)
			fail++
			continue
		}
		content, skip := collab.NormalizeChannelLLMReply(raw)
		spoke := !skip
		if spoke != tc.speak {
			t.Errorf("%s speak=%v want %v raw=%q post=%q", tc.persona, spoke, tc.speak, trimLog(raw), trimLog(content))
			fail++
			continue
		}
		if spoke && strings.Contains(strings.ToLower(raw), "vote:") {
			t.Errorf("%s used VOTE format: %q", tc.persona, trimLog(raw))
			fail++
			continue
		}
		t.Logf("%s speak=%v ok raw=%q", tc.persona, spoke, trimLog(raw))
	}
	t.Logf("Court persona live: %d cases, %d failed, model=%s", len(cases), fail, model)
}
