package main

import "testing"

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
				"from": "project-manager-main",
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