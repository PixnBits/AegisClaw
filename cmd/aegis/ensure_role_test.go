package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"AegisClaw/internal/runtime"
)

type fakeEnsureRoler struct {
	n       int
	role    string
	channel string
	id      string
	err     error
}

func (f *fakeEnsureRoler) EnsureRoleAgent(_ context.Context, role, channel string) (string, error) {
	f.n++
	f.role = role
	f.channel = channel
	if f.err != nil {
		return "", f.err
	}
	if f.id != "" {
		return f.id, nil
	}
	return role + "-" + channel, nil
}

func TestHandleEnsureRoleRefusesReserved(t *testing.T) {
	prevOut := logrus.StandardLogger().Out
	prevLevel := logrus.GetLevel()
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(prevOut)
		logrus.SetLevel(prevLevel)
	})

	cases := []struct{ role, channel string }{
		{"court-persona-ciso", ""},
		{"court-persona-ciso", "main"},
		{"court", "persona-ciso"},
		{"store", ""},
		{"network-boundary", ""},
		{"web-portal", ""},
		{"court-scribe", ""},
		{"daemon", ""},
		{"memory", "abc"},
		{"hub", ""},
		{"aegishub", ""},
		{"channel-facilitator", "out"},
		{"aegis-cli-internal", ""},
		{"builder", ""},
	}
	for _, tc := range cases {
		buf.Reset()
		orch := &fakeEnsureRoler{}
		bridges := 0
		_, err := handleEnsureRole(orch, tc.role, tc.channel, func(string) { bridges++ })
		if !errors.Is(err, runtime.ErrReservedRoleID) {
			t.Errorf("handleEnsureRole(%q, %q) err=%v", tc.role, tc.channel, err)
		}
		if orch.n != 0 || bridges != 0 {
			t.Errorf("handleEnsureRole(%q, %q) EnsureRoleAgent=%d bridge=%d", tc.role, tc.channel, orch.n, bridges)
		}
		if ensureRoleAddsChannelMember(err) {
			t.Errorf("handleEnsureRole(%q, %q) must not add channel membership", tc.role, tc.channel)
		}
		if !strings.Contains(buf.String(), "Audit:") {
			t.Errorf("handleEnsureRole(%q, %q) missing Audit warning: %q", tc.role, tc.channel, buf.String())
		}
	}
}

func TestHandleEnsureRoleRefusesUnsafe(t *testing.T) {
	prevOut := logrus.StandardLogger().Out
	prevLevel := logrus.GetLevel()
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(prevOut)
		logrus.SetLevel(prevLevel)
	})

	cases := []struct{ role, channel string }{
		{"Coder", ""},
		{"Coder", "1"},
		{"coder-1", ""},
		{"../x", ""},
		{"coder", "../../../x"},
		{"coder", "Plan-1"},
		{"court-persona-ciso", ""},
		{"court", "persona-ciso"},
		{"store", ""},
		{"network-boundary", ""},
		{"web-portal", ""},
		{"court-scribe", ""},
		{"general", "main"},
		{"sdlc-coder", ""},
		{"senior-coder", "main"},
		{"agent", "../../../x"},
	}
	for _, tc := range cases {
		buf.Reset()
		orch := &fakeEnsureRoler{}
		bridges := 0
		_, err := handleEnsureRole(orch, tc.role, tc.channel, func(string) { bridges++ })
		if !runtime.EnsureRoleRefused(err) {
			t.Errorf("handleEnsureRole(%q, %q) err=%v", tc.role, tc.channel, err)
		}
		if orch.n != 0 || bridges != 0 {
			t.Errorf("handleEnsureRole(%q, %q) EnsureRoleAgent=%d bridge=%d", tc.role, tc.channel, orch.n, bridges)
		}
		if ensureRoleAddsChannelMember(err) {
			t.Errorf("handleEnsureRole(%q, %q) must not add channel membership", tc.role, tc.channel)
		}
		if !strings.Contains(buf.String(), "Audit:") {
			t.Errorf("handleEnsureRole(%q, %q) missing Audit warning: %q", tc.role, tc.channel, buf.String())
		}
	}
}

func TestHandleEnsureRoleCoderChannels(t *testing.T) {
	orch := &fakeEnsureRoler{}
	var bridged string
	id, err := handleEnsureRole(orch, "coder", "1", func(got string) { bridged = got })
	if err != nil {
		t.Fatal(err)
	}
	if id != "coder-1" || bridged != "coder-1" || orch.n != 1 || orch.channel != "1" {
		t.Fatalf("id=%q bridged=%q calls=%d channel=%q", id, bridged, orch.n, orch.channel)
	}
	orch10 := &fakeEnsureRoler{}
	id10, err := handleEnsureRole(orch10, "coder", "10", func(string) {})
	if err != nil || id10 != "coder-10" || id10 == id {
		t.Fatalf("coder-10: id=%q err=%v", id10, err)
	}
}

func TestHandleEnsureRoleStartsNormalRole(t *testing.T) {
	prevOut := logrus.StandardLogger().Out
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	t.Cleanup(func() { logrus.SetOutput(prevOut) })

	orch := &fakeEnsureRoler{id: "coder-plan-1"}
	var bridged string
	id, err := handleEnsureRole(orch, "coder", "plan-1", func(got string) { bridged = got })
	if err != nil {
		t.Fatal(err)
	}
	if id != "coder-plan-1" || bridged != "coder-plan-1" {
		t.Fatalf("id=%q bridged=%q", id, bridged)
	}
	if orch.n != 1 || orch.role != "coder" || orch.channel != "plan-1" {
		t.Fatalf("orch calls=%d role=%q channel=%q", orch.n, orch.role, orch.channel)
	}
	if !ensureRoleAddsChannelMember(err) {
		t.Fatal("successful ensure must be allowed to add membership")
	}
	if strings.Contains(buf.String(), "Audit:") {
		t.Fatalf("normal role logged Audit: %s", buf.String())
	}
}

func TestHandleEnsureRoleStartErrorDoesNotBridge(t *testing.T) {
	orch := &fakeEnsureRoler{err: errors.New("boot failed")}
	bridges := 0
	_, err := handleEnsureRole(orch, "coder", "plan-1", func(string) { bridges++ })
	if err == nil || orch.n != 1 || bridges != 0 {
		t.Fatalf("err=%v calls=%d bridges=%d", err, orch.n, bridges)
	}
	if !ensureRoleAddsChannelMember(err) {
		t.Fatal("non-reserved start errors still add membership on the hub path")
	}
}
