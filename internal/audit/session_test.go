package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionLogCreatesFile(t *testing.T) {
	dir := t.TempDir()

	sl, err := NewSessionLog(dir)
	if err != nil {
		t.Fatalf("NewSessionLog: %v", err)
	}
	defer sl.Close()

	if sl.SessionID() == "" {
		t.Fatal("SessionID should not be empty")
	}

	// Verify chat-sessions subdirectory was created.
	sessDir := filepath.Join(dir, "chat-sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		t.Fatalf("reading chat-sessions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	// File permissions should be 0600.
	info, err := entries[0].Info()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected file perm 0600, got %o", perm)
	}
}

func TestSessionLogEvents(t *testing.T) {
	dir := t.TempDir()

	sl, err := NewSessionLog(dir)
	if err != nil {
		t.Fatalf("NewSessionLog: %v", err)
	}

	sl.Log(SessionEvent{Event: EventUserMessage, Role: "user", Content: "hello"})
	sl.Log(SessionEvent{Event: EventAssistantMessage, Role: "assistant", Content: "hi there"})
	sl.Log(SessionEvent{Event: EventToolCall, ToolName: "proposal.create_draft", ToolArgs: `{"title":"test"}`})
	sl.Log(SessionEvent{Event: EventToolResult, ToolName: "proposal.create_draft", Content: "Draft created"})
	sl.Log(SessionEvent{Event: EventSlashCommand, Role: "user", Content: "/status"})

	sl.Close()

	// Read back and verify events.
	sessDir := filepath.Join(dir, "chat-sessions")
	entries, _ := os.ReadDir(sessDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	f, err := os.Open(filepath.Join(sessDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	var events []SessionEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var evt SessionEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		events = append(events, evt)
	}

	// session_start + 5 logged + session_end = 7
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d", len(events))
	}

	// Verify event types in order.
	expectedTypes := []SessionEventType{
		EventSessionStart,
		EventUserMessage,
		EventAssistantMessage,
		EventToolCall,
		EventToolResult,
		EventSlashCommand,
		EventSessionEnd,
	}
	for i, want := range expectedTypes {
		if events[i].Event != want {
			t.Errorf("event %d: expected type %s, got %s", i, want, events[i].Event)
		}
	}

	// All events should have the same session ID.
	sid := events[0].SessionID
	for i, evt := range events {
		if evt.SessionID != sid {
			t.Errorf("event %d: session ID mismatch: %s != %s", i, evt.SessionID, sid)
		}
		if evt.Timestamp.IsZero() {
			t.Errorf("event %d: timestamp should not be zero", i)
		}
	}

	// Spot-check content.
	if events[1].Content != "hello" {
		t.Errorf("user message content: got %q", events[1].Content)
	}
	if events[3].ToolName != "proposal.create_draft" {
		t.Errorf("tool call name: got %q", events[3].ToolName)
	}
	if events[3].ToolArgs != `{"title":"test"}` {
		t.Errorf("tool call args: got %q", events[3].ToolArgs)
	}
}

func TestSessionLogErrorField(t *testing.T) {
	dir := t.TempDir()

	sl, err := NewSessionLog(dir)
	if err != nil {
		t.Fatalf("NewSessionLog: %v", err)
	}

	sl.Log(SessionEvent{Event: EventToolResult, ToolName: "fail_tool", Error: "something broke"})
	sl.Close()

	sessDir := filepath.Join(dir, "chat-sessions")
	entries, _ := os.ReadDir(sessDir)
	f, _ := os.Open(filepath.Join(sessDir, entries[0].Name()))
	defer f.Close()

	var events []SessionEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var evt SessionEvent
		json.Unmarshal(scanner.Bytes(), &evt)
		events = append(events, evt)
	}

	// session_start + 1 logged + session_end = 3
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[1].Error != "something broke" {
		t.Errorf("error field: got %q", events[1].Error)
	}
}

func TestSessionLogDirPermissions(t *testing.T) {
	dir := t.TempDir()

	sl, err := NewSessionLog(dir)
	if err != nil {
		t.Fatalf("NewSessionLog: %v", err)
	}
	sl.Close()

	info, err := os.Stat(filepath.Join(dir, "chat-sessions"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("expected dir perm 0700, got %o", perm)
	}
}
