// Package boundarycrypto contains reusable cryptographic helpers
// for the Network Boundary's Hub-mediated signed message paths
// (primarily "secrets.update" and related reconciliation flows).
//
// The helpers implement:
//   - Canonical data construction for signing/verification
//   - Timestamp freshness checks (with replay window)
//   - Bounded nonce-based replay protection
//
// These are deliberately factored out so they can be unit tested
// in isolation, reused across message types, and evolved without
// touching the main boundary binary.
//
// All functions are designed with a paranoid security mindset:
// they fail closed on bad input and never log secret material.
package boundarycrypto

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"
)

// canonicalSecretsUpdateData produces a deterministic byte slice for signing/verification.
// It extracts the secrets content, timestamp (required), and optional nonce in a stable order.
// (Moved from the main boundary binary for maintainability.)
func CanonicalSecretsUpdateData(payload map[string]interface{}) []byte {
	ts, ok := payload["timestamp"].(string)
	if !ok || ts == "" {
		return nil
	}

	canonical := map[string]interface{}{
		"timestamp": ts,
	}

	if secrets, ok := payload["secrets"]; ok {
		canonical["secrets"] = secrets
	} else if sid, ok := payload["skill_id"].(string); ok {
		canonical["skill_id"] = sid
		if sec, ok := payload["secret"].(string); ok {
			canonical["secret"] = sec
		}
	}

	if nonce, ok := payload["nonce"].(string); ok && nonce != "" {
		canonical["nonce"] = nonce
	}

	data, _ := json.Marshal(canonical)
	return data
}

// IsTimestampFresh returns true if the timestamp in the payload is within an
// acceptable window (currently 5 minutes in the past to 1 minute in the future).
// This provides basic replay and clock-skew protection.
func IsTimestampFresh(payload map[string]interface{}) bool {
	tsStr, ok := payload["timestamp"].(string)
	if !ok || tsStr == "" {
		return false
	}

	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		return false
	}

	now := time.Now().UTC()
	age := now.Sub(ts)

	return age >= -1*time.Minute && age <= 5*time.Minute
}

// NonceCache provides bounded, TTL-based replay protection for signed messages
// that carry a "nonce" field.
//
// Design goals (paranoid + practical):
// - Bounded memory (maxEntries) to prevent DoS via nonce flooding.
// - Entries older than the replay window are periodically cleaned.
// - A nonce is considered "seen" (replay) only if it was accepted within the
//   recent window.
// - Nonces are only recorded after full successful verification.
type NonceCache struct {
	mu           sync.RWMutex
	entries      map[string]time.Time
	maxEntries   int
	replayWindow time.Duration
}

func NewNonceCache(maxEntries int, replayWindow time.Duration) *NonceCache {
	if maxEntries <= 0 {
		maxEntries = 10000
	}
	if replayWindow <= 0 {
		replayWindow = 10 * time.Minute
	}
	return &NonceCache{
		entries:      make(map[string]time.Time),
		maxEntries:   maxEntries,
		replayWindow: replayWindow,
	}
}

func (c *NonceCache) CheckAndRecord(nonce string) bool {
	if nonce == "" {
		return true
	}

	now := time.Now().UTC()

	c.mu.Lock()
	defer c.mu.Unlock()

	for n, t := range c.entries {
		if now.Sub(t) > c.replayWindow {
			delete(c.entries, n)
		}
	}

	if seenAt, exists := c.entries[nonce]; exists {
		if now.Sub(seenAt) <= c.replayWindow {
			return false
		}
	}

	if len(c.entries) >= c.maxEntries {
		oldestNonce := ""
		oldestTime := now
		for n, t := range c.entries {
			if t.Before(oldestTime) {
				oldestTime = t
				oldestNonce = n
			}
		}
		if oldestNonce != "" {
			delete(c.entries, oldestNonce)
		}
	}

	c.entries[nonce] = now
	return true
}

func (c *NonceCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// RateLimiter provides defensive token-bucket rate limiting (bounded, with refill).
// Used for the privileged secrets paths (especially "secrets.update").
//
// Design goals (paranoid + practical):
// - Bounded memory and CPU protection for expensive operations (signature verification).
// - Generous but safe limits for normal Store-driven use.
// - Simple and self-contained.
type RateLimiter struct {
	mu         sync.Mutex
	tokens     int
	lastRefill time.Time
	maxTokens  int
	refill     time.Duration
}

func NewRateLimiter(maxTokens int, refill time.Duration) *RateLimiter {
	if maxTokens <= 0 {
		maxTokens = 10
	}
	if refill <= 0 {
		refill = time.Minute
	}
	return &RateLimiter{
		tokens:     maxTokens,
		lastRefill: time.Now().UTC(),
		maxTokens:  maxTokens,
		refill:     refill,
	}
}

func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	elapsed := now.Sub(r.lastRefill)

	if elapsed > 0 {
		refillCount := int(elapsed / r.refill)
		if refillCount > 0 {
			r.tokens += refillCount
			if r.tokens > r.maxTokens {
				r.tokens = r.maxTokens
			}
			r.lastRefill = now
		}
	}

	if r.tokens > 0 {
		r.tokens--
		return true
	}
	return false
}

// VerifyBoundarySignedResponse is the symmetric helper for the Store side.
// It verifies a response (e.g. from secrets.get) that was signed by the boundary
// using its registered private key.
//
// The payload should contain the "signer_pubkey" (base64) that was included in the response.
// The signature is the Message.Signature field from the wire format.
//
// This makes the mutual authentication story complete and easy to implement on the Store side.
func VerifyBoundarySignedResponse(payload map[string]interface{}, signatureB64 string, expectedPub []byte) bool {
	if len(expectedPub) != ed25519.PublicKeySize {
		return false
	}

	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return false
	}

	// For response verification we use a simple canonical form of the payload
	// (the same approach as incoming messages for consistency in the stub).
	data, _ := json.Marshal(payload)

	return ed25519.Verify(expectedPub, data, sig)
}
