package dashboard

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/portalstomp"
)

type usageFeedClient struct {
	mu      sync.Mutex
	records []map[string]interface{}
	calls   int
	called  chan struct{}
}

func (c *usageFeedClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	c.mu.Lock()
	c.calls++
	recs := c.records
	c.mu.Unlock()
	if c.called != nil {
		select {
		case c.called <- struct{}{}:
		default:
		}
	}
	if action != "llm.usage.recent" {
		return &APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
	}
	body, err := json.Marshal(recs)
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

func TestLLMUsageFeedPublishesOnlyNewRecords(t *testing.T) {
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

	client := &usageFeedClient{records: []map[string]interface{}{
		{"agent_id": "coder-1", "model": "qwen", "timestamp": "2026-10-06T00:00:01Z", "tokens_prompt": float64(3), "tokens_completion": float64(4), "success": true},
	}}
	s := &Server{apiClient: client, stompHub: hub}

	s.publishNewLLMUsage(context.Background())
	if got := drainUsageFrames(global); len(got) != 1 || got[0]["type"] != contracts.TypeLLMUsage || got[0]["tokens_prompt"].(float64) != 3 {
		t.Fatalf("global frames %+v", got)
	}
	if got := drainUsageFrames(agent); len(got) != 1 || got[0]["agent_id"] != "coder-1" {
		t.Fatalf("agent frames %+v", got)
	}

	s.publishNewLLMUsage(context.Background())
	if got := drainUsageFrames(global); len(got) != 0 {
		t.Fatalf("republished %+v", got)
	}

	client.mu.Lock()
	client.records = append(client.records, map[string]interface{}{
		"agent_id": "coder-1", "model": "qwen", "timestamp": "2026-10-06T00:00:02Z", "tokens_prompt": float64(9), "success": false,
	})
	client.mu.Unlock()
	s.publishNewLLMUsage(context.Background())
	got := drainUsageFrames(global)
	if len(got) != 1 || got[0]["timestamp"] != "2026-10-06T00:00:02Z" || got[0]["success"] != false {
		t.Fatalf("new record frames %+v", got)
	}
	if extra := drainUsageFrames(agent); len(extra) != 1 {
		t.Fatalf("agent new frames %+v", extra)
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
