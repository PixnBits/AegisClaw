package proposal

import (
	"os"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	store, err := NewStore(dir, logger)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	return store, dir
}

func TestStoreCreate(t *testing.T) {
	store, _ := newTestStore(t)

	p, err := NewProposal("Test Proposal", "A test proposal for storage", CategoryNewSkill, "admin")
	if err != nil {
		t.Fatalf("NewProposal failed: %v", err)
	}

	if err := store.Create(p); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
}

func TestStoreCreateAndGet(t *testing.T) {
	store, _ := newTestStore(t)

	p, _ := NewProposal("Stored Proposal", "Testing get after create", CategoryEditSkill, "admin")
	p.TargetSkill = "logging"

	if err := store.Create(p); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	loaded, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if loaded.ID != p.ID {
		t.Errorf("ID mismatch: %q vs %q", loaded.ID, p.ID)
	}
	if loaded.Title != p.Title {
		t.Errorf("Title mismatch: %q vs %q", loaded.Title, p.Title)
	}
	if loaded.Category != p.Category {
		t.Errorf("Category mismatch")
	}
	if loaded.TargetSkill != p.TargetSkill {
		t.Errorf("TargetSkill mismatch: %q vs %q", loaded.TargetSkill, p.TargetSkill)
	}
}

func TestStoreUpdate(t *testing.T) {
	store, _ := newTestStore(t)

	p, _ := NewProposal("Updatable Proposal", "Will be updated", CategoryNewSkill, "admin")
	if err := store.Create(p); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := p.Transition(StatusSubmitted, "ready for review", "admin"); err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if err := store.Update(p); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	loaded, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("Get after update failed: %v", err)
	}
	if loaded.Status != StatusSubmitted {
		t.Errorf("expected status %q, got %q", StatusSubmitted, loaded.Status)
	}
	if loaded.Version != 2 {
		t.Errorf("expected version 2, got %d", loaded.Version)
	}
}

func TestStoreList(t *testing.T) {
	store, _ := newTestStore(t)

	for i := 0; i < 3; i++ {
		p, _ := NewProposal("Proposal "+string(rune('A'+i)), "Description", CategoryNewSkill, "admin")
		if err := store.Create(p); err != nil {
			t.Fatalf("Create %d failed: %v", i, err)
		}
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 proposals, got %d", len(list))
	}
}

func TestStoreListByStatus(t *testing.T) {
	store, _ := newTestStore(t)

	p1, _ := NewProposal("Draft One", "Stays draft", CategoryNewSkill, "admin")
	if err := store.Create(p1); err != nil {
		t.Fatal(err)
	}

	p2, _ := NewProposal("Submitted One", "Will be submitted", CategoryNewSkill, "admin")
	if err := store.Create(p2); err != nil {
		t.Fatal(err)
	}
	if err := p2.Transition(StatusSubmitted, "ready", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(p2); err != nil {
		t.Fatal(err)
	}

	drafts, err := store.ListByStatus(StatusDraft)
	if err != nil {
		t.Fatalf("ListByStatus failed: %v", err)
	}
	if len(drafts) != 1 {
		t.Errorf("expected 1 draft, got %d", len(drafts))
	}

	submitted, err := store.ListByStatus(StatusSubmitted)
	if err != nil {
		t.Fatalf("ListByStatus failed: %v", err)
	}
	if len(submitted) != 1 {
		t.Errorf("expected 1 submitted, got %d", len(submitted))
	}
}

func TestStoreGetNotFound(t *testing.T) {
	store, _ := newTestStore(t)

	_, err := store.Get("nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent proposal")
	}
}

func TestStoreEmptyPath(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	_, err := NewStore("", logger)
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestStoreInitCreatesRepo(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	_, err := NewStore(dir, logger)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Verify .git directory exists
	if _, err := os.Stat(dir + "/.git"); os.IsNotExist(err) {
		t.Error("expected .git directory to be created")
	}
}

func TestStoreReopenExisting(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	// Create store and add a proposal
	store1, err := NewStore(dir, logger)
	if err != nil {
		t.Fatal(err)
	}

	p, _ := NewProposal("Persistent", "Should survive reopen", CategoryNewSkill, "admin")
	if err := store1.Create(p); err != nil {
		t.Fatal(err)
	}

	// Reopen store
	store2, err := NewStore(dir, logger)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}

	loaded, err := store2.Get(p.ID)
	if err != nil {
		t.Fatalf("Get after reopen failed: %v", err)
	}
	if loaded.ID != p.ID {
		t.Error("proposal ID mismatch after reopen")
	}
}

func TestStoreResolveID(t *testing.T) {
	store, _ := newTestStore(t)

	p1, _ := NewProposal("First Proposal", "First desc", CategoryNewSkill, "admin")
	if err := store.Create(p1); err != nil {
		t.Fatal(err)
	}
	p2, _ := NewProposal("Second Proposal", "Second desc", CategoryNewSkill, "admin")
	if err := store.Create(p2); err != nil {
		t.Fatal(err)
	}

	// Full ID should resolve exactly.
	got, err := store.ResolveID(p1.ID)
	if err != nil {
		t.Fatalf("ResolveID full ID failed: %v", err)
	}
	if got != p1.ID {
		t.Errorf("expected %s, got %s", p1.ID, got)
	}

	// 8-char prefix should resolve (UUIDs are unique enough).
	got, err = store.ResolveID(p1.ID[:8])
	if err != nil {
		t.Fatalf("ResolveID prefix failed: %v", err)
	}
	if got != p1.ID {
		t.Errorf("expected %s, got %s", p1.ID, got)
	}

	// Empty ID should error.
	_, err = store.ResolveID("")
	if err == nil {
		t.Error("expected error for empty ID")
	}

	// Nonexistent prefix should error.
	_, err = store.ResolveID("zzzzzzzz")
	if err == nil {
		t.Error("expected error for nonexistent prefix")
	}
}
