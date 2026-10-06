package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/portalstomp"
)

type usageFeedClient struct {
	mu        sync.Mutex
	records   []map[string]interface{}
	lastSeq   uint64
	calls     int
	recent    int
	called    chan struct{}
	honorPage bool
}

func (c *usageFeedClient) Call(_ context.Context, action string, payload json.RawMessage) (*APIResponse, error) {
	c.mu.Lock()
	c.calls++
	if action == "llm.usage.recent" {
		c.recent++
	}
	recs := append([]map[string]interface{}(nil), c.records...)
	last := c.lastSeq
	c.mu.Unlock()
	if action != "llm.usage.recent" {
		return &APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
	}
	if c.called != nil {
		select {
		case c.called <- struct{}{}:
		default:
		}
	}

	var req map[string]interface{}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &req)
	}
	limit := uint64(llmUsageRecentDefault)
	if seq, ok := llmUsageSeq(req["limit"]); ok && seq > 0 {
		limit = seq
	}
	after := uint64(0)
	_, hasAfter := req["after_seq"]
	if hasAfter {
		after, _ = llmUsageSeq(req["after_seq"])
	}

	page := recs
	if c.honorPage {
		if hasAfter {
			filtered := make([]map[string]interface{}, 0)
			for _, rec := range recs {
				seq, ok := llmUsageSeq(rec["seq"])
				if ok && seq > after {
					filtered = append(filtered, rec)
				}
			}
			page = filtered
		}
		if uint64(len(page)) > limit {
			if hasAfter {
				page = page[:limit]
			} else {
				page = page[uint64(len(page))-limit:]
			}
		}
	}
	var body []byte
	var err error
	if c.honorPage {
		body, err = json.Marshal(map[string]interface{}{
			"records":  page,
			"last_seq": last,
		})
	} else {
		body, err = json.Marshal(page)
	}
	if err != nil {
		return nil, err
	}
	return &APIResponse{Success: true, Data: body}, nil
}

func (c *usageFeedClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *usageFeedClient) recentCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recent
}

func (c *usageFeedClient) setRecords(last uint64, recs []map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSeq = last
	c.records = recs
}

func TestEnsureBackgroundPublishersStartsOneLLMUsageFeed(t *testing.T) {
	// Moving the feed back to Server.Start (and out of EnsureBackgroundPublishers)
	// leaves this at zero polls: web-portal calls Ensure and never Start.
	client := &usageFeedClient{called: make(chan struct{}, 4)}
	s, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	s.llmUsageInterval = time.Hour
	t.Cleanup(s.Close)

	s.EnsureBackgroundPublishers()
	select {
	case <-client.called:
	case <-time.After(2 * time.Second):
		t.Fatal("EnsureBackgroundPublishers did not poll llm.usage.recent")
	}
	s.EnsureBackgroundPublishers()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start(ctx)
	}()
	time.Sleep(200 * time.Millisecond)
	if n := client.recentCount(); n != 1 {
		t.Fatalf("llm.usage.recent polls = %d, want 1 after Ensure twice and Start", n)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return")
	}
}

func TestLLMUsageFeedFirstPollDoesNotReplay(t *testing.T) {
	// Publishing on the first poll, or publishing every record on later polls, fails.
	client := &usageFeedClient{honorPage: true}
	client.setRecords(2, []map[string]interface{}{
		{"seq": uint64(1), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:01Z", "success": true},
		{"seq": uint64(2), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:01Z", "success": true},
	})
	var got []uint64
	s := &Server{
		apiClient: client,
		llmUsageEmit: func(_ string, rec map[string]interface{}) {
			seq, _ := llmUsageSeq(rec["seq"])
			got = append(got, seq)
		},
	}
	s.publishNewLLMUsage(context.Background())
	if len(got) != 0 {
		t.Fatalf("first poll published %v, want nothing", got)
	}
	s.publishNewLLMUsage(context.Background())
	if len(got) != 0 {
		t.Fatalf("second poll republished %v", got)
	}
	client.setRecords(3, []map[string]interface{}{
		{"seq": uint64(1), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:01Z", "success": true},
		{"seq": uint64(2), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:01Z", "success": true},
		{"seq": uint64(3), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:02Z", "success": false},
	})
	s.publishNewLLMUsage(context.Background())
	if len(got) != 1 || got[0] != 3 {
		t.Fatalf("published %v, want only seq 3", got)
	}
}

func TestLLMUsageFeedPublishesSameSecondRecords(t *testing.T) {
	hub := portalstomp.NewHub()
	global := portalstomp.NewSession(hub)
	global.HandleFrame("SUBSCRIBE", map[string]string{
		"id":          "g",
		"destination": contracts.TopicLLMUsagePrefix,
	}, "")
	agent := portalstomp.NewSession(hub)
	agent.HandleFrame("SUBSCRIBE", map[string]string{
		"id":          "a",
		"destination": contracts.LLMUsageTopic("coder-1"),
	}, "")

	client := &usageFeedClient{honorPage: true}
	s := &Server{apiClient: client, stompHub: hub}
	s.publishNewLLMUsage(context.Background())

	ts := "2026-10-06T00:00:01Z"
	client.setRecords(2, []map[string]interface{}{
		{"seq": uint64(1), "agent_id": "coder-1", "model": "qwen", "timestamp": ts, "tokens_prompt": float64(3), "tokens_completion": float64(4), "success": true},
		{"seq": uint64(2), "agent_id": "coder-1", "model": "qwen", "timestamp": ts, "tokens_prompt": float64(9), "success": false},
	})
	s.publishNewLLMUsage(context.Background())
	got := drainUsageFrames(global)
	if len(got) != 2 {
		t.Fatalf("global frames %d %+v, want both same-second records", len(got), got)
	}
	if got[0]["tokens_prompt"].(float64) != 3 || got[1]["tokens_prompt"].(float64) != 9 || got[1]["success"] != false {
		t.Fatalf("frames %+v", got)
	}
	if got[0]["timestamp"] != ts || got[1]["timestamp"] != ts || got[0]["agent_id"] != "coder-1" {
		t.Fatalf("frames %+v", got)
	}
	if _, ok := got[1]["error"]; ok {
		t.Fatalf("stomp frame carried error: %+v", got[1])
	}
	if extra := drainUsageFrames(agent); len(extra) != 2 {
		t.Fatalf("agent frames %+v", extra)
	}
}

func TestLLMUsageFeedPagesBurst(t *testing.T) {
	// Not paging fails: one page is llmUsageRecentDefault (100), and 250 remain.
	client := &usageFeedClient{honorPage: true}
	s := &Server{apiClient: client}
	s.publishNewLLMUsage(context.Background())
	if n := client.recentCount(); n != 1 {
		t.Fatalf("baseline polls %d", n)
	}

	recs := make([]map[string]interface{}, 250)
	for i := 0; i < len(recs); i++ {
		recs[i] = map[string]interface{}{
			"seq":       uint64(i + 1),
			"agent_id":  "coder-1",
			"timestamp": "2026-10-06T00:00:01Z",
			"success":   true,
		}
	}
	client.setRecords(250, recs)
	var got []uint64
	s.llmUsageEmit = func(_ string, rec map[string]interface{}) {
		seq, _ := llmUsageSeq(rec["seq"])
		got = append(got, seq)
	}
	before := client.recentCount()
	s.publishNewLLMUsage(context.Background())
	pages := client.recentCount() - before
	if pages < 3 {
		t.Fatalf("catch-up polls %d, want at least 3 pages", pages)
	}
	if len(got) != 250 {
		t.Fatalf("published %d records, want 250", len(got))
	}
	seen := map[uint64]int{}
	for i, seq := range got {
		seen[seq]++
		if seq != uint64(i+1) {
			t.Fatalf("published order broke at %d: %d", i, seq)
		}
	}
	if len(seen) != 250 {
		t.Fatalf("unique seqs %d", len(seen))
	}
	s.publishNewLLMUsage(context.Background())
	if len(got) != 250 {
		t.Fatalf("republished, total %d", len(got))
	}
}

func TestLLMUsageFeedStoreRestartDoesNotReplay(t *testing.T) {
	client := &usageFeedClient{honorPage: true}
	client.setRecords(10, []map[string]interface{}{
		{"seq": uint64(10), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:01Z", "success": true},
	})
	var got []uint64
	s := &Server{
		apiClient: client,
		llmUsageEmit: func(_ string, rec map[string]interface{}) {
			seq, _ := llmUsageSeq(rec["seq"])
			got = append(got, seq)
		},
	}
	s.publishNewLLMUsage(context.Background())
	if len(got) != 0 {
		t.Fatalf("baseline published %v", got)
	}
	// Store process restarted: seq space dropped under the baseline.
	client.setRecords(2, []map[string]interface{}{
		{"seq": uint64(1), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:09Z", "success": true},
		{"seq": uint64(2), "agent_id": "pm", "timestamp": "2026-10-06T00:00:09Z", "success": true},
	})
	s.publishNewLLMUsage(context.Background())
	if len(got) != 0 {
		t.Fatalf("restart poll published %v, want nothing", got)
	}
	client.setRecords(3, []map[string]interface{}{
		{"seq": uint64(1), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:09Z", "success": true},
		{"seq": uint64(2), "agent_id": "pm", "timestamp": "2026-10-06T00:00:09Z", "success": true},
		{"seq": uint64(3), "agent_id": "coder-1", "timestamp": "2026-10-06T00:00:10Z", "success": true},
	})
	s.publishNewLLMUsage(context.Background())
	if len(got) != 1 || got[0] != 3 {
		t.Fatalf("after restart published %v, want only seq 3", got)
	}
}

func TestLLMUsageFeedStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &usageFeedClient{}
	s := &Server{apiClient: client}
	done := make(chan struct{})
	go func() {
		s.runLLMUsageFeed(ctx, time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled feed did not return")
	}
	if client.callCount() != 0 {
		t.Fatalf("cancelled feed polled %d times", client.callCount())
	}

	ctx, cancel = context.WithCancel(context.Background())
	client = &usageFeedClient{called: make(chan struct{}, 1), records: []map[string]interface{}{
		{"agent_id": "coder-1", "timestamp": "2026-10-06T00:00:03Z"},
	}}
	s = &Server{apiClient: client, stompHub: portalstomp.NewHub()}
	done = make(chan struct{})
	go func() {
		s.runLLMUsageFeed(ctx, time.Hour)
		close(done)
	}()
	select {
	case <-client.called:
	case <-time.After(2 * time.Second):
		t.Fatal("feed did not poll")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("feed did not stop after cancel")
	}
	if n := client.callCount(); n != 1 {
		t.Fatalf("polls after stop %d, want 1", n)
	}
}

func drainUsageFrames(sess *portalstomp.Session) []map[string]interface{} {
	var out []map[string]interface{}
	for {
		select {
		case frame := <-sess.Outbound():
			if !strings.Contains(frame, "MESSAGE") {
				continue
			}
			idx := strings.Index(frame, "\n\n")
			if idx < 0 {
				continue
			}
			body := strings.TrimSuffix(frame[idx+2:], "\x00")
			var m map[string]interface{}
			if json.Unmarshal([]byte(body), &m) == nil {
				out = append(out, m)
			}
		default:
			return out
		}
	}
}
