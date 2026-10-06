package main

import (
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestInvalidChannelIDs(t *testing.T) {
	list := []interface{}{
		map[string]interface{}{"id": "main"},
		map[string]interface{}{"id": "MyProj"},
		"not-a-channel",
		map[string]interface{}{"id": "plan-demo"},
		map[string]interface{}{"id": "q4_plan"},
		map[string]interface{}{"id": "v1.2"},
		map[string]interface{}{"name": "missing-id"},
		map[string]interface{}{"id": "trailing-"},
		map[string]interface{}{"id": "has--dash"},
		map[string]interface{}{"id": strings.Repeat("a", 46)},
		map[string]interface{}{"id": strings.Repeat("b", 45)},
	}
	got := invalidChannelIDs(list)
	want := []string{"MyProj", "q4_plan", "v1.2", "", "trailing-", "has--dash", strings.Repeat("a", 46)}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("got[%d].ID=%q want %q", i, got[i].ID, id)
		}
		if got[i].Err == nil || !strings.Contains(got[i].Err.Error(), "invalid channel id") {
			t.Errorf("id %q err=%v", id, got[i].Err)
		}
	}
	if invalidChannelIDs(nil) != nil {
		t.Fatal("nil list should report nothing")
	}
	if len(invalidChannelIDs([]interface{}{map[string]interface{}{"id": "main"}})) != 0 {
		t.Fatal("valid id was reported")
	}
}

type logMsgHook struct {
	msgs []string
}

func (h *logMsgHook) Levels() []logrus.Level { return logrus.AllLevels }

func (h *logMsgHook) Fire(e *logrus.Entry) error {
	h.msgs = append(h.msgs, e.Message)
	return nil
}

func TestLogInvalidChannelIDs(t *testing.T) {
	prevOut := logrus.StandardLogger().Out
	prevLevel := logrus.GetLevel()
	logrus.SetOutput(io.Discard)
	logrus.SetLevel(logrus.WarnLevel)
	hook := &logMsgHook{}
	oldHooks := logrus.StandardLogger().ReplaceHooks(logrus.LevelHooks{})
	logrus.AddHook(hook)
	t.Cleanup(func() {
		logrus.SetOutput(prevOut)
		logrus.SetLevel(prevLevel)
		logrus.StandardLogger().ReplaceHooks(oldHooks)
	})

	logInvalidChannelIDs([]interface{}{
		map[string]interface{}{"id": "main"},
		map[string]interface{}{"id": "MyProj"},
	})
	want := `channel "MyProj" has an invalid id (invalid channel id: must match ^[a-z][a-z0-9-]*$ and be <= 45 chars); agents cannot be started for it. Create a new channel with a valid id and move the work there.`
	if len(hook.msgs) != 1 || hook.msgs[0] != want {
		t.Fatalf("logs %#v", hook.msgs)
	}

	hook.msgs = nil
	logInvalidChannelIDs(nil)
	if len(hook.msgs) != 0 {
		t.Fatalf("empty list logged %#v", hook.msgs)
	}
}

// TestInspectStartupChannelList covers the startup call site: the pass over
// channel.list that setupDefaultMainChannelAndMembers runs must warn about
// stranded ids and still find "main".
func TestInspectStartupChannelList(t *testing.T) {
	prevOut := logrus.StandardLogger().Out
	prevLevel := logrus.GetLevel()
	logrus.SetOutput(io.Discard)
	logrus.SetLevel(logrus.WarnLevel)
	hook := &logMsgHook{}
	oldHooks := logrus.StandardLogger().ReplaceHooks(logrus.LevelHooks{})
	logrus.AddHook(hook)
	t.Cleanup(func() {
		logrus.SetOutput(prevOut)
		logrus.SetLevel(prevLevel)
		logrus.StandardLogger().ReplaceHooks(oldHooks)
	})

	hasMain := inspectStartupChannelList([]interface{}{
		map[string]interface{}{"id": "MyProj"},
		map[string]interface{}{"id": "main"},
	})
	if !hasMain {
		t.Fatal("main not found")
	}
	if len(hook.msgs) != 1 || !strings.Contains(hook.msgs[0], `channel "MyProj" has an invalid id`) {
		t.Fatalf("startup warnings %#v", hook.msgs)
	}

	hook.msgs = nil
	if inspectStartupChannelList([]interface{}{map[string]interface{}{"id": "plan-demo"}}) {
		t.Fatal("hasMain without main")
	}
	if len(hook.msgs) != 0 {
		t.Fatalf("valid list warned %#v", hook.msgs)
	}
	if inspectStartupChannelList("not a list") {
		t.Fatal("non-list reported main")
	}
}
