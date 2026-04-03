package worker_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/worker"
)

func TestStore_UpsertGetList(t *testing.T) {
	dir := t.TempDir()
	s, err := worker.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	now := time.Now().UTC()
	w := &worker.WorkerRecord{
		WorkerID:        "wid-1",
		Role:            worker.RoleResearcher,
		TaskDescription: "research Go generics",
		SpawnedBy:       "orchestrator",
		SpawnedAt:       now,
		TimeoutAt:       now.Add(30 * time.Minute),
		Status:          worker.StatusRunning,
	}

	if err := s.Upsert(w); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, ok := s.Get("wid-1")
	if !ok {
		t.Fatal("expected record to exist")
	}
	if got.Role != worker.RoleResearcher {
		t.Errorf("role mismatch: %s", got.Role)
	}
	if got.Status != worker.StatusRunning {
		t.Errorf("status mismatch: %s", got.Status)
	}

	active := s.List(true)
	if len(active) != 1 {
		t.Errorf("expected 1 active, got %d", len(active))
	}

	// Mark done.
	w.Status = worker.StatusDone
	w.Result = `{"findings":"done"}`
	s.Upsert(w)

	active = s.List(true)
	if len(active) != 0 {
		t.Errorf("expected 0 active after completion, got %d", len(active))
	}
	all := s.List(false)
	if len(all) != 1 {
		t.Errorf("expected 1 total, got %d", len(all))
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	s1, err := worker.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	w := &worker.WorkerRecord{
		WorkerID:        "wid-persist",
		Role:            worker.RoleCoder,
		TaskDescription: "implement foo",
		SpawnedAt:       now,
		TimeoutAt:       now.Add(time.Hour),
		Status:          worker.StatusDone,
		Result:          "done",
	}
	if err := s1.Upsert(w); err != nil {
		t.Fatal(err)
	}

	// Reopen.
	s2, err := worker.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get("wid-persist")
	if !ok {
		t.Fatal("record not persisted")
	}
	if got.Result != "done" {
		t.Errorf("result mismatch: %q", got.Result)
	}
}

func TestStore_CountActive(t *testing.T) {
	dir := t.TempDir()
	s, _ := worker.NewStore(dir)
	now := time.Now().UTC()

	for i, status := range []worker.WorkerStatus{
		worker.StatusRunning, worker.StatusSpawning, worker.StatusDone,
	} {
		s.Upsert(&worker.WorkerRecord{
			WorkerID:  fmt.Sprintf("w%d", i),
			SpawnedAt: now,
			TimeoutAt: now.Add(time.Hour),
			Status:    status,
		})
	}
	if n := s.CountActive(); n != 2 {
		t.Errorf("expected 2 active, got %d", n)
	}
}

func TestRolePrompts(t *testing.T) {
	for _, role := range []worker.Role{
		worker.RoleResearcher, worker.RoleCoder, worker.RoleSummarizer, worker.RoleCustom,
	} {
		p := worker.RolePrompt(role)
		if p == "" {
			t.Errorf("empty prompt for role %s", role)
		}
	}
}

func TestRoleTimeouts(t *testing.T) {
	if worker.RoleDefaultTimeoutMins(worker.RoleResearcher) <= 0 {
		t.Error("researcher timeout must be positive")
	}
	if worker.RoleDefaultTimeoutMins(worker.RoleCoder) <= 0 {
		t.Error("coder timeout must be positive")
	}
}
