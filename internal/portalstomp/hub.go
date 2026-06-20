package portalstomp

import (
	"sync"
)

// Hub routes STOMP MESSAGE frames to WebSocket sessions subscribed by topic.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[*Session]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[*Session]struct{})}
}

func (h *Hub) Subscribe(topic string, s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[topic] == nil {
		h.subs[topic] = make(map[*Session]struct{})
	}
	h.subs[topic][s] = struct{}{}
}

func (h *Hub) Unsubscribe(topic string, s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.subs[topic]; m != nil {
		delete(m, s)
		if len(m) == 0 {
			delete(h.subs, topic)
		}
	}
}

func (h *Hub) UnsubscribeAll(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for topic, m := range h.subs {
		delete(m, s)
		if len(m) == 0 {
			delete(h.subs, topic)
		}
	}
}

// Publish delivers a JSON payload to every session subscribed to topic.
func (h *Hub) Publish(topic string, body []byte) {
	h.mu.RLock()
	targets := make([]*Session, 0, len(h.subs[topic]))
	for s := range h.subs[topic] {
		targets = append(targets, s)
	}
	h.mu.RUnlock()
	for _, s := range targets {
		s.Deliver(topic, body)
	}
}

// ChannelTopic returns the canonical channel activity destination.
func ChannelTopic(channelID string) string {
	return "/topic/channel." + channelID + ".activity"
}

// LegacyChannelTopic returns the pre-spec messages topic (dual-publish shim).
func LegacyChannelTopic(channelID string) string {
	return "/topic/channels." + channelID + ".messages"
}
