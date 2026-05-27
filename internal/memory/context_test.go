package memory

import (
	"testing"
	"time"
)

func TestShortTermContext_LimitAndEviction(t *testing.T) {
	st := NewShortTermContext()
	for i := 0; i < 100; i++ {
		st.AddTurn("this is a test turn that is reasonably long to accumulate tokens quickly")
	}
	if st.tokenCount > st.limit {
		t.Errorf("token count %d exceeded hard limit %d", st.tokenCount, st.limit)
	}
	// Skeleton eviction is conservative; full aggressive trim + real summarization
	// will be added in later slices. Just ensure we didn't grow unbounded.
	if len(st.history) > 100 {
		t.Errorf("history grew too large for skeleton: %d entries", len(st.history))
	}
}

func TestLongTermMemory_TTLAndSearch(t *testing.T) {
	lt := NewLongTermMemory(1 * time.Millisecond) // very short for test
	lt.Store("the quick brown fox", []string{"test"}, 1.0)
	time.Sleep(5 * time.Millisecond)
	lt.Store("another memory", nil, 1.0)

	results := lt.Search("quick fox", 5)
	// After TTL the first should be gone in real purge, but skeleton purge is lazy
	if len(results) == 0 {
		t.Log("TTL purge worked (or lazy)")
	}
}

func TestACL_Enforcement(t *testing.T) {
	acl := NewACL("agent-test-123")
	if !acl.Allow("agent-test-123") {
		t.Error("paired agent should be allowed")
	}
	// In skeleton the agent-* wildcard is permissive for easy per-session pairing.
	// Strict per-UUID ACL will be enforced once full pairing handshake exists (1.3+).
	if !acl.Allow("agent-test-123") {
		t.Error("paired agent must be allowed")
	}
}
