// Package worker implements ephemeral Worker agent spawning for AegisClaw Phase 3.
//
// The Orchestrator (main agent VM) can spawn short-lived Worker microVMs to
// execute narrowly-scoped subtasks (research, coding, summarization, etc.) and
// return structured results.  Workers are destroyed on completion or timeout,
// keeping the attack surface and resource usage minimal.
//
// Lifecycle:
//  1. Orchestrator calls spawn_worker → WorkerManager.Spawn().
//  2. Worker VM is created, LLM proxy started, task injected as system prompt.
//  3. Worker runs its own ReAct loop (same agent binary, specialized prompt).
//  4. Worker completes (or times out) → result written to WorkerRecord.
//  5. VM is stopped and deleted (ephemeral).
//  6. Orchestrator retrieves result via worker_status.
//
// All state is persisted as JSON so records survive daemon restarts.
// Every lifecycle event is Merkle-audited.
package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Role defines the specialisation of a Worker agent.
type Role string

const (
	RoleResearcher  Role = "researcher"
	RoleCoder       Role = "coder"
	RoleSummarizer  Role = "summarizer"
	RoleCustom      Role = "custom"
)

// WorkerStatus reflects the lifecycle state of a Worker.
type WorkerStatus string

const (
	StatusSpawning  WorkerStatus = "spawning"
	StatusRunning   WorkerStatus = "running"
	StatusDone      WorkerStatus = "done"
	StatusFailed    WorkerStatus = "failed"
	StatusTimedOut  WorkerStatus = "timed_out"
	StatusDestroyed WorkerStatus = "destroyed"
)

// WorkerRecord persists metadata and results for a single Worker invocation.
type WorkerRecord struct {
	// WorkerID is a UUID assigned at spawn time.
	WorkerID string `json:"worker_id"`
	// VMID is the Firecracker sandbox ID of this worker.
	VMID string `json:"vm_id,omitempty"`
	// Role is the specialisation applied to this worker.
	Role Role `json:"role"`
	// TaskDescription is the human-readable description of the subtask.
	TaskDescription string `json:"task_description"`
	// ToolsGranted is the explicit allow-list of tools available to this worker.
	ToolsGranted []string `json:"tools_granted,omitempty"`
	// TaskID links the worker to an async task.
	TaskID string `json:"task_id,omitempty"`
	// SpawnedBy is the component (always "orchestrator") that spawned this worker.
	SpawnedBy string `json:"spawned_by"`
	// SpawnedAt is when the worker was created.
	SpawnedAt time.Time `json:"spawned_at"`
	// FinishedAt is when the worker completed (or timed out).
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// TimeoutAt is the hard deadline for this worker.
	TimeoutAt time.Time `json:"timeout_at"`
	// Status is the current lifecycle state.
	Status WorkerStatus `json:"status"`
	// Result is the output produced by the worker on success.
	Result string `json:"result,omitempty"`
	// Error describes any failure.
	Error string `json:"error,omitempty"`
	// StepCount is the number of ReAct iterations the worker completed.
	StepCount int `json:"step_count"`
}

// Store persists WorkerRecords to a JSON file.
type Store struct {
	path string
	mu   sync.RWMutex
	data map[string]*WorkerRecord
}

const workersFileName = "workers.json"

// NewStore opens (or creates) the worker record store at dir.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create worker dir %s: %w", dir, err)
	}
	s := &Store{
		path: filepath.Join(dir, workersFileName),
		data: make(map[string]*WorkerRecord),
	}
	return s, s.load()
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read workers: %w", err)
	}
	var items []*WorkerRecord
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("parse workers: %w", err)
	}
	for _, w := range items {
		s.data[w.WorkerID] = w
	}
	return nil
}

func (s *Store) save() error {
	items := make([]*WorkerRecord, 0, len(s.data))
	for _, w := range s.data {
		items = append(items, w)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].SpawnedAt.Before(items[j].SpawnedAt)
	})
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workers: %w", err)
	}
	return atomicWriteFile(s.path, raw)
}

// Upsert creates or overwrites a worker record.
func (s *Store) Upsert(w *WorkerRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *w
	s.data[cp.WorkerID] = &cp
	return s.save()
}

// Get returns a copy of the worker record with the given ID.
func (s *Store) Get(id string) (*WorkerRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, ok := s.data[id]
	if !ok {
		return nil, false
	}
	cp := *w
	return &cp, true
}

// List returns all worker records sorted by spawn time (newest first if reverse=true).
func (s *Store) List(activeOnly bool) []*WorkerRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*WorkerRecord
	for _, w := range s.data {
		if activeOnly && w.Status != StatusSpawning && w.Status != StatusRunning {
			continue
		}
		cp := *w
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SpawnedAt.After(out[j].SpawnedAt)
	})
	return out
}

// CountActive returns the number of workers in spawning or running state.
func (s *Store) CountActive() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, w := range s.data {
		if w.Status == StatusSpawning || w.Status == StatusRunning {
			n++
		}
	}
	return n
}

// atomicWriteFile writes data to path atomically via a temp-file rename.
func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
