package portalstomp

import (
	"sync"

	"AegisClaw/internal/dashboard/contracts"
)

// Session is one browser STOMP connection.
type Session struct {
	hub  *Hub
	send chan string
	subs map[string]string // topic -> subscription id
	mu   sync.Mutex
}

func NewSession(hub *Hub) *Session {
	return &Session{
		hub:  hub,
		send: make(chan string, 32),
		subs: make(map[string]string),
	}
}

func (s *Session) Outbound() <-chan string {
	return s.send
}

func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for topic := range s.subs {
		s.hub.Unsubscribe(topic, s)
	}
	s.subs = map[string]string{}
}

func (s *Session) HandleFrame(command string, headers map[string]string, body string) {
	switch command {
	case "CONNECT", "STOMP":
		select {
		case s.send <- BuildConnectedFrame():
		default:
		}
	case "SUBSCRIBE":
		dest := headers["destination"]
		subID := headers["id"]
		if dest == "" || !contracts.IsAllowedTopic(dest) {
			return
		}
		s.mu.Lock()
		s.subs[dest] = subID
		s.mu.Unlock()
		s.hub.Subscribe(dest, s)
	case "UNSUBSCRIBE":
		subID := headers["id"]
		s.mu.Lock()
		for topic, id := range s.subs {
			if id == subID {
				delete(s.subs, topic)
				s.hub.Unsubscribe(topic, s)
			}
		}
		s.mu.Unlock()
	case "DISCONNECT":
		s.Close()
	default:
		_ = body
	}
}

func (s *Session) Deliver(topic string, body []byte) {
	s.mu.Lock()
	subID := s.subs[topic]
	s.mu.Unlock()
	if subID == "" {
		return
	}
	frame := BuildMessageFrame(topic, subID, body)
	select {
	case s.send <- frame:
	default:
	}
}
