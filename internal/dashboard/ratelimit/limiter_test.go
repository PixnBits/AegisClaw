package ratelimit

import (
	"net/http/httptest"
	"testing"
)

func TestLimiterBlocksAfterMax(t *testing.T) {
	l := &Limiter{buckets: make(map[string]*bucket)}
	key := "127.0.0.1"
	for i := 0; i < 12; i++ {
		if !l.Allow(key, CategoryProposalAction) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow(key, CategoryProposalAction) {
		t.Fatal("13th request should be rate limited")
	}
}

func TestGuardWrites429(t *testing.T) {
	l := &Limiter{buckets: make(map[string]*bucket)}
	Default = l
	req := httptest.NewRequest("POST", "/api/proposals/p1/approve", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	w := httptest.NewRecorder()
	for i := 0; i < 12; i++ {
		_ = Guard(w, req, CategoryProposalAction)
	}
	w2 := httptest.NewRecorder()
	if Guard(w2, req, CategoryProposalAction) {
		t.Fatal("expected rate limit")
	}
	if w2.Code != 429 {
		t.Fatalf("got %d, want 429", w2.Code)
	}
}