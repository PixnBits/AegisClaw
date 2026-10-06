package dashboard

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

// llmUsagePollInterval matches startMonitoringPublisher.
const llmUsagePollInterval = 15 * time.Second

// runLLMUsageFeed polls Store llm.usage.recent until ctx is cancelled.
// The first successful poll records the Store's max seq and publishes
// nothing, so a dashboard restart does not replay history. Later polls pass
// after_seq and keep requesting the oldest page until a short page comes
// back, so a burst larger than one page is not dropped.
//
// If the Store's max seq is below the baseline, the Store restarted. That
// poll publishes nothing and the baseline becomes the new max. Records that
// already existed when the drop was noticed are not replayed. A later record
// (seq above the new baseline) is published on a following poll.
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
	if s == nil || ctx.Err() != nil {
		return
	}
	s.llmUsageMu.Lock()
	ready := s.llmUsageReady
	cursor := s.llmUsageLast
	s.llmUsageMu.Unlock()

	if !ready {
		_, lastSeq, hasLast, err := s.fetchLLMUsagePage(ctx, 0, false)
		if err != nil || ctx.Err() != nil {
			return
		}
		base := uint64(0)
		if hasLast {
			base = lastSeq
		}
		s.llmUsageMu.Lock()
		if !s.llmUsageReady {
			s.llmUsageLast = base
			s.llmUsageReady = true
		}
		s.llmUsageMu.Unlock()
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}
		rows, lastSeq, hasLast, err := s.fetchLLMUsagePage(ctx, cursor, true)
		if err != nil || ctx.Err() != nil {
			return
		}
		if hasLast && lastSeq < cursor {
			s.llmUsageMu.Lock()
			s.llmUsageLast = lastSeq
			s.llmUsageReady = true
			s.llmUsageMu.Unlock()
			return
		}
		if len(rows) == 0 {
			return
		}
		advanced := false
		for _, rec := range rows {
			seq, ok := llmUsageSeq(rec["seq"])
			if !ok || seq <= cursor {
				continue
			}
			s.emitLLMUsage(rec)
			cursor = seq
			advanced = true
		}
		if !advanced {
			return
		}
		s.llmUsageMu.Lock()
		if cursor > s.llmUsageLast {
			s.llmUsageLast = cursor
		}
		s.llmUsageMu.Unlock()
		if len(rows) < llmUsageRecentDefault {
			return
		}
	}
}

func (s *Server) emitLLMUsage(rec map[string]interface{}) {
	agentID, _ := rec["agent_id"].(string)
	s.stompPublisher().PublishLLMUsage(agentID, rec)
	if s.llmUsageEmit != nil {
		s.llmUsageEmit(agentID, rec)
	}
}

func (s *Server) fetchLLMUsagePage(ctx context.Context, after uint64, withAfter bool) ([]map[string]interface{}, uint64, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, spaAPITimeout)
	defer cancel()
	payload := map[string]interface{}{"limit": llmUsageRecentDefault}
	if withAfter {
		payload["after_seq"] = after
	}
	data, err := s.fetchRaw(ctx, "llm.usage.recent", payload)
	if err != nil {
		return nil, 0, false, err
	}
	rows, lastSeq, hasLast := parseLLMUsageRecent(data)
	return rows, lastSeq, hasLast, nil
}

// parseLLMUsageRecent accepts the Store wrapper {"records","last_seq"} and a
// bare record array (fixture clients). last_seq is the Store max, which can
// be greater than any seq on this page and is present even when records is empty.
func parseLLMUsageRecent(data interface{}) (rows []map[string]interface{}, lastSeq uint64, hasLast bool) {
	switch body := data.(type) {
	case map[string]interface{}:
		rows = llmUsageRows(body["records"])
		if seq, ok := llmUsageSeq(body["last_seq"]); ok {
			return rows, seq, true
		}
	default:
		rows = llmUsageRows(data)
	}
	if seq, ok := maxLLMUsageSeq(rows); ok {
		return rows, seq, true
	}
	return rows, 0, false
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

func maxLLMUsageSeq(rows []map[string]interface{}) (uint64, bool) {
	var max uint64
	ok := false
	for _, rec := range rows {
		seq, has := llmUsageSeq(rec["seq"])
		if !has {
			continue
		}
		if !ok || seq > max {
			max = seq
			ok = true
		}
	}
	return max, ok
}

func llmUsageSeq(v interface{}) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case uint:
		return uint64(n), true
	case int:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int32:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, false
		}
		return uint64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i < 0 {
			return 0, false
		}
		return uint64(i), true
	default:
		return 0, false
	}
}
