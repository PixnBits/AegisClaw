package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Category identifies a rate-limit bucket per security-boundaries.md.
type Category string

const (
	CategoryProposalAction Category = "proposal_action"
	CategoryAgentControl   Category = "agent_control"
	CategoryMemberRemove   Category = "member_remove"
	CategoryChannelArchive Category = "channel_archive"
)

// Limits per category: max requests per window.
var limits = map[Category]struct {
	Max    int
	Window time.Duration
}{
	CategoryProposalAction: {Max: 12, Window: time.Minute},
	CategoryAgentControl:   {Max: 24, Window: time.Minute},
	CategoryMemberRemove:   {Max: 10, Window: time.Minute},
	CategoryChannelArchive: {Max: 6, Window: time.Minute},
}

type bucket struct {
	tokens     int
	lastRefill time.Time
	max        int
	window     time.Duration
}

// Limiter provides per-client, per-category token-bucket rate limiting.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

// Default is the shared portal API rate limiter.
var Default = &Limiter{buckets: make(map[string]*bucket)}

// Allow reports whether the client may perform an action in category.
func (l *Limiter) Allow(clientKey string, cat Category) bool {
	cfg, ok := limits[cat]
	if !ok || clientKey == "" {
		return true
	}
	key := clientKey + ":" + string(cat)

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: cfg.Max, lastRefill: time.Now().UTC(), max: cfg.Max, window: cfg.Window}
		l.buckets[key] = b
	}
	now := time.Now().UTC()
	elapsed := now.Sub(b.lastRefill)
	if elapsed >= b.window {
		refills := int(elapsed / b.window)
		if refills > 0 {
			b.tokens += refills * b.max
			if b.tokens > b.max {
				b.tokens = b.max
			}
			b.lastRefill = now
		}
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// ClientKey extracts a stable per-request client identifier (IP).
func ClientKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// WriteTooManyRequests sets standard 429 response headers.
func WriteTooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	http.Error(w, "rate limited", http.StatusTooManyRequests)
}

// Guard returns false and writes 429 when the limit is exceeded.
func Guard(w http.ResponseWriter, r *http.Request, cat Category) bool {
	if Default.Allow(ClientKey(r), cat) {
		return true
	}
	WriteTooManyRequests(w)
	return false
}