package contracts

// Pipeline stages per docs/specs/web-portal/harness-pipeline-data-model.md.

var PipelineStageNames = []string{
	"Plan",
	"Delegate",
	"Execute",
	"Propose",
	"Court Review",
	"Apply",
}

// PlanStatus values for harness plans.
const (
	PlanStatusActive    = "active"
	PlanStatusCompleted = "completed"
	PlanStatusCancelled = "cancelled"
)

// StageStatus values for pipeline stages.
const (
	StagePending    = "pending"
	StageInProgress = "in_progress"
	StageCompleted  = "completed"
	StageFailed     = "failed"
)

// Plan is the high-level harness goal for a channel.
type Plan struct {
	PlanID    string         `json:"plan_id"`
	ChannelID string         `json:"channel_id"`
	Goal      string         `json:"goal"`
	CreatedAt string         `json:"created_at"`
	Status    string         `json:"status"`
	Stages    []StageStatus  `json:"stages"`
}

// StageStatus is one pipeline stage with status.
type StageStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// NarrowTask is a decomposed unit of work for a specialist persona.
// agent_instance_id must never be serialized to browser-bound JSON.
type NarrowTask struct {
	TaskID       string `json:"task_id"`
	PlanID       string `json:"plan_id"`
	AgentPersona string `json:"agent_persona"`
	Scope        string `json:"scope"`
	Status       string `json:"status"`
	CurrentStage string `json:"current_stage"`
	Progress     int    `json:"progress"`
	LastUpdate   string `json:"last_update"`
}

// HarnessState aggregates plan + tasks for channel/dashboard views.
type HarnessState struct {
	Plan  *Plan        `json:"plan,omitempty"`
	Tasks []NarrowTask `json:"tasks"`
}

// DefaultStages returns pending pipeline stages for a new plan.
func DefaultStages() []StageStatus {
	out := make([]StageStatus, len(PipelineStageNames))
	for i, name := range PipelineStageNames {
		status := StagePending
		if i == 0 {
			status = StageInProgress
		}
		out[i] = StageStatus{Name: name, Status: status}
	}
	return out
}

// HarnessPlanCreated event payload.
type HarnessPlanCreated struct {
	Type      string        `json:"type"`
	PlanID    string        `json:"plan_id"`
	ChannelID string        `json:"channel_id"`
	Goal      string        `json:"goal"`
	Stages    []StageStatus `json:"stages"`
}

// HarnessTaskAssigned event payload.
type HarnessTaskAssigned struct {
	Type          string `json:"type"`
	TaskID        string `json:"task_id"`
	PlanID        string `json:"plan_id"`
	AgentPersona  string `json:"agent_persona"`
	Scope         string `json:"scope"`
	CurrentStage  string `json:"current_stage"`
}

// HarnessTaskProgress event payload.
type HarnessTaskProgress struct {
	Type         string `json:"type"`
	TaskID       string `json:"task_id"`
	Progress     int    `json:"progress"`
	CurrentStage string `json:"current_stage"`
	Summary      string `json:"summary"`
}

// HarnessStageTransition event payload.
type HarnessStageTransition struct {
	Type           string   `json:"type"`
	PlanID         string   `json:"plan_id"`
	Stage          string   `json:"stage"`
	Status         string   `json:"status"`
	RelatedTaskIDs []string `json:"related_task_ids"`
}

// HarnessProposalCreated event payload.
type HarnessProposalCreated struct {
	Type       string `json:"type"`
	PlanID     string `json:"plan_id"`
	TaskID     string `json:"task_id"`
	ProposalID string `json:"proposal_id"`
	Stage      string `json:"stage"`
}