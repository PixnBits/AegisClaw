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
	harnessMu     sync.RWMutex
	harnessByCh   = map[string]*harnessRecord{}
	defaultStages = []map[string]string{
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

	// Post the original goal from the Home page (or CLI) as a user message in the channel.
	// This provides context alongside the PM's structured plan (both are useful).
	_, _ = sendToComponentViaHub("store", "channel.post", map[string]interface{}{
		"channel_id": chID,
		"from":       "user",
		"content":    goalText,
	})

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
	pmPlanContent := latestPMPlanContent(msgs)
	hasProposal := channelHasProposalSignal(msgs)

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

	if pmPlanContent != "" {
		if extracted := extractGoalFromPMPlan(pmPlanContent); extracted != "" {
			*goal = extracted
		} else if *goal == "" {
			if fallback := firstNonHeaderLine(pmPlanContent); fallback != "" {
				*goal = fallback
			}
		}
		markStageCompleted(stages, "Plan")
		markStageInProgress(stages, "Delegate")

		roles := uniqueMemberRoles(ch)
		if len(roles) == 0 {
			roles = rolesFromPlanText(pmPlanContent)
		}
		if len(roles) == 0 {
			roles = []string{"researcher"}
		}

		*tasks = buildHarnessTasks(planID, roles, "Delegate", 15)

		if hasExecutionRoles(roles) {
			markStageCompleted(stages, "Delegate")
			markStageInProgress(stages, "Execute")
			for i := range *tasks {
				if tm, ok := (*tasks)[i].(map[string]interface{}); ok {
					tm["current_stage"] = "Execute"
					tm["progress"] = 35
				}
			}
		}

		if hasProposal {
			markStageCompleted(stages, "Execute")
			markStageCompleted(stages, "Propose")
			markStageInProgress(stages, "Court Review")
			for i := range *tasks {
				if tm, ok := (*tasks)[i].(map[string]interface{}); ok {
					tm["current_stage"] = "Court Review"
					tm["progress"] = 75
				}
			}
		}
	}
}

func latestPMPlanContent(msgs []interface{}) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		from := strings.ToLower(payloadString(msg["from"]))
		content := payloadString(msg["content"])
		if strings.Contains(from, "project-manager") && content != "" {
			if strings.Contains(content, "Plan") || strings.Contains(content, "Goal") || strings.Contains(content, "Tasks") {
				return content
			}
		}
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		from := strings.ToLower(payloadString(msg["from"]))
		content := payloadString(msg["content"])
		if strings.Contains(from, "project-manager") && content != "" {
			return content
		}
	}
	return ""
}

func extractGoalFromPMPlan(content string) string {
	lines := strings.Split(content, "\n")
	inGoal := false
	var goalLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "##") && strings.Contains(lower, "goal") {
			inGoal = true
			continue
		}
		if inGoal {
			if strings.HasPrefix(trimmed, "##") {
				break
			}
			if trimmed != "" {
				goalLines = append(goalLines, trimmed)
			}
		}
	}
	if len(goalLines) > 0 {
		goal := strings.Join(goalLines, " ")
		if len(goal) > 300 {
			return goal[:300] + "…"
		}
		return goal
	}
	return ""
}

func firstNonHeaderLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > 300 {
			return trimmed[:300] + "…"
		}
		return trimmed
	}
	return ""
}

func uniqueMemberRoles(ch map[string]interface{}) []string {
	members, _ := ch["members"].([]interface{})
	seen := map[string]bool{}
	var roles []string
	for _, raw := range members {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role := strings.ToLower(payloadString(m["role"]))
		if role == "" {
			continue
		}
		if strings.Contains(role, "project-manager") {
			continue
		}
		if role == "user" || strings.HasPrefix(role, "user:") {
			continue
		}
		if seen[role] {
			continue
		}
		seen[role] = true
		roles = append(roles, role)
	}
	return roles
}

func rolesFromPlanText(text string) []string {
	roles := []string{}
	lower := strings.ToLower(text)
	candidates := []struct{ key, role string }{
		{"coder", "coder"},
		{"tester", "tester"},
		{"ciso", "ciso"},
		{"security architect", "security-architect"},
		{"architect", "architect"},
		{"researcher", "researcher"},
		{"court", "ciso"},
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if strings.Contains(lower, c.key) && !seen[c.role] {
			seen[c.role] = true
			roles = append(roles, c.role)
		}
	}
	return roles
}

func hasExecutionRoles(roles []string) bool {
	for _, r := range roles {
		switch r {
		case "coder", "tester", "researcher", "ciso", "architect", "senior-coder":
			return true
		}
	}
	return false
}

func channelHasProposalSignal(msgs []interface{}) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		content := strings.ToLower(payloadString(msg["content"]))
		if strings.Contains(content, "proposal") || strings.Contains(content, "court review") {
			return true
		}
	}
	return false
}

func buildHarnessTasks(planID string, roles []string, stage string, progress int) []interface{} {
	tasks := make([]interface{}, 0, len(roles))
	for _, role := range roles {
		tasks = append(tasks, map[string]interface{}{
			"task_id":       planID + "_task_" + role,
			"plan_id":       planID,
			"agent_persona": role,
			"scope":         scopeForHarnessRole(role),
			"status":        "active",
			"current_stage": stage,
			"progress":      progress,
		})
	}
	return tasks
}

func scopeForHarnessRole(role string) string {
	switch role {
	case "coder":
		return "Implement scoped changes per PM plan"
	case "tester":
		return "Validate functionality and edge cases"
	case "ciso":
		return "Review security posture and risks"
	case "researcher":
		return "Research and synthesize findings for the plan"
	default:
		return "Execute narrow task for channel plan (" + role + ")"
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