package eventbus

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	// DefaultMaxPendingItems is the hard cap on active timers + subscriptions.
	DefaultMaxPendingItems = 20
	// timerCheckInterval is how often the daemon polls for due timers.
	timerCheckInterval = time.Minute
)

// TimerCheckInterval returns the interval used by the background timer daemon.
func TimerCheckInterval() time.Duration { return timerCheckInterval }

// Config holds construction parameters for the EventBus.
type Config struct {
	// Dir is where all event bus JSON files are stored.
	Dir string
	// MaxPendingTimers is the cap on active timers. Default: DefaultMaxPendingItems.
	MaxPendingTimers int
	// MaxSubscriptions is the cap on active subscriptions. Default: DefaultMaxPendingItems.
	MaxSubscriptions int
}

// FiredEvent carries information about a timer that just fired, used by the
// WakeupDispatcher callback to inject context into the agent.
type FiredEvent struct {
	Timer  *Timer
	Signal *Signal
}

// WakeupFunc is called by the timer daemon whenever a timer fires.
// Implementations should restore the appropriate snapshot and inject
// the signal as the agent's first Observation.
type WakeupFunc func(event FiredEvent)

// Bus is the central Event Bus for AegisClaw.
// It coordinates timers, subscriptions, approvals and the background timer daemon.
// All exported methods are safe for concurrent use.
type Bus struct {
	cfg       Config
	timers    *timerStore
	subs      *subscriptionStore
	approvals *approvalStore
	onFire    WakeupFunc // optional callback when a timer fires
}

// New opens (or creates) the EventBus at cfg.Dir.
func New(cfg Config) (*Bus, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("event bus directory is required")
	}
	if cfg.MaxPendingTimers <= 0 {
		cfg.MaxPendingTimers = DefaultMaxPendingItems
	}
	if cfg.MaxSubscriptions <= 0 {
		cfg.MaxSubscriptions = DefaultMaxPendingItems
	}

	if err := os.MkdirAll(cfg.Dir, 0700); err != nil {
		return nil, fmt.Errorf("create event bus dir %s: %w", cfg.Dir, err)
	}

	ts, err := newTimerStore(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("open timer store: %w", err)
	}
	ss, err := newSubscriptionStore(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("open subscription store: %w", err)
	}
	as, err := newApprovalStore(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("open approval store: %w", err)
	}

	return &Bus{
		cfg:       cfg,
		timers:    ts,
		subs:      ss,
		approvals: as,
	}, nil
}

// SetWakeupFunc registers the callback invoked when a timer fires.
// Must be called before starting the timer daemon (or not at all for tests).
func (b *Bus) SetWakeupFunc(fn WakeupFunc) { b.onFire = fn }

// ──────────────────────────────────────────────────────────────────────────────
// Timer API
// ──────────────────────────────────────────────────────────────────────────────

// SetTimerParams are the inputs for creating a new timer.
type SetTimerParams struct {
	Name      string
	Type      TimerType
	TriggerAt *time.Time     // for one-shot
	Cron      string         // for cron
	Payload   json.RawMessage
	TaskID    string
	Owner     string
}

// SetTimer creates and persists a new timer, returning its ID.
// Returns ErrResourceLimit if the active timer count would exceed MaxPendingTimers.
func (b *Bus) SetTimer(p SetTimerParams) (*Timer, error) {
	if p.Name == "" {
		return nil, fmt.Errorf("timer name is required")
	}
	if p.Type == "" {
		if p.TriggerAt != nil {
			p.Type = TimerOneShot
		} else if p.Cron != "" {
			p.Type = TimerCron
		} else {
			return nil, fmt.Errorf("timer requires either trigger_at (one-shot) or cron expression")
		}
	}
	if p.Type == TimerOneShot && p.TriggerAt == nil {
		return nil, fmt.Errorf("one-shot timer requires trigger_at")
	}
	if p.Type == TimerCron && p.Cron == "" {
		return nil, fmt.Errorf("cron timer requires cron expression")
	}
	if p.Owner == "" {
		p.Owner = "agent"
	}

	if b.timers.countActive() >= b.cfg.MaxPendingTimers {
		return nil, fmt.Errorf("resource limit: cannot create more than %d active timers", b.cfg.MaxPendingTimers)
	}

	now := time.Now().UTC()
	t := &Timer{
		TimerID:   newTimerID(),
		Name:      p.Name,
		Type:      p.Type,
		TriggerAt: p.TriggerAt,
		Cron:      p.Cron,
		Payload:   p.Payload,
		TaskID:    p.TaskID,
		Owner:     p.Owner,
		CreatedAt: now,
		Status:    TimerActive,
	}
	if p.Type == TimerCron {
		next := NextCronTime(p.Cron, now)
		t.NextFireAt = &next
	}

	if err := b.timers.set(t); err != nil {
		return nil, fmt.Errorf("persist timer: %w", err)
	}
	return t, nil
}

// CancelTimer marks a timer as cancelled.
// Returns (true, nil) if the timer was successfully cancelled,
// (false, nil) if the timer was not found or already terminal.
func (b *Bus) CancelTimer(timerID string) (bool, error) {
	t, ok := b.timers.get(timerID)
	if !ok {
		return false, nil
	}
	if t.Status != TimerActive {
		return false, nil
	}
	t.Status = TimerCancelled
	if err := b.timers.set(t); err != nil {
		return false, fmt.Errorf("cancel timer: %w", err)
	}
	return true, nil
}

// GetTimer returns a timer by ID.
func (b *Bus) GetTimer(timerID string) (*Timer, bool) { return b.timers.get(timerID) }

// ListTimers returns timers. Pass "" to list all; otherwise filter by status.
func (b *Bus) ListTimers(status TimerStatus) []*Timer { return b.timers.list(status) }

// ──────────────────────────────────────────────────────────────────────────────
// Subscription API
// ──────────────────────────────────────────────────────────────────────────────

// Subscribe registers an agent's interest in signals from a source.
func (b *Bus) Subscribe(source SignalSource, filter json.RawMessage, taskID, owner string) (*Subscription, error) {
	if source == "" {
		return nil, fmt.Errorf("signal source is required")
	}
	if owner == "" {
		owner = "agent"
	}

	if b.subs.countActive() >= b.cfg.MaxSubscriptions {
		return nil, fmt.Errorf("resource limit: cannot create more than %d active subscriptions", b.cfg.MaxSubscriptions)
	}

	sub := &Subscription{
		SubscriptionID: newSubID(),
		Source:         source,
		Filter:         filter,
		TaskID:         taskID,
		Owner:          owner,
		CreatedAt:      time.Now().UTC(),
		Active:         true,
	}
	if err := b.subs.addSub(sub); err != nil {
		return nil, fmt.Errorf("persist subscription: %w", err)
	}
	return sub, nil
}

// Unsubscribe deactivates a subscription.
func (b *Bus) Unsubscribe(subscriptionID string) (bool, error) {
	return b.subs.deactivateSub(subscriptionID)
}

// GetSubscription returns a subscription by ID.
func (b *Bus) GetSubscription(id string) (*Subscription, bool) { return b.subs.getSub(id) }

// ListSubscriptions returns subscriptions. Pass activeOnly=true to skip inactive ones.
func (b *Bus) ListSubscriptions(activeOnly bool) []*Subscription { return b.subs.listSubs(activeOnly) }

// ListSignals returns received signals, optionally filtered by taskID.
func (b *Bus) ListSignals(taskID string, limit int) []*Signal { return b.subs.listSignals(taskID, limit) }

// ──────────────────────────────────────────────────────────────────────────────
// Approval API
// ──────────────────────────────────────────────────────────────────────────────

// RequestApproval creates a new human approval request.
func (b *Bus) RequestApproval(title, description, riskLevel, requestedBy, taskID string, payload json.RawMessage, expiresIn time.Duration) (*ApprovalRequest, error) {
	if title == "" {
		return nil, fmt.Errorf("approval title is required")
	}
	if requestedBy == "" {
		requestedBy = "agent"
	}
	now := time.Now().UTC()
	a := &ApprovalRequest{
		ApprovalID:  newApprovalID(),
		Title:       title,
		Description: description,
		RiskLevel:   riskLevel,
		Payload:     payload,
		TaskID:      taskID,
		RequestedBy: requestedBy,
		CreatedAt:   now,
		Status:      ApprovalPending,
	}
	if expiresIn > 0 {
		exp := now.Add(expiresIn)
		a.ExpiresAt = &exp
	}
	if err := b.approvals.add(a); err != nil {
		return nil, fmt.Errorf("persist approval: %w", err)
	}
	return a, nil
}

// DecideApproval records a human's approve/reject decision.
func (b *Bus) DecideApproval(approvalID string, approved bool, decidedBy, reason string) error {
	return b.approvals.decide(approvalID, approved, decidedBy, reason)
}

// GetApproval returns an approval request by ID.
func (b *Bus) GetApproval(id string) (*ApprovalRequest, bool) { return b.approvals.get(id) }

// ListPendingApprovals returns all pending approval requests.
func (b *Bus) ListPendingApprovals() []*ApprovalRequest { return b.approvals.listPending() }

// ListApprovals returns all approval requests (any status), newest first.
func (b *Bus) ListApprovals() []*ApprovalRequest { return b.approvals.list() }

// PendingApprovalCount returns the number of pending approvals.
func (b *Bus) PendingApprovalCount() int { return b.approvals.countPending() }

// ──────────────────────────────────────────────────────────────────────────────
// Timer daemon (CheckAndFire)
// ──────────────────────────────────────────────────────────────────────────────

// CheckAndFire checks for due timers, fires them, and returns the fired events.
// This is called by the background daemon goroutine.
// It is also safe to call directly in tests (no goroutines needed).
func (b *Bus) CheckAndFire() []FiredEvent {
	now := time.Now().UTC()
	due := b.timers.dueTimers(now)
	var fired []FiredEvent

	for _, t := range due {
		// Mark the timer.
		switch t.Type {
		case TimerOneShot:
			t.Status = TimerFired
		case TimerCron:
			// Cron timers stay active; advance NextFireAt.
			next := NextCronTime(t.Cron, now)
			t.NextFireAt = &next
			t.FiredCount++
		}
		t.LastFiredAt = &now
		if err := b.timers.set(t); err != nil {
			continue
		}

		// Record a signal for the fired timer.
		sig := &Signal{
			SignalID:   newSignalID(),
			Source:     SourceTimer,
			Type:       "timer",
			Payload:    t.Payload,
			TaskID:     t.TaskID,
			TimerID:    t.TimerID,
			ReceivedAt: now,
		}
		if err := b.subs.addSignal(sig); err != nil {
			// Log via the onFire callback context if available; best-effort.
			_ = err
		}

		event := FiredEvent{Timer: t, Signal: sig}
		fired = append(fired, event)

		if b.onFire != nil {
			b.onFire(event)
		}
	}

	// Also expire any pending approvals past their deadline.
	b.expireApprovals(now)
	return fired
}

// expireApprovals marks pending approvals as expired when they pass ExpiresAt.
func (b *Bus) expireApprovals(now time.Time) {
	pending := b.approvals.listPending()
	for _, a := range pending {
		if a.ExpiresAt != nil && now.After(*a.ExpiresAt) {
			b.approvals.mu.Lock()
			if ap, ok := b.approvals.data[a.ApprovalID]; ok {
				ap.Status = ApprovalExpired
				b.approvals.save() //nolint:errcheck - best-effort; logged at startup on next load
			}
			b.approvals.mu.Unlock()
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Summary helpers (used by system prompt injection)
// ──────────────────────────────────────────────────────────────────────────────

// PendingSummary returns a one-line summary of pending async items for
// injection into the agent system prompt (e.g. "2 active timers, 1 pending approval").
func (b *Bus) PendingSummary() string {
	activeTimers := b.timers.countActive()
	activeSubs := b.subs.countActive()
	pendingApprovals := b.approvals.countPending()

	var parts []string
	if activeTimers > 0 {
		parts = append(parts, fmt.Sprintf("%d active timer(s)", activeTimers))
	}
	if activeSubs > 0 {
		parts = append(parts, fmt.Sprintf("%d active subscription(s)", activeSubs))
	}
	if pendingApprovals > 0 {
		parts = append(parts, fmt.Sprintf("%d pending approval(s)", pendingApprovals))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}
