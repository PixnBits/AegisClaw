package contracts

import "encoding/json"

// Event type constants for STOMP MESSAGE JSON bodies.

const (
	TypeOverviewStats       = "overview.stats"
	TypeConversationUpdate  = "conversation.update"
	TypeChannelActivity     = "channel.activity"
	TypeCanvasEvent         = "canvas.event"
	TypeHarnessPlanCreated  = "harness.plan.created"
	TypeHarnessTaskAssigned = "harness.task.assigned"
	TypeHarnessTaskProgress = "harness.task.progress"
	TypeHarnessStageTrans   = "harness.stage.transition"
	TypeHarnessProposal     = "harness.proposal.created"
	TypeMonitoringStats     = "monitoring.stats"
	TypeLLMUsage            = "llm.usage" // Phase 1 metrics
)

// OverviewStats is pushed on /topic/overview.stats.
type OverviewStats struct {
	Type              string         `json:"type"`
	Timestamp         string         `json:"timestamp"`
	ActiveAgents      AgentCounts    `json:"active_agents"`
	BackgroundTasks   BackgroundTask `json:"background_tasks"`
	PendingProposals  int            `json:"pending_proposals"`
}

type AgentCounts struct {
	Total  int            `json:"total"`
	ByRole map[string]int `json:"by_role"`
}

type BackgroundTask struct {
	Total       int `json:"total"`
	AvgProgress int `json:"avg_progress"`
}

// ChannelActivity wraps a channel feed event.
type ChannelActivity struct {
	Type      string          `json:"type"`
	ChannelID string          `json:"channel_id"`
	Event     json.RawMessage `json:"event"`
	Timestamp string          `json:"timestamp"`
}

// ChannelMessageEvent is a human or agent message in the activity feed.
type ChannelMessageEvent struct {
	Kind    string `json:"kind"`
	From    string `json:"from"`
	Content string `json:"content"`
	TS      string `json:"ts,omitempty"`
}

// ConversationUpdate is a streaming chat/trace delta.
type ConversationUpdate struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id"`
	Delta     json.RawMessage `json:"delta"`
	Timestamp string          `json:"timestamp"`
}

// CanvasEvent is an inter-agent pipeline update. PersonaTaskID is a sanitized
// task identifier — never an internal microVM instance id.
type CanvasEvent struct {
	Type          string `json:"type"`
	PersonaTaskID string `json:"persona_task_id"`
	TaskID        string `json:"task_id"`
	Persona       string `json:"persona,omitempty"`
	Stage         string `json:"stage"`
	Progress      int    `json:"progress"`
	Timestamp     string `json:"timestamp"`
}

// ParsePayload decodes a STOMP body into a typed envelope, ignoring unknown fields.
func ParsePayload(body []byte) (eventType string, err error) {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &base); err != nil {
		return "", err
	}
	return base.Type, nil
}

// KnownEventTypes lists payload types the portal must parse.
var KnownEventTypes = []string{
	TypeOverviewStats,
	TypeConversationUpdate,
	TypeChannelActivity,
	TypeCanvasEvent,
	TypeHarnessPlanCreated,
	TypeHarnessTaskAssigned,
	TypeHarnessTaskProgress,
	TypeHarnessStageTrans,
	TypeHarnessProposal,
	TypeLLMUsage,
}

// LLMUsageEvent is the live metrics update (emitted from boundary via store/dashboard publish).
type LLMUsageEvent struct {
	Type      string                 `json:"type"`
	AgentID   string                 `json:"agent_id"`
	Timestamp string                 `json:"timestamp"`
	Model     string                 `json:"model"`
	TokensIn  int                    `json:"tokens_prompt"`
	TokensOut int                    `json:"tokens_completion"`
	Duration  int                    `json:"duration_ms"`
	Success   bool                   `json:"success"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}