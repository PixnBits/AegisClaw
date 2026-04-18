// Package eventbus implements the host-level Event Bus for AegisClaw Phase 2.
//
// It provides:
//   - A persistent Timer service (one-shot and cron) for async agent wakeups.
//   - A Signal Subscription registry for external event sources.
//   - A Human Approval queue for high-risk operations.
//
// All state is persisted as JSON files in the event bus directory.
// Every mutation is recorded in the Merkle audit tree before being applied.
//
// Resource guardrail: the combined count of active timers + subscriptions is
// capped at MaxPendingItems (default 20) to prevent runaway async growth.
package eventbus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TimerType distinguishes one-shot from recurring timers.
type TimerType string

const (
	TimerOneShot TimerType = "one-shot"
	TimerCron    TimerType = "cron"
)

// TimerStatus tracks lifecycle state of a timer.
type TimerStatus string

const (
	TimerActive    TimerStatus = "active"
	TimerFired     TimerStatus = "fired"
	TimerCancelled TimerStatus = "cancelled"
	TimerExpired   TimerStatus = "expired"
)

// Timer is a scheduled wakeup entry.
type Timer struct {
	// TimerID is a UUID assigned on creation.
	TimerID string `json:"timer_id"`
	// Name is a human-readable label.
	Name string `json:"name"`
	// Type is "one-shot" or "cron".
	Type TimerType `json:"type"`
	// TriggerAt is the UTC time for one-shot timers (nil for cron).
	TriggerAt *time.Time `json:"trigger_at,omitempty"`
	// Cron is the cron expression for recurring timers (empty for one-shot).
	// Supported: "@daily", "@hourly", "@weekly", "@monthly",
	//             "*/N" style (every N minutes) when used as "*/N * * * *",
	//             and standard 5-field cron.
	Cron string `json:"cron,omitempty"`
	// Payload is arbitrary JSON passed to the agent on wakeup.
	Payload json.RawMessage `json:"payload,omitempty"`
	// TaskID links the timer to an async task (optional).
	TaskID string `json:"task_id,omitempty"`
	// Owner is the identity that created the timer.
	Owner string `json:"owner"`
	// CreatedAt is the creation timestamp.
	CreatedAt time.Time `json:"created_at"`
	// LastFiredAt is the most recent fire time (nil = never fired).
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
	// NextFireAt is computed for cron timers (nil for one-shot after fire).
	NextFireAt *time.Time `json:"next_fire_at,omitempty"`
	// Status reflects the current lifecycle state.
	Status TimerStatus `json:"status"`
	// FiredCount tracks how many times a cron timer has fired.
	FiredCount int `json:"fired_count"`
}

// timerStore is the persistent storage layer for timers.
type timerStore struct {
	path string
	mu   sync.RWMutex
	data map[string]*Timer
}

const timersFileName = "timers.json"

func newTimerStore(dir string) (*timerStore, error) {
	ts := &timerStore{
		path: filepath.Join(dir, timersFileName),
		data: make(map[string]*Timer),
	}
	return ts, ts.load()
}

func (ts *timerStore) load() error {
	raw, err := os.ReadFile(ts.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read timers: %w", err)
	}
	var items []*Timer
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("parse timers: %w", err)
	}
	for _, t := range items {
		ts.data[t.TimerID] = t
	}
	return nil
}

func (ts *timerStore) save() error {
	items := make([]*Timer, 0, len(ts.data))
	for _, t := range ts.data {
		items = append(items, t)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal timers: %w", err)
	}
	return atomicWriteFile(ts.path, raw)
}

// set creates or overwrites a timer entry.
// It takes the lock and stores a defensive copy so callers cannot mutate persisted state.
func (ts *timerStore) set(t *Timer) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	cp := *t
	ts.data[cp.TimerID] = &cp
	return ts.save()
}

// get returns a copy of the timer with the given ID.
func (ts *timerStore) get(id string) (*Timer, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	t, ok := ts.data[id]
	if !ok {
		return nil, false
	}
	cp := *t
	return &cp, true
}

// list returns all timers matching the given status filter.
// Pass "" to return all timers.
func (ts *timerStore) list(status TimerStatus) []*Timer {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	var out []*Timer
	for _, t := range ts.data {
		if status != "" && t.Status != status {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// countActive returns the number of active timers.
func (ts *timerStore) countActive() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	n := 0
	for _, t := range ts.data {
		if t.Status == TimerActive {
			n++
		}
	}
	return n
}

// dueTimers returns all active timers whose trigger time has passed.
func (ts *timerStore) dueTimers(now time.Time) []*Timer {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	var due []*Timer
	for _, t := range ts.data {
		if t.Status != TimerActive {
			continue
		}
		switch t.Type {
		case TimerOneShot:
			if t.TriggerAt != nil && !t.TriggerAt.After(now) {
				cp := *t
				due = append(due, &cp)
			}
		case TimerCron:
			if t.NextFireAt != nil && !t.NextFireAt.After(now) {
				cp := *t
				due = append(due, &cp)
			}
		}
	}
	return due
}

// ──────────────────────────────────────────────────────────────────────────────
// Cron helpers
// ──────────────────────────────────────────────────────────────────────────────

// NextCronTime computes the next fire time for a cron expression relative to now.
// Supports: "@daily", "@hourly", "@weekly", "@monthly", and "*/N * * * *" (every N minutes).
// For unsupported patterns, returns now+1h as a safe fallback.
func NextCronTime(expr string, from time.Time) time.Time {
	expr = strings.TrimSpace(expr)
	switch expr {
	case "@hourly":
		return from.Add(time.Hour).Truncate(time.Hour)
	case "@daily":
		next := from.Add(24 * time.Hour)
		return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, time.UTC)
	case "@weekly":
		// Advance to the next Monday (start of week). If today is Monday, go 7 days ahead.
		days := (int(time.Monday) - int(from.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		next := from.AddDate(0, 0, days)
		return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, time.UTC)
	case "@monthly":
		y, m, _ := from.Date()
		m++
		if m > 12 {
			m = 1
			y++
		}
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	case "@quarterly":
		// Advance to the first day of the next calendar quarter.
		// Quarter boundaries are months January (1), April (4), July (7), October (10).
		// Formula: current quarter index = (month-1)/3 (integer division, 0-based).
		// Next quarter start month = ((currentQuarterIndex + 1) * 3) + 1.
		y, m, _ := from.Date()
		nextQMonth := nextQuarterStartMonth(int(m))
		if nextQMonth > 12 {
			nextQMonth -= 12
			y++
		}
		return time.Date(y, time.Month(nextQMonth), 1, 0, 0, 0, 0, time.UTC)
	}
	// Try "*/N * * * *" style (every N minutes).
	if strings.HasPrefix(expr, "*/") {
		parts := strings.Fields(expr)
		if len(parts) >= 1 {
			var n int
			if _, err := fmt.Sscanf(parts[0], "*/%d", &n); err == nil && n > 0 {
				interval := time.Duration(n) * time.Minute
				// Round from down to the last interval boundary then advance one interval.
				// This ensures the result is always strictly after from.
				next := from.Truncate(interval).Add(interval)
				if !next.After(from) {
					next = next.Add(interval)
				}
				return next
			}
		}
	}
	// Fallback: every hour.
	return from.Add(time.Hour)
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// nextQuarterStartMonth returns the month number (1–12) of the first month of
// the calendar quarter that follows the quarter containing monthNum.
// Quarter boundaries: Q1→Jan(1), Q2→Apr(4), Q3→Jul(7), Q4→Oct(10).
// The returned value may be 13, which the caller should normalise by
// subtracting 12 and incrementing the year.
func nextQuarterStartMonth(monthNum int) int {
	// (monthNum-1)/3 gives the 0-based current quarter index (0..3).
	// Multiplying the next index by 3 and adding 1 gives the 1-based start month.
	return ((monthNum-1)/3+1)*3 + 1
}

// atomicWriteFile writes data to path atomically via a temp-file rename.
func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// newTimerID generates a new UUID string.
func newTimerID() string { return uuid.New().String() }
