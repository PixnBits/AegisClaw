package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEnrichHarnessFromChannelPMPlan(t *testing.T) {
	goal := ""
	stages := cloneDefaultStages()
	tasks := []interface{}{}
	ch := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"from": "user", "content": "Research Zig vs Rust"},
			map[string]interface{}{"from": "project-manager", "content": "Plan: task 1 delegate to researcher"},
		},
	}
	enrichHarnessFromChannel(ch, &goal, &stages, &tasks, "plan_main")
	if goal != "Research Zig vs Rust" {
		t.Fatalf("goal=%q", goal)
	}
	if stages[0]["status"] != "completed" || stages[0]["name"] != "Plan" {
		t.Fatalf("stages[0]=%v", stages[0])
	}
	if stages[1]["status"] != "completed" {
		t.Fatalf("delegate stage=%v", stages[1])
	}
	if stages[2]["status"] != "in_progress" {
		t.Fatalf("execute stage=%v", stages[2])
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%v", tasks)
	}
}

func TestEnrichHarnessFromChannelMembersAndGoalSection(t *testing.T) {
	goal := ""
	stages := cloneDefaultStages()
	tasks := []interface{}{}
	ch := map[string]interface{}{
		"members": []interface{}{
			map[string]interface{}{"role": "project-manager"},
			map[string]interface{}{"role": "coder"},
			map[string]interface{}{"role": "tester"},
			map[string]interface{}{"role": "ciso"},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"from":    "project-manager-main",
				"content": "# Project Plan\n\n## Goal\nBuild hello world with tests\n\n## Tasks\nCoder implements; Tester validates.",
			},
		},
	}
	enrichHarnessFromChannel(ch, &goal, &stages, &tasks, "plan_demo")
	if goal != "Build hello world with tests" {
		t.Fatalf("goal=%q", goal)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d: %v", len(tasks), tasks)
	}
	if stages[2]["name"] != "Execute" || stages[2]["status"] != "in_progress" {
		t.Fatalf("execute stage=%v", stages[2])
	}
}

func TestExtractGoalFromPMPlan(t *testing.T) {
	content := "## 🎯 Goal\nCreate a minimal Go hello world\n\n## Tasks\n1. Code"
	got := extractGoalFromPMPlan(content)
	if got != "Create a minimal Go hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestPortalGoalSubmitRequiresGoal(t *testing.T) {
	_, err := portalGoalSubmit(map[string]interface{}{"channel_id": "main"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPortalGoalSubmitRejectsBadChannelAndEnsureRefusal(t *testing.T) {
	saved := portalGoals
	t.Cleanup(func() { portalGoals = saved })
	portalGoals.hub = func(string, string, interface{}) (interface{}, error) {
		t.Fatal("hub called for a refused goal")
		return nil, nil
	}
	portalGoals.boot = func(string) (interface{}, error) {
		t.Fatal("ensure boot called for a refused goal")
		return nil, nil
	}
	portalGoals.wait = func(string, time.Duration) error {
		t.Fatal("wait called for a refused goal")
		return nil
	}

	bad := []string{"MyProj", "q4_plan", "v1.2", strings.Repeat("a", 46)}
	for _, id := range bad {
		resp, err := portalGoalSubmit(map[string]interface{}{"goal": "ship", "channel_id": id})
		if err == nil || resp != nil {
			t.Fatalf("%q accepted: resp=%v err=%v", id, resp, err)
		}
		if !strings.Contains(err.Error(), "invalid channel id") {
			t.Fatalf("%q: %v", id, err)
		}
		harnessMu.Lock()
		_, ok := harnessByCh[id]
		harnessMu.Unlock()
		if ok {
			t.Fatalf("%q left a harness record", id)
		}
	}

	portalGoals.ensure = func(string) error {
		return fmt.Errorf("goal.submit: ensure PM for main: invalid vm id: refused")
	}
	resp, err := portalGoalSubmit(map[string]interface{}{"goal": "ship", "channel_id": "main"})
	if err == nil || resp != nil {
		t.Fatalf("refused ensure accepted: resp=%v err=%v", resp, err)
	}
	if !strings.Contains(err.Error(), "invalid vm id") || strings.Contains(fmt.Sprint(resp), "accepted") {
		t.Fatalf("ensure error not surfaced: resp=%v err=%v", resp, err)
	}
	harnessMu.Lock()
	_, ok := harnessByCh["main"]
	harnessMu.Unlock()
	if ok {
		t.Fatal("refused goal left an accepted harness record")
	}
}

func TestDeliverPortalPMGoalSurfacesEnsureError(t *testing.T) {
	saved := portalGoals
	t.Cleanup(func() { portalGoals = saved })
	portalGoals.hub = func(target, cmd string, payload interface{}) (interface{}, error) {
		if cmd == "channel.post" {
			t.Fatal("posted after a refused ensure")
		}
		return map[string]interface{}{"id": "main"}, nil
	}
	portalGoals.boot = func(string) (interface{}, error) {
		return map[string]interface{}{"error": "invalid vm id: longer than 64 bytes"}, nil
	}
	err := deliverPortalPMGoal("main", "ship")
	if err == nil {
		t.Fatal("expected ensure error, kickoff swallowed it")
	}
	if !strings.Contains(err.Error(), "invalid vm id: longer than 64 bytes") {
		t.Fatalf("ensure error not surfaced: %v", err)
	}
	if strings.Contains(err.Error(), "missing guest id") {
		t.Fatalf("ensure error collapsed to missing guest id: %v", err)
	}
}

func TestPortalGoalSubmitStillAccepted(t *testing.T) {
	saved := portalGoals
	t.Cleanup(func() {
		portalGoals = saved
		harnessMu.Lock()
		delete(harnessByCh, "plan-demo")
		harnessMu.Unlock()
	})
	posted := make(chan struct{})
	portalGoals.hub = func(target, cmd string, payload interface{}) (interface{}, error) {
		if cmd == "channel.post" {
			close(posted)
		}
		return map[string]interface{}{"id": "plan-demo"}, nil
	}
	portalGoals.boot = func(chID string) (interface{}, error) {
		return map[string]interface{}{"id": "project-manager-" + chID}, nil
	}
	portalGoals.wait = func(id string, _ time.Duration) error {
		if id != "project-manager-plan-demo" {
			return fmt.Errorf("wait id %s", id)
		}
		return nil
	}
	resp, err := portalGoalSubmit(map[string]interface{}{"goal": "ship", "channel_id": "plan-demo"})
	if err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "accepted" || resp["channel_id"] != "plan-demo" || resp["plan_id"] != "plan_plan-demo" {
		t.Fatalf("resp=%v", resp)
	}
	select {
	case <-posted:
	case <-time.After(2 * time.Second):
		t.Fatal("success path did not post the goal")
	}
}
