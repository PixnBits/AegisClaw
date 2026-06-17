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
}