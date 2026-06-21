package dashboard

import (
	"context"

	"AegisClaw/internal/dashboard/contracts"
)

// publishHarnessDeltas compares fresh harness state with the last snapshot for a
// channel and emits structured harness.* STOMP events (per harness-pipeline-data-model.md).
func (s *Server) publishHarnessDeltas(ctx context.Context, channelID string) {
	if channelID == "" {
		return
	}
	state := s.collectHarnessState(ctx, channelID)
	planID := "plan_" + channelID
	if state.Plan != nil && state.Plan.PlanID != "" {
		planID = state.Plan.PlanID
	}

	s.harnessMu.Lock()
	if s.harnessCache == nil {
		s.harnessCache = make(map[string]contracts.HarnessState)
	}
	prev, hadPrev := s.harnessCache[channelID]
	s.harnessCache[channelID] = state
	s.harnessMu.Unlock()

	pub := s.stompPublisher()

	if state.Plan != nil && state.Plan.Goal != "" {
		emitPlan := !hadPrev || prev.Plan == nil || prev.Plan.Goal != state.Plan.Goal
		if emitPlan {
			pub.PublishHarness(planID, channelID, contracts.HarnessPlanCreated{
				Type:      contracts.TypeHarnessPlanCreated,
				PlanID:    planID,
				ChannelID: channelID,
				Goal:      state.Plan.Goal,
				Stages:    state.Plan.Stages,
			})
		} else if hadPrev && prev.Plan != nil {
			for i, stage := range state.Plan.Stages {
				if i >= len(prev.Plan.Stages) {
					break
				}
				if stage.Status != prev.Plan.Stages[i].Status {
					pub.PublishHarness(planID, channelID, contracts.HarnessStageTransition{
						Type:   contracts.TypeHarnessStageTrans,
						PlanID: planID,
						Stage:  stage.Name,
						Status: stage.Status,
					})
				}
			}
		}
	}

	prevTasks := map[string]contracts.NarrowTask{}
	if hadPrev {
		for _, t := range prev.Tasks {
			prevTasks[t.TaskID] = t
		}
	}
	for _, task := range state.Tasks {
		prevTask, existed := prevTasks[task.TaskID]
		if !existed {
			pub.PublishHarness(planID, channelID, contracts.HarnessTaskAssigned{
				Type:         contracts.TypeHarnessTaskAssigned,
				TaskID:       task.TaskID,
				PlanID:       planID,
				AgentPersona: task.AgentPersona,
				Scope:        task.Scope,
				CurrentStage: task.CurrentStage,
			})
			continue
		}
		if task.Progress != prevTask.Progress ||
			task.CurrentStage != prevTask.CurrentStage ||
			task.Scope != prevTask.Scope {
			summary := task.Scope
			if task.Progress > prevTask.Progress {
				summary = progressSummary(task)
			}
			pub.PublishHarness(planID, channelID, contracts.HarnessTaskProgress{
				Type:         contracts.TypeHarnessTaskProgress,
				TaskID:       task.TaskID,
				Progress:     task.Progress,
				CurrentStage: task.CurrentStage,
				Summary:      summary,
			})
		}
	}
}

func progressSummary(task contracts.NarrowTask) string {
	if task.Scope != "" {
		return task.Scope
	}
	return "Task progress updated"
}