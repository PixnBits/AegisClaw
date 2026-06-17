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
	if stages[1]["status"] != "in_progress" {
		t.Fatalf("stages[1]=%v", stages[1])
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%v", tasks)
	}
}

func TestPortalGoalSubmitRequiresGoal(t *testing.T) {
	_, err := portalGoalSubmit(map[string]interface{}{"channel_id": "main"})
	if err == nil {
		t.Fatal("expected error")
	}
}