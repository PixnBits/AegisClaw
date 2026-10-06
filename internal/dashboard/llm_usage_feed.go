package dashboard

import (
	"context"
	"time"
)

// llmUsagePollInterval matches startMonitoringPublisher. That publisher does
// not stop with the server context; this feed does.
const llmUsagePollInterval = 15 * time.Second

// llmUsageSeenCap bounds the dedupe set. The poll itself asks for at most
// llmUsageRecentDefault records, so the set stays near the recent window.
const llmUsageSeenCap = 1024

// runLLMUsageFeed polls Store llm.usage.recent and publishes records the feed
// has not seen. It returns when ctx is cancelled and does not start another
// goroutine.
func (s *Server) runLLMUsageFeed(ctx context.Context, interval time.Duration) {
	if s == nil || ctx == nil {
		return
	}
	if interval <= 0 {
		interval = llmUsagePollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.publishNewLLMUsage(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) publishNewLLMUsage(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, spaAPITimeout)
	defer cancel()
	data, err := s.fetchRaw(ctx, "llm.usage.recent", map[string]interface{}{
		"limit": llmUsageRecentDefault,
	})
	if err != nil {
		return
	}
	pub := s.stompPublisher()
	for _, rec := range llmUsageRows(data) {
		if !s.rememberLLMUsage(llmUsageDedupeKey(rec)) {
			continue
		}
		agentID, _ := rec["agent_id"].(string)
		pub.PublishLLMUsage(agentID, rec)
	}
}

func llmUsageRows(data interface{}) []map[string]interface{} {
	switch rows := data.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(rows))
		for _, row := range rows {
			if m, ok := row.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]interface{}:
		return rows
	default:
		return nil
	}
}

func llmUsageDedupeKey(rec map[string]interface{}) string {
	ts, _ := rec["timestamp"].(string)
	agent, _ := rec["agent_id"].(string)
	return ts + "\x00" + agent
}

// rememberLLMUsage reports whether key is new. The oldest keys are dropped
// once the set exceeds llmUsageSeenCap.
func (s *Server) rememberLLMUsage(key string) bool {
	s.llmUsageMu.Lock()
	defer s.llmUsageMu.Unlock()
	if s.llmUsageSeen == nil {
		s.llmUsageSeen = make(map[string]struct{})
	}
	if _, ok := s.llmUsageSeen[key]; ok {
		return false
	}
	s.llmUsageSeen[key] = struct{}{}
	s.llmUsageKeys = append(s.llmUsageKeys, key)
	if len(s.llmUsageKeys) > llmUsageSeenCap {
		drop := s.llmUsageKeys[0]
		s.llmUsageKeys = append([]string(nil), s.llmUsageKeys[1:]...)
		delete(s.llmUsageSeen, drop)
	}
	return true
}
