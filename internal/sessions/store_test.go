package sessions

import (
	"testing"
	"time"
)

func TestStore_OpenAndGet(t *testing.T) {
	s := NewStore()

	r := s.Open("s1", "vm-abc")
	if r == nil {
		t.Fatal("Open returned nil")
	}
	if r.ID != "s1" {
		t.Errorf("expected ID=s1, got %q", r.ID)
	}
	if r.SandboxID != "vm-abc" {
		t.Errorf("expected SandboxID=vm-abc, got %q", r.SandboxID)
	}
	if r.Status != StatusIdle {
		t.Errorf("expected StatusIdle, got %q", r.Status)
	}

	got, ok := s.Get("s1")
	if !ok {
		t.Fatal("Get returned not-found for existing session")
	}
	if got.ID != "s1" {
		t.Errorf("Get: expected ID=s1, got %q", got.ID)
	}
}

func TestStore_OpenIdempotent(t *testing.T) {
	s := NewStore()
	s.Open("s1", "vm-abc")
	r2 := s.Open("s1", "vm-xyz") // should update SandboxID
	if r2.SandboxID != "vm-xyz" {
		t.Errorf("expected SandboxID=vm-xyz after second Open, got %q", r2.SandboxID)
	}
}

func TestStore_OpenGeneratesID(t *testing.T) {
	s := NewStore()
	r := s.Open("", "vm1")
	if r.ID == "" {
		t.Error("expected generated session ID, got empty string")
	}
}

func TestStore_AppendMessage(t *testing.T) {
	s := NewStore()
	s.Open("s1", "vm1")
	s.AppendMessage("s1", "vm1", "user", "hello")
	s.AppendMessage("s1", "vm1", "assistant", "hi there")

	msgs, err := s.History("s1", 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("unexpected msg[0]: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi there" {
		t.Errorf("unexpected msg[1]: %+v", msgs[1])
	}
}

func TestStore_AppendMessageCreatesSession(t *testing.T) {
	s := NewStore()
	// AppendMessage should create the session if it doesn't exist.
	s.AppendMessage("new-session", "vm2", "user", "hello")
	msgs, err := s.History("new-session", 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestStore_HistoryLimit(t *testing.T) {
	s := NewStore()
	s.Open("s1", "vm1")
	for i := 0; i < 10; i++ {
		s.AppendMessage("s1", "vm1", "user", "msg")
	}
	msgs, err := s.History("s1", 3)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages with limit=3, got %d", len(msgs))
	}
}

func TestStore_HistoryNotFound(t *testing.T) {
	s := NewStore()
	_, err := s.History("nonexistent", 0)
	if err == nil {
		t.Error("expected error for non-existent session, got nil")
	}
}

func TestStore_SetStatus(t *testing.T) {
	s := NewStore()
	s.Open("s1", "vm1")
	s.SetStatus("s1", StatusActive)

	r, ok := s.Get("s1")
	if !ok {
		t.Fatal("session not found")
	}
	if r.Status != StatusActive {
		t.Errorf("expected StatusActive, got %q", r.Status)
	}

	s.Close("s1")
	r, _ = s.Get("s1")
	if r.Status != StatusClosed {
		t.Errorf("expected StatusClosed after Close, got %q", r.Status)
	}
}

func TestStore_SetStatusNoOp(t *testing.T) {
	s := NewStore()
	// Must not panic for a nonexistent session.
	s.SetStatus("nonexistent", StatusActive)
}

func TestStore_List(t *testing.T) {
	s := NewStore()
	s.Open("s1", "")
	s.Open("s2", "vm2")

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
}

func TestStore_MessageCap(t *testing.T) {
	s := NewStore()
	s.Open("s1", "vm1")

	// Append more messages than the cap.
	for i := 0; i < maxMessagesPerSession+50; i++ {
		s.AppendMessage("s1", "vm1", "user", "m")
	}
	msgs, _ := s.History("s1", 0)
	if len(msgs) != maxMessagesPerSession {
		t.Errorf("expected history capped at %d, got %d", maxMessagesPerSession, len(msgs))
	}
}

func TestStore_Eviction(t *testing.T) {
	s := NewStore()

	// Fill up to the cap with idle sessions.
	for i := 0; i < maxSessions; i++ {
		s.Open(GenerateID(), "")
	}
	if len(s.sessions) != maxSessions {
		t.Fatalf("expected %d sessions, got %d", maxSessions, len(s.sessions))
	}

	// Opening one more should trigger eviction; we should still be at cap.
	s.Open(GenerateID(), "")
	if len(s.sessions) != maxSessions {
		t.Errorf("expected eviction to keep count at %d, got %d", maxSessions, len(s.sessions))
	}
}

func TestStore_LastActiveAt(t *testing.T) {
	s := NewStore()
	s.Open("s1", "vm1")
	before := time.Now()
	time.Sleep(time.Millisecond)
	s.AppendMessage("s1", "vm1", "user", "hello")

	r, _ := s.Get("s1")
	if !r.LastActiveAt.After(before) {
		t.Error("LastActiveAt should be updated after AppendMessage")
	}
}

func TestGenerateID(t *testing.T) {
	a := GenerateID()
	b := GenerateID()
	if a == "" || b == "" {
		t.Error("GenerateID returned empty string")
	}
	if a == b {
		t.Error("GenerateID returned same value twice")
	}
}
