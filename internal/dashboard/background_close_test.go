package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockUntilCancelClient stays inside Call until ctx is cancelled, then pauses
// so a Close that skips Wait returns while the goroutine is still in Call.
type blockUntilCancelClient struct {
	entered  chan struct{}
	once     sync.Once
	inFlight atomic.Int32
}

func (c *blockUntilCancelClient) Call(ctx context.Context, _ string, _ json.RawMessage) (*APIResponse, error) {
	if err := ctx.Err(); err != nil {
		return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, err
	}
	c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	time.Sleep(150 * time.Millisecond)
	return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
}

func TestCloseWaitsForFeedGoroutine(t *testing.T) {
	// Close that only cancels and does not Wait returns while Call is still
	// inside its post-cancel pause, so inFlight is not zero.
	client := &blockUntilCancelClient{entered: make(chan struct{})}
	s, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	s.llmUsageInterval = time.Hour
	s.EnsureBackgroundPublishers()
	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("background call did not start")
	}
	start := time.Now()
	s.Close()
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("Close returned before the in-flight call finished")
	}
	if n := client.inFlight.Load(); n != 0 {
		t.Fatalf("Close returned with %d calls still in flight", n)
	}
	// Second Close must not deadlock or panic.
	s.Close()
}

func TestCloseZeroValueTwice(t *testing.T) {
	var s Server
	s.Close()
	s.Close()
}

// monitorBlockClient blocks the monitoring collect (worker.list) and the usage
// feed until their ctx is done, and records the error the collect observed.
type monitorBlockClient struct {
	started chan struct{}
	once    sync.Once
	mu      sync.Mutex
	errs    map[string]error
}

func (c *monitorBlockClient) Call(ctx context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	if action == "worker.list" {
		c.once.Do(func() { close(c.started) })
		<-ctx.Done()
		c.mu.Lock()
		if c.errs == nil {
			c.errs = map[string]error{}
		}
		c.errs[action] = ctx.Err()
		c.mu.Unlock()
		return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, ctx.Err()
	}
	if action == "llm.usage.recent" {
		<-ctx.Done()
		return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, err
	}
	return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
}

func TestCloseCancelsMonitoringCollect(t *testing.T) {
	// collect's timeout parent must be the background ctx. With
	// context.Background(), Close stays in the in-flight worker.list call
	// until spaAPITimeout and the call's ctx.Err is not Canceled.
	client := &monitorBlockClient{started: make(chan struct{})}
	s, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	s.llmUsageInterval = time.Hour
	s.EnsureBackgroundPublishers()
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatal("monitoring collect did not start")
	}
	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return promptly; in-flight monitoring collect was not cancelled")
	}
	client.mu.Lock()
	got := client.errs["worker.list"]
	client.mu.Unlock()
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("monitoring collect ctx err = %v, want context.Canceled", got)
	}
}

// TestGoBackgroundAfterCloseStartsNothing: once Close has run, goBackground
// must not start fn. Otherwise its bgWG.Add could race Close's bgWG.Wait.
func TestGoBackgroundAfterCloseStartsNothing(t *testing.T) {
	s := &Server{}
	s.Close()
	ran := make(chan struct{}, 1)
	s.goBackground(func() { ran <- struct{}{} })
	select {
	case <-ran:
		t.Fatal("goBackground started fn after Close")
	case <-time.After(50 * time.Millisecond):
	}
	s.waitBackground()
}
