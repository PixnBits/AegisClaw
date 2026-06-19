package realtime

import (
	"encoding/json"
	"time"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/dashboard/sanitize"
	"AegisClaw/internal/portalstomp"
)

// Publisher sends sanitized STOMP events to subscribed browsers.
type Publisher struct {
	Hub *portalstomp.Hub
}

func NewPublisher(hub *portalstomp.Hub) *Publisher {
	return &Publisher{Hub: hub}
}

// PublishChannelActivity emits channel.activity on canonical and legacy topics.
func (p *Publisher) PublishChannelActivity(channelID, from, content string) {
	if p == nil || p.Hub == nil || channelID == "" {
		return
	}
	event, _ := json.Marshal(map[string]interface{}{
		"kind":    "message",
		"from":    from,
		"content": content,
		"ts":      time.Now().UTC().Format(time.RFC3339),
	})
	payload, err := json.Marshal(contracts.ChannelActivity{
		Type:      contracts.TypeChannelActivity,
		ChannelID: channelID,
		Event:     event,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	clean, err := sanitize.JSONBytes(sanitize.ContextChat, payload)
	if err != nil {
		clean = payload
	}
	canonical := contracts.ChannelActivityTopic(channelID)
	legacy := contracts.LegacyChannelMessagesTopic(channelID)
	p.Hub.Publish(canonical, clean)
	p.Hub.Publish(legacy, clean)
}

// PublishMonitoringStats pushes dashboard monitoring metrics to subscribers.
func (p *Publisher) PublishMonitoringStats(stats map[string]interface{}) {
	if p == nil || p.Hub == nil || stats == nil {
		return
	}
	body, err := json.Marshal(map[string]interface{}{
		"type":      contracts.TypeMonitoringStats,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"stats":     stats["stats"],
		"agents":    stats["agents"],
	})
	if err != nil {
		return
	}
	clean, err := sanitize.JSONBytes(sanitize.ContextChat, body)
	if err != nil {
		clean = body
	}
	p.Hub.Publish(contracts.TopicMonitoringStats, clean)
}

// PublishHarness publishes a harness event to plan-specific and channel topics.
func (p *Publisher) PublishHarness(planID, channelID string, event interface{}) {
	if p == nil || p.Hub == nil || planID == "" {
		return
	}
	body, err := json.Marshal(event)
	if err != nil {
		return
	}
	clean, err := sanitize.JSONBytes(sanitize.ContextChat, body)
	if err != nil {
		clean = body
	}
	p.Hub.Publish(contracts.HarnessUpdatesTopic(planID), clean)
	if channelID != "" {
		wrapped, _ := json.Marshal(contracts.ChannelActivity{
			Type:      contracts.TypeChannelActivity,
			ChannelID: channelID,
			Event:     clean,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		p.Hub.Publish(contracts.ChannelActivityTopic(channelID), wrapped)
	}
	p.publishCanvasFromHarness(clean)
}

func (p *Publisher) publishCanvasFromHarness(body []byte) {
	var base map[string]interface{}
	if json.Unmarshal(body, &base) != nil {
		return
	}
	eventType, _ := base["type"].(string)
	switch eventType {
	case contracts.TypeHarnessTaskAssigned, contracts.TypeHarnessTaskProgress:
	default:
		return
	}
	taskID, _ := base["task_id"].(string)
	if taskID == "" {
		return
	}
	persona, _ := base["agent_persona"].(string)
	stage, _ := base["current_stage"].(string)
	if stage == "" {
		stage, _ = base["stage"].(string)
	}
	progress := 0
	if v, ok := base["progress"].(float64); ok {
		progress = int(v)
	}
	canvasBody, err := json.Marshal(contracts.CanvasEvent{
		Type:          contracts.TypeCanvasEvent,
		PersonaTaskID: taskID,
		TaskID:        taskID,
		Persona:       persona,
		Stage:         stage,
		Progress:      progress,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	clean, err := sanitize.JSONBytes(sanitize.ContextChat, canvasBody)
	if err != nil {
		clean = canvasBody
	}
	p.Hub.Publish(contracts.TopicCanvasEvents, clean)
}