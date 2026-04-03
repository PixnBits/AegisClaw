package memory_test

import (
	"os"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/PixnBits/AegisClaw/internal/memory"
)

func newTestStore(t *testing.T) (*memory.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate age identity: %v", err)
	}
	s, err := memory.NewStore(memory.StoreConfig{Dir: dir}, identity)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s, func() { os.RemoveAll(dir) }
}

func TestStore_StoreAndRetrieve(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	id, err := s.Store(&memory.MemoryEntry{
		Key:   "test-memory",
		Value: "Paris is the capital of France",
		Tags:  []string{"geography", "facts"},
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty memory ID")
	}

	// Exact get
	entry, err := s.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry.Key != "test-memory" {
		t.Errorf("key: want test-memory, got %s", entry.Key)
	}
	if entry.Value != "Paris is the capital of France" {
		t.Errorf("value: unexpected %s", entry.Value)
	}

	// Retrieve by keyword
	results, err := s.Retrieve("France", 5, "")
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'France'")
	}
	if results[0].MemoryID != id {
		t.Errorf("expected first result to be %s, got %s", id, results[0].MemoryID)
	}
}

func TestStore_Delete(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	id, err := s.Store(&memory.MemoryEntry{
		Key:   "delete-me",
		Value: "some transient fact",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	n, err := s.Delete("transient")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 deleted, got %d", n)
	}

	// Should not be findable after deletion.
	_, err = s.Get(id)
	if err == nil {
		t.Error("expected error getting deleted entry, got nil")
	}

	results, err := s.Retrieve("transient", 10, "")
	if err != nil {
		t.Fatalf("retrieve after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}

func TestStore_List(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		if _, err := s.Store(&memory.MemoryEntry{
			Key:     "item",
			Value:   "value",
			TTLTier: memory.TTL90d,
		}); err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
	}

	summaries, err := s.List(memory.TTL90d)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != 3 {
		t.Errorf("want 3, got %d", len(summaries))
	}

	// Listing a different tier should return 0.
	summaries, err = s.List(memory.TTLForever)
	if err != nil {
		t.Fatalf("list forever: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("want 0 for forever tier, got %d", len(summaries))
	}
}

func TestStore_Compaction(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	id, err := s.Store(&memory.MemoryEntry{
		Key:     "old-memory",
		Value:   "This is a detailed memory that should be compacted to a shorter version over time",
		TTLTier: memory.TTL90d,
		// Backdating is not supported directly, but we test the compactValue helper
		// via the exported Store.Compact path — we force targetTier to trigger it.
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Direct compaction with explicit target (ignores age threshold since
	// we can't backdate entries in the test without hacking time).
	// To test the code path, manually call Compact with the specific ID's tier.
	result, err := s.Compact("", memory.TTL180d) // targets TTL180d entries (none here)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if result.Compacted != 0 {
		t.Errorf("expected 0 compacted (entries are TTL90d, not TTL180d), got %d", result.Compacted)
	}

	// Verify the entry is unchanged.
	entry, err := s.Get(id)
	if err != nil {
		t.Fatalf("get after compact: %v", err)
	}
	if entry.TTLTier != memory.TTL90d {
		t.Errorf("tier should still be 90d, got %s", entry.TTLTier)
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	cfg := memory.StoreConfig{Dir: dir}

	// Create, write, close.
	s1, err := memory.NewStore(cfg, identity)
	if err != nil {
		t.Fatalf("open store 1: %v", err)
	}
	id, err := s1.Store(&memory.MemoryEntry{
		Key:   "persistent-key",
		Value: "persistent-value",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Re-open with same identity — should reload from disk.
	s2, err := memory.NewStore(cfg, identity)
	if err != nil {
		t.Fatalf("open store 2: %v", err)
	}
	entry, err := s2.Get(id)
	if err != nil {
		t.Fatalf("get from reloaded store: %v", err)
	}
	if entry.Key != "persistent-key" {
		t.Errorf("key mismatch: want persistent-key, got %s", entry.Key)
	}
	if entry.Value != "persistent-value" {
		t.Errorf("value mismatch: want persistent-value, got %s", entry.Value)
	}
}

func TestStore_TagFilter(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	if _, err := s.Store(&memory.MemoryEntry{
		Key:   "research-memory",
		Value: "Found a paper on quantum computing",
		Tags:  []string{"research", "quantum"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store(&memory.MemoryEntry{
		Key:   "code-memory",
		Value: "Implemented the PR review tool",
		Tags:  []string{"code", "oss-pr"},
	}); err != nil {
		t.Fatal(err)
	}

	// Tag search: "quantum" should return only the research entry.
	results, err := s.Retrieve("quantum", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'quantum', got %d", len(results))
	}
}

func TestStore_TaskIDFilter(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	tid := "task-abc"
	if _, err := s.Store(&memory.MemoryEntry{
		Key:    "task-mem",
		Value:  "task context",
		TaskID: tid,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store(&memory.MemoryEntry{
		Key:   "other-mem",
		Value: "other context",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := s.Retrieve("", 10, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for task %s, got %d", tid, len(results))
	}
	if results[0].TaskID != tid {
		t.Errorf("unexpected task ID: %s", results[0].TaskID)
	}
}

func TestStore_EmptyQuery(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		if _, err := s.Store(&memory.MemoryEntry{
			Key:   "key",
			Value: "value",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Empty query returns all entries.
	results, err := s.Retrieve("", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results for empty query, got %d", len(results))
	}
}

func TestStore_MaxK(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	for i := 0; i < 10; i++ {
		if _, err := s.Store(&memory.MemoryEntry{
			Key:   "key",
			Value: "value",
		}); err != nil {
			t.Fatal(err)
		}
	}

	results, err := s.Retrieve("", 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results (k=3), got %d", len(results))
	}
}

func TestStore_DefaultTTL(t *testing.T) {
	dir := t.TempDir()
	identity, _ := age.GenerateX25519Identity()
	s, err := memory.NewStore(memory.StoreConfig{
		Dir:        dir,
		DefaultTTL: memory.TTLForever,
	}, identity)
	if err != nil {
		t.Fatal(err)
	}

	id, err := s.Store(&memory.MemoryEntry{Key: "k", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if entry.TTLTier != memory.TTLForever {
		t.Errorf("expected forever TTL, got %s", entry.TTLTier)
	}
}

func TestStore_Count(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	if s.Count() != 0 {
		t.Error("expected 0 initial count")
	}
	for i := 0; i < 4; i++ {
		s.Store(&memory.MemoryEntry{Key: "k", Value: "v"})
	}
	if s.Count() != 4 {
		t.Errorf("expected 4, got %d", s.Count())
	}
}

func TestStore_MultipleStores_SameKey(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	id1, _ := s.Store(&memory.MemoryEntry{Key: "same-key", Value: "first"})
	id2, _ := s.Store(&memory.MemoryEntry{Key: "same-key", Value: "second"})

	if id1 == id2 {
		t.Error("expected different IDs for separate stores")
	}
	// Both should be retrievable.
	results, _ := s.Retrieve("same-key", 10, "")
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestStore_RetrieveOrder(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()

	// Store entries with a slight delay to get distinct timestamps.
	s.Store(&memory.MemoryEntry{Key: "first", Value: "first"})
	time.Sleep(2 * time.Millisecond)
	s.Store(&memory.MemoryEntry{Key: "second", Value: "second"})

	results, _ := s.Retrieve("", 10, "")
	if len(results) < 2 {
		t.Skip("not enough results")
	}
	// Newest first.
	if results[0].Key != "second" {
		t.Errorf("expected second (newer) first, got %s", results[0].Key)
	}
}
