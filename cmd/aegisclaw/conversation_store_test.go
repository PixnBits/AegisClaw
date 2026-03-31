package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConversationStoreAppendLoad is a round-trip test for the append-only
// JSONL conversation store (PRD §10.6 A2 / architecture.md §8.1).
func TestConversationStoreAppendLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := openConversationStore(dir, 100)
	if err != nil {
		t.Fatalf("openConversationStore: %v", err)
	}

	msgs := []agentChatMsg{
		{Role: "system", Content: "You are the agent."}, // should be skipped
		{Role: "user", Content: "hello there"},
		{Role: "assistant", Content: "hi!"},
		{Role: "tool", Name: "list_skills", Content: "No skills."},
	}
	if err := store.AppendTurn(msgs); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	store.Close()

	// Re-open and load.
	store2, err := openConversationStore(dir, 100)
	if err != nil {
		t.Fatalf("second openConversationStore: %v", err)
	}
	defer store2.Close()

	history, err := store2.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	// System message should have been skipped.
	if len(history) != 3 {
		t.Fatalf("expected 3 messages (no system), got %d: %+v", len(history), history)
	}
	if history[0].Role != "user" || history[0].Content != "hello there" {
		t.Errorf("history[0] = %+v; want user/hello there", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "hi!" {
		t.Errorf("history[1] = %+v; want assistant/hi!", history[1])
	}
	if history[2].Role != "tool" || history[2].Name != "list_skills" {
		t.Errorf("history[2] = %+v; want tool/list_skills", history[2])
	}
}

// TestConversationStoreMaxMessages verifies that LoadHistory returns at most
// maxMsgs entries even when the file contains more.
func TestConversationStoreMaxMessages(t *testing.T) {
	dir := t.TempDir()
	store, err := openConversationStore(dir, 3)
	if err != nil {
		t.Fatalf("openConversationStore: %v", err)
	}
	for i := 0; i < 10; i++ {
		if err := store.Append(agentChatMsg{Role: "user", Content: fmt.Sprintf("msg %d", i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	store.Close()

	store2, err := openConversationStore(dir, 3)
	if err != nil {
		t.Fatalf("openConversationStore: %v", err)
	}
	defer store2.Close()

	history, err := store2.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 messages (maxMsgs=3), got %d", len(history))
	}
	// Should be the last 3 messages.
	for i, m := range history {
		want := fmt.Sprintf("msg %d", 7+i)
		if m.Content != want {
			t.Errorf("history[%d].Content = %q; want %q", i, m.Content, want)
		}
	}
}

// TestConversationStoreZeroMax verifies that LoadHistory returns nil when
// maxMsgs is 0 (history disabled).
func TestConversationStoreZeroMax(t *testing.T) {
	dir := t.TempDir()
	store, err := openConversationStore(dir, 0)
	if err != nil {
		t.Fatalf("openConversationStore: %v", err)
	}
	if err := store.Append(agentChatMsg{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	store.Close()

	store2, err := openConversationStore(dir, 0)
	if err != nil {
		t.Fatalf("openConversationStore: %v", err)
	}
	defer store2.Close()

	history, err := store2.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if history != nil {
		t.Errorf("expected nil history with maxMsgs=0, got %v", history)
	}
}

// TestConversationStoreNotExist verifies that LoadHistory on a missing file
// returns nil (no error) — there is simply no history yet.
func TestConversationStoreNotExist(t *testing.T) {
	dir := t.TempDir()
	// Don't write anything — file won't exist.
	store, err := openConversationStore(dir, 10)
	if err != nil {
		t.Fatalf("openConversationStore: %v", err)
	}
	store.Close()

	// Remove the file that was just created.
	os.Remove(filepath.Join(dir, "conversation.jsonl"))

	store2, err := openConversationStore(dir, 10)
	if err != nil {
		t.Fatalf("openConversationStore (no history): %v", err)
	}
	defer store2.Close()

	history, err := store2.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory on missing file: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected empty history, got %d messages", len(history))
	}
}

// TestConversationStoreSkipsSystemMessages verifies that system messages are
// never persisted (they are rebuilt per-session from the system prompt).
func TestConversationStoreSkipsSystemMessages(t *testing.T) {
	dir := t.TempDir()
	store, err := openConversationStore(dir, 100)
	if err != nil {
		t.Fatalf("openConversationStore: %v", err)
	}
	defer store.Close()

	if err := store.Append(agentChatMsg{Role: "system", Content: "DO NOT PERSIST"}); err != nil {
		t.Fatalf("Append system: %v", err)
	}
	if err := store.Append(agentChatMsg{Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("Append user: %v", err)
	}
	store.Close()

	store2, err := openConversationStore(dir, 100)
	if err != nil {
		t.Fatalf("openConversationStore 2: %v", err)
	}
	defer store2.Close()
	history, err := store2.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	for _, m := range history {
		if m.Role == "system" {
			t.Errorf("system message must not be persisted, got: %+v", m)
		}
		if strings.Contains(m.Content, "DO NOT PERSIST") {
			t.Errorf("system content must not be persisted, got: %+v", m)
		}
	}
}

// TestConversationStoreCorruptionTolerance verifies that malformed JSON lines
// are skipped gracefully rather than causing a fatal error.
func TestConversationStoreCorruptionTolerance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation.jsonl")

	// Write two valid lines, one corrupt line, and one valid line.
	content := `{"role":"user","content":"first"}
not-valid-json
{"role":"assistant","content":"second"}
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store, err := openConversationStore(dir, 100)
	if err != nil {
		t.Fatalf("openConversationStore: %v", err)
	}
	defer store.Close()

	history, err := store.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory with corrupt line: %v", err)
	}
	// Should get 2 valid messages (corrupt line skipped).
	if len(history) != 2 {
		t.Fatalf("expected 2 valid messages, got %d: %+v", len(history), history)
	}
}

// TestConversationStoreDirCreation verifies that the store creates its parent
// directory with safe permissions when it does not yet exist.
func TestConversationStoreDirCreation(t *testing.T) {
	base := t.TempDir()
	newDir := filepath.Join(base, "nested", "conversations")

	store, err := openConversationStore(newDir, 10)
	if err != nil {
		t.Fatalf("openConversationStore with missing dirs: %v", err)
	}
	store.Close()

	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("Stat created dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected a directory to be created")
	}
	// Permissions should be 0700 (user-only).
	if info.Mode().Perm() != 0700 {
		t.Errorf("dir permissions = %o; want 0700", info.Mode().Perm())
	}
}
