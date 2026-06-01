package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"AegisClaw/internal/dashboard"
)

// lazyBridgeClient starts HTTP/vsock immediately and retries Hub/portal-bridge
// in the background until connected (guest vsock to host can be slow or flaky).
type lazyBridgeClient struct {
	mu     sync.RWMutex
	client dashboard.APIClient
}

func newLazyBridgeClient() dashboard.APIClient {
	l := &lazyBridgeClient{client: &noopAPIClient{}}
	go l.connectLoop()
	return l
}

func (l *lazyBridgeClient) connectLoop() {
	delay := 500 * time.Millisecond
	for attempt := 1; attempt <= 120; attempt++ {
		c, err := newHubBridgeClient()
		if err == nil {
			l.mu.Lock()
			l.client = c
			l.mu.Unlock()
			log.Println("web-portal: hub/portal bridge connected (background)")
			return
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("WARNING: web-portal hub bridge attempt %d: %v", attempt, err)
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay += 250 * time.Millisecond
		}
	}
	log.Println("WARNING: web-portal hub bridge gave up after 120 attempts; chat/sessions use host /api/chat/* and /chat/send on :8080; dashboard actions inside the guest need portal bridge :1030 or inverted :9102")
}

func (l *lazyBridgeClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	l.mu.RLock()
	c := l.client
	l.mu.RUnlock()
	return c.Call(ctx, action, payload)
}
