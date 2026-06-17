package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type harnessRecord struct {
	PlanID    string
	ChannelID string
	Goal      string
	CreatedAt time.Time
	Status    string
}

var (
	harnessMu      sync.RWMutex
	harnessByCh    = map[string]*harnessRecord{}
	defaultStages  = []map[string]string{
		{"name": "Plan", "status": "in_progress"},
		{"name": "Delegate", "status": "pending"},
		{"name": "Execute", "status": "pending"},
		{"name": "Propose", "status": "pending"},
		{"name": "Court Review", "status": "pending"},
		{"name": "Apply", "status": "pending"},
	}
)

func portalGoalSubmit(payload interface{}) (map[string]interface{}, error) {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("goal.submit: invalid payload")
	}
	goal := payloadString(m["goal"])
	if goal == "" {
		return nil, fmt.Errorf("goal.submit: goal required")
	}
	chID := payloadString(m["channel_id"])
	if chID == "" {
		chID = "main"
	}
	planID := "plan_" + chID
	now := time.Now().UTC()

	harnessMu.Lock()
	harnessByCh[chID] = &harnessRecord{
		PlanID:    planID,
		ChannelID: chID,
		Goal:      goal,
		CreatedAt: now,
		Status:    "active",
	}
	harnessMu.Unlock()

	go portalKickoffPMGoal(chID, goal)

	return map[string]interface{}{
		"plan_id":    planID,
		"channel_id": chID,
		"goal":       goal,
		"status":     "accepted",
		"preview":    true,
	}, nil
}

func portalKickoffPMGoal(chID, goalText string) {
	if chData, err := sendToComponentViaHub("store", "channel.get", map[string]string{"id": chID}); err != nil || chData == nil {
		_, _ = sendToComponentViaHub("store", "channel.create", map[string]interface{}{"id": chID})
	}

	ensurePayload := map[string]interface{}{
		"role":    "project-manager",
		"channel": chID,
	}
	roleTarget := "project-manager"
	if sockResp, sockErr := sendSocketRequestWithTimeout("orchestrator.ensure_role", map[string]string{
		"role":    "project-manager",
		"channel": chID,
	}, false, 90*time.Second); sockErr == nil && sockResp.OK && sockResp.Data != nil {
		if idMap, ok := sockResp.Data.(map[string]interface{}); ok {
			if id, ok := idMap["id"].(string); ok && strings.TrimSpace(id) != "" {
				roleTarget = id
			}
		}
	} else if ensureResp, err := sendToComponentViaHubRetry("daemon-orchestrator", "ensure.role", ensurePayload, 30*time.Second); err == nil {
		if idMap, ok := ensureResp.(map[string]interface{}); ok {
			if id, ok := idMap["id"].(string); ok && strings.TrimSpace(id) != "" {
				roleTarget = id
			}
		}
	} else {
		logrus.Warnf("portal goal.submit: ensure PM for %s: %v", chID, err)
	}

	time.Sleep(2 * time.Second)
	goalPayload := map[string]interface{}{
		"goal":    goalText,
		"channel": chID,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, err := sendToComponentViaHubContext(ctx, roleTarget, "user.goal", goalPayload); err != nil {
		logrus.Warnf("portal goal.submit: user.goal to %s: %v", roleTarget, err)
	}
}

func portalHarnessGet(payload interface{}) (map[string]interface{}, error) {
	chID := "main"
	if m, ok := payload.(map[string]interface{}); ok {
		if v := payloadString(m["channel_id"]); v != "" {
			chID = v
		}
	}

	planID := "plan_" + chID
	goal := ""
	createdAt := time.Now().UTC().Format(time.RFC3339)
	status := "active"
	stages := cloneDefaultStages()

	harnessMu.RLock()
	if rec, ok := harnessByCh[chID]; ok && rec != nil {
		planID = rec.PlanID
		goal = rec.Goal
		createdAt = rec.CreatedAt.UTC().Format(time.RFC3339)
		status = rec.Status
	}
	harnessMu.RUnlock()

	tasks := []interface{}{}
	if chData, err := sendToComponentViaHub("store", "channel.get", map[string]string{"id": chID}); err == nil {
		if chMap, ok := chData.(map[string]interface{}); ok {
			enrichHarnessFromChannel(chMap, &goal, &stages, &tasks, planID)
		}
	}

	return map[string]interface{}{
		"plan": map[string]interface{}{
			"plan_id":    planID,
			"channel_id": chID,
			"goal":       goal,
			"created_at": createdAt,
			"status":     status,
			"stages":     stages,
		},
		"tasks": tasks,
	}, nil
}

func enrichHarnessFromChannel(ch map[string]interface{}, goal *string, stages *[]map[string]string, tasks *[]interface{}, planID string) {
	msgs, _ := ch["messages"].([]interface{})
	pmPlanFound := false
	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		from := strings.ToLower(payloadString(msg["from"]))
		content := payloadString(msg["content"])
		if content == "" {
			continue
		}
		if *goal == "" && (from == "user" || strings.HasPrefix(from, "user:")) {
			*goal = content
		}
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		from := strings.ToLower(payloadString(msg["from"]))
		if strings.Contains(from, "project-manager") && payloadString(msg["content"]) != "" {
			pmPlanFound = true
			break
		}
	}
	if pmPlanFound {
		markStageCompleted(stages, "Plan")
		markStageInProgress(stages, "Delegate")
		if len(*tasks) == 0 {
			*tasks = append(*tasks, map[string]interface{}{
				"task_id":       planID + "_task_1",
				"plan_id":       planID,
				"agent_persona": "researcher",
				"scope":         "Execute PM plan tasks for channel",
				"status":        "active",
				"current_stage": "Delegate",
				"progress":      10,
			})
		}
	}
}

func markStageCompleted(stages *[]map[string]string, name string) {
	for i := range *stages {
		if (*stages)[i]["name"] == name {
			(*stages)[i]["status"] = "completed"
		}
	}
}

func markStageInProgress(stages *[]map[string]string, name string) {
	for i := range *stages {
		if (*stages)[i]["name"] == name {
			(*stages)[i]["status"] = "in_progress"
		}
	}
}

func payloadString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func cloneDefaultStages() []map[string]string {
	out := make([]map[string]string, len(defaultStages))
	for i, s := range defaultStages {
		out[i] = map[string]string{"name": s["name"], "status": s["status"]}
	}
	return out
}