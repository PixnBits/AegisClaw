package boundarycrypto

import (
	"testing"
	"time"
)

func TestCanonicalSecretsUpdateData(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]interface{}
		wantNil bool
	}{
		{
			name: "full secrets with timestamp and nonce",
			input: map[string]interface{}{
				"timestamp": "2026-06-01T12:00:00Z",
				"secrets":   map[string]string{"skill1": "secret1"},
				"nonce":     "abc123",
			},
			wantNil: false,
		},
		{
			name: "single skill update",
			input: map[string]interface{}{
				"timestamp": "2026-06-01T12:00:00Z",
				"skill_id":  "skill1",
				"secret":    "secret1",
			},
			wantNil: false,
		},
		{
			name: "missing timestamp",
			input: map[string]interface{}{
				"secrets": map[string]string{"skill1": "secret1"},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalSecretsUpdateData(tt.input)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil, got data")
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected data, got nil")
			}
		})
	}
}

func TestIsTimestampFresh(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Format(time.RFC3339)
	stale := now.Add(-10 * time.Minute).Format(time.RFC3339)
	future := now.Add(2 * time.Minute).Format(time.RFC3339)

	tests := []struct {
		name  string
		ts    string
		want  bool
	}{
		{"fresh", fresh, true},
		{"stale", stale, false},
		{"future", future, false},
		{"missing", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]interface{}{"timestamp": tt.ts}
			if got := IsTimestampFresh(payload); got != tt.want {
				t.Errorf("IsTimestampFresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNonceCache_Basic(t *testing.T) {
	cache := NewNonceCache(10, time.Minute)

	if !cache.CheckAndRecord("nonce1") {
		t.Error("first use of nonce should be accepted")
	}
	if cache.CheckAndRecord("nonce1") {
		t.Error("immediate replay should be rejected")
	}
	if !cache.CheckAndRecord("nonce2") {
		t.Error("different nonce should be accepted")
	}
}

func TestNonceCache_EvictionAndTTL(t *testing.T) {
	cache := NewNonceCache(2, 10*time.Millisecond)

	cache.CheckAndRecord("a")
	cache.CheckAndRecord("b")
	time.Sleep(15 * time.Millisecond) // allow cleanup
	cache.CheckAndRecord("c") // should evict old ones

	if cache.Size() > 2 {
		t.Errorf("cache size should be bounded, got %d", cache.Size())
	}
}

func TestRateLimiter_Basic(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	if !rl.Allow() {
		t.Error("first allow should succeed")
	}
	if !rl.Allow() {
		t.Error("second allow should succeed")
	}
	if rl.Allow() {
		t.Error("third allow should be rate limited")
	}
}
