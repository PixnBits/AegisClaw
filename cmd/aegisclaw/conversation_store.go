package main

// conversationStore provides append-only, host-side persistence for conversation
// history between agent sessions (architecture.md §8.1, PRD §10.6 A2).
//
// # Design rationale
//
// The target architecture (§8.1) places conversation history inside the agent
// VM's Firecracker boundary. This implementation is an interim daemon-side
// store — valid while D2-a (full ReAct loop in agent VM) is still open.
// When D2-a is resolved and the agent VM maintains its own in-memory history,
// this store will be migrated inside the VM boundary.
//
// # Security properties
//   - No secrets are ever written here. The store records conversation roles
//     and text only; secrets are injected at runtime via the secrets proxy and
//     never appear in LLM context or conversation history.
//   - The store directory is created with mode 0700 (user-only access).
//   - File is created with mode 0600.
//   - JSON lines are validated on read; malformed lines are skipped with a
//     warning rather than crashing — a partially corrupted file degrades
//     gracefully to the messages that could be parsed.
//
// # File format
//
// One JSON object per line (JSONL), each representing a single agentChatMsg:
//
//	{"role":"user","content":"what skills are available?"}
//	{"role":"assistant","content":"Here are your active skills…"}
//
// The file is append-only at runtime; old entries are never removed during a
// session. On startup, the daemon reads the last HistoryMaxMessages lines to
// seed the new session context.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConversationStore is a simple append-only JSONL store for conversation
// messages.  It is safe for sequential use by a single goroutine (the daemon's
// chat handler holds the agentVMMu lock for each turn).
type ConversationStore struct {
	path    string
	maxMsgs int
	file    *os.File
}

// maxConvLineSizeBytes is the maximum size of a single JSON line in the
// conversation store. Lines larger than this are rejected by the scanner.
// 1 MB is generous — a normal conversation message is well under 10 KB.
const maxConvLineSizeBytes = 1024 * 1024 // 1 MB
// maxMsgs is the maximum number of past messages returned by LoadHistory.
// Pass maxMsgs=0 to disable history loading (the file is still written).
func openConversationStore(dir string, maxMsgs int) (*ConversationStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("conversation store: create dir %q: %w", dir, err)
	}
	p := filepath.Join(dir, "conversation.jsonl")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("conversation store: open %q: %w", p, err)
	}
	return &ConversationStore{path: p, maxMsgs: maxMsgs, file: f}, nil
}

// Append writes msg as a JSON line at the end of the store file.
// Roles "system" are skipped — system prompts are rebuilt each session.
// Security: callers must ensure msg.Content contains no raw secrets.
func (s *ConversationStore) Append(msg agentChatMsg) error {
	if msg.Role == "system" {
		return nil // system prompts are per-session; do not persist them
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("conversation store: marshal: %w", err)
	}
	line = append(line, '\n')
	if _, err := s.file.Write(line); err != nil {
		return fmt.Errorf("conversation store: write: %w", err)
	}
	return nil
}

// AppendTurn persists all non-system messages from a completed chat turn.
// It is called once after the assistant returns its final response.
func (s *ConversationStore) AppendTurn(msgs []agentChatMsg) error {
	for _, m := range msgs {
		if err := s.Append(m); err != nil {
			return err
		}
	}
	return nil
}

// LoadHistory reads the last s.maxMsgs non-system messages from the store.
// Returns nil (no error) when the file does not exist yet or maxMsgs == 0.
// Malformed JSON lines are silently skipped to avoid crashing on corruption.
func (s *ConversationStore) LoadHistory() ([]agentChatMsg, error) {
	if s.maxMsgs == 0 {
		return nil, nil
	}
	rf, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("conversation store: open for read %q: %w", s.path, err)
	}
	defer rf.Close()

	// Read all lines, keep only the last maxMsgs valid non-system messages.
	var all []agentChatMsg
	scanner := bufio.NewScanner(rf)
	scanner.Buffer(make([]byte, maxConvLineSizeBytes), maxConvLineSizeBytes) // cap individual lines
	for scanner.Scan() {
		var m agentChatMsg
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			continue // skip malformed lines
		}
		if m.Role == "system" {
			continue
		}
		all = append(all, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("conversation store: scan %q: %w", s.path, err)
	}

	if len(all) <= s.maxMsgs {
		return all, nil
	}
	return all[len(all)-s.maxMsgs:], nil
}

// Close flushes and closes the underlying file.
func (s *ConversationStore) Close() error {
	return s.file.Close()
}
