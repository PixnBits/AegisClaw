package dashboard

import (
	"context"
	"time"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/dashboard/sanitize"
)

func (s *Server) collectHarnessState(ctx context.Context, channelID string) contracts.HarnessState {
	if raw, err := s.fetchRaw(ctx, "harness.get", map[string]string{"channel_id": channelID}); err == nil {
		if m, ok := raw.(map[string]interface{}); ok {
			state := harnessFromMap(m)
			if state.Plan != nil || len(state.Tasks) > 0 {
				return state
			}
		}
	}
	// Presentation-only fallback until PM publishes harness state via bridge.
	now := time.Now().UTC().Format(time.RFC3339)
	return contracts.HarnessState{
		Plan: &contracts.Plan{
			PlanID:    "plan_" + channelID,
			ChannelID: channelID,
			Goal:      "",
			CreatedAt: now,
			Status:    contracts.PlanStatusActive,
			Stages:    contracts.DefaultStages(),
		},
		Tasks: []contracts.NarrowTask{},
	}
}

func harnessFromMap(m map[string]interface{}) contracts.HarnessState {
	var state contracts.HarnessState
	if planRaw, ok := m["plan"].(map[string]interface{}); ok {
		plan := contracts.Plan{
			PlanID:    stringField(planRaw, "plan_id"),
			ChannelID: stringField(planRaw, "channel_id"),
			Goal:      sanitize.Text(sanitize.ContextChat, stringField(planRaw, "goal")),
			CreatedAt: stringField(planRaw, "created_at"),
			Status:    stringField(planRaw, "status"),
		}
		if stages, ok := planRaw["stages"].([]interface{}); ok {
			for _, s := range stages {
				if sm, ok := s.(map[string]interface{}); ok {
					plan.Stages = append(plan.Stages, contracts.StageStatus{
						Name:   stringField(sm, "name"),
						Status: stringField(sm, "status"),
					})
				}
			}
		}
		state.Plan = &plan
	}
	if tasks, ok := m["tasks"].([]interface{}); ok {
		for _, t := range tasks {
			if tm, ok := t.(map[string]interface{}); ok {
				clean := sanitize.JSONMap(sanitize.ContextChat, tm)
				state.Tasks = append(state.Tasks, contracts.NarrowTask{
					TaskID:       stringField(clean, "task_id"),
					PlanID:       stringField(clean, "plan_id"),
					AgentPersona: stringField(clean, "agent_persona"),
					Scope:        stringField(clean, "scope"),
					Status:       stringField(clean, "status"),
					CurrentStage: stringField(clean, "current_stage"),
					Progress:     intField(clean, "progress"),
					LastUpdate:   stringField(clean, "last_update"),
				})
			}
		}
	}
	return state
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func intField(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}