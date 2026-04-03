package eventbus_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/eventbus"
)

func newTestBus(t *testing.T) (*eventbus.Bus, func()) {
	t.Helper()
	dir := t.TempDir()
	b, err := eventbus.New(eventbus.Config{Dir: dir})
	if err != nil {
		t.Fatalf("new bus: %v", err)
	}
	return b, func() { os.RemoveAll(dir) }
}

// ──────────────────────────────────────────────────────────────────────────────
// Timer tests
// ──────────────────────────────────────────────────────────────────────────────

func TestBus_SetTimer_OneShot(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	triggerAt := time.Now().UTC().Add(time.Hour)
	timer, err := b.SetTimer(eventbus.SetTimerParams{
		Name:      "my-timer",
		TriggerAt: &triggerAt,
		Owner:     "test",
	})
	if err != nil {
		t.Fatalf("SetTimer: %v", err)
	}
	if timer.TimerID == "" {
		t.Fatal("expected non-empty timer ID")
	}
	if timer.Status != eventbus.TimerActive {
		t.Errorf("expected active, got %s", timer.Status)
	}
}

func TestBus_SetTimer_Cron(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	timer, err := b.SetTimer(eventbus.SetTimerParams{
		Name:  "daily-summary",
		Cron:  "@daily",
		Owner: "agent",
	})
	if err != nil {
		t.Fatalf("SetTimer cron: %v", err)
	}
	if timer.NextFireAt == nil {
		t.Fatal("expected NextFireAt for cron timer")
	}
	if timer.Type != eventbus.TimerCron {
		t.Errorf("expected cron type, got %s", timer.Type)
	}
}

func TestBus_CancelTimer(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	triggerAt := time.Now().UTC().Add(time.Hour)
	timer, err := b.SetTimer(eventbus.SetTimerParams{
		Name:      "cancel-me",
		TriggerAt: &triggerAt,
		Owner:     "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	ok, err := b.CancelTimer(timer.TimerID)
	if err != nil {
		t.Fatalf("CancelTimer: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	got, found := b.GetTimer(timer.TimerID)
	if !found {
		t.Fatal("expected timer to still exist")
	}
	if got.Status != eventbus.TimerCancelled {
		t.Errorf("expected cancelled, got %s", got.Status)
	}
}

func TestBus_CheckAndFire_OneShot(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	// A timer that fired 1 second ago.
	past := time.Now().UTC().Add(-time.Second)
	timer, err := b.SetTimer(eventbus.SetTimerParams{
		Name:      "past-timer",
		TriggerAt: &past,
		Owner:     "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	var fired []eventbus.FiredEvent
	b.SetWakeupFunc(func(e eventbus.FiredEvent) {
		fired = append(fired, e)
	})

	events := b.CheckAndFire()
	if len(events) != 1 {
		t.Fatalf("expected 1 fired event, got %d", len(events))
	}
	if events[0].Timer.TimerID != timer.TimerID {
		t.Errorf("wrong timer ID: %s", events[0].Timer.TimerID)
	}
	if len(fired) != 1 {
		t.Error("WakeupFunc not called")
	}

	// Timer should now be in "fired" state.
	got, _ := b.GetTimer(timer.TimerID)
	if got.Status != eventbus.TimerFired {
		t.Errorf("expected fired, got %s", got.Status)
	}

	// CheckAndFire again should not re-fire.
	events2 := b.CheckAndFire()
	if len(events2) != 0 {
		t.Errorf("expected 0 events on second check, got %d", len(events2))
	}
}

func TestBus_CheckAndFire_Cron(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	// Create a cron timer with @daily — guaranteed to be ~24h in the future.
	timer, err := b.SetTimer(eventbus.SetTimerParams{
		Name:  "daily-summary",
		Cron:  "@daily",
		Owner: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Cron timer should be active and have a future NextFireAt.
	if timer.NextFireAt == nil {
		t.Fatal("expected NextFireAt for cron timer")
	}
	if !timer.NextFireAt.After(time.Now().UTC()) {
		t.Fatalf("NextFireAt should be in the future, got %v", timer.NextFireAt)
	}

	// CheckAndFire should not fire a timer whose NextFireAt is in the future.
	events := b.CheckAndFire()
	if len(events) != 0 {
		t.Errorf("future cron timer should not fire, got %d", len(events))
	}

	// Cron timer should still be active (not fired/cancelled).
	got, found := b.GetTimer(timer.TimerID)
	if !found {
		t.Fatal("cron timer missing")
	}
	if got.Status != eventbus.TimerActive {
		t.Errorf("expected active, got %s", got.Status)
	}
}

func TestBus_ResourceLimit_Timers(t *testing.T) {
	dir := t.TempDir()
	b, err := eventbus.New(eventbus.Config{Dir: dir, MaxPendingTimers: 2})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		triggerAt := time.Now().UTC().Add(time.Hour)
		if _, err := b.SetTimer(eventbus.SetTimerParams{
			Name:      "t",
			TriggerAt: &triggerAt,
			Owner:     "test",
		}); err != nil {
			t.Fatalf("expected success for timer %d: %v", i, err)
		}
	}

	triggerAt := time.Now().UTC().Add(time.Hour)
	_, err = b.SetTimer(eventbus.SetTimerParams{
		Name:      "overflow",
		TriggerAt: &triggerAt,
		Owner:     "test",
	})
	if err == nil {
		t.Error("expected resource limit error, got nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Subscription tests
// ──────────────────────────────────────────────────────────────────────────────

func TestBus_Subscribe_Unsubscribe(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	sub, err := b.Subscribe(eventbus.SourceEmail, nil, "task-1", "agent")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.SubscriptionID == "" {
		t.Fatal("empty subscription ID")
	}
	if !sub.Active {
		t.Error("expected active subscription")
	}

	ok, err := b.Unsubscribe(sub.SubscriptionID)
	if err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	got, found := b.GetSubscription(sub.SubscriptionID)
	if !found {
		t.Fatal("subscription missing after unsubscribe")
	}
	if got.Active {
		t.Error("expected inactive after unsubscribe")
	}
}

func TestBus_ResourceLimit_Subscriptions(t *testing.T) {
	dir := t.TempDir()
	b, err := eventbus.New(eventbus.Config{Dir: dir, MaxSubscriptions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Subscribe(eventbus.SourceGit, nil, "", "agent"); err != nil {
		t.Fatal(err)
	}
	_, err = b.Subscribe(eventbus.SourceWebhook, nil, "", "agent")
	if err == nil {
		t.Error("expected resource limit error")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Approval tests
// ──────────────────────────────────────────────────────────────────────────────

func TestBus_RequestApproval_Decide(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	payload, _ := json.Marshal(map[string]string{"action": "delete"})
	a, err := b.RequestApproval(
		"Delete production database",
		"The agent wants to delete the prod DB. This is irreversible.",
		"high",
		"agent",
		"task-999",
		payload,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if a.Status != eventbus.ApprovalPending {
		t.Errorf("expected pending, got %s", a.Status)
	}

	pending := b.ListPendingApprovals()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}

	// Approve it.
	if err := b.DecideApproval(a.ApprovalID, true, "admin", "looks fine"); err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}

	got, found := b.GetApproval(a.ApprovalID)
	if !found {
		t.Fatal("approval missing")
	}
	if got.Status != eventbus.ApprovalApproved {
		t.Errorf("expected approved, got %s", got.Status)
	}
	if got.DecidedBy != "admin" {
		t.Errorf("wrong decidedBy: %s", got.DecidedBy)
	}

	// No pending approvals remain.
	if b.PendingApprovalCount() != 0 {
		t.Error("expected 0 pending approvals after decision")
	}
}

func TestBus_Approval_Expiry(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	// Request an approval that expired in the past.
	a, err := b.RequestApproval("expiry-test", "", "low", "agent", "", nil, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// CheckAndFire advances the expiry logic.
	time.Sleep(5 * time.Millisecond)
	b.CheckAndFire()

	got, found := b.GetApproval(a.ApprovalID)
	if !found {
		t.Fatal("approval missing")
	}
	if got.Status != eventbus.ApprovalExpired {
		t.Errorf("expected expired, got %s", got.Status)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Persistence tests
// ──────────────────────────────────────────────────────────────────────────────

func TestBus_Persistence(t *testing.T) {
	dir := t.TempDir()

	b1, err := eventbus.New(eventbus.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	triggerAt := time.Now().UTC().Add(time.Hour)
	timer, err := b1.SetTimer(eventbus.SetTimerParams{
		Name:      "persistent-timer",
		TriggerAt: &triggerAt,
		Owner:     "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := b1.Subscribe(eventbus.SourceFile, nil, "task-p", "agent")
	if err != nil {
		t.Fatal(err)
	}

	// Reopen.
	b2, err := eventbus.New(eventbus.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, found := b2.GetTimer(timer.TimerID); !found {
		t.Error("timer not persisted")
	}
	if _, found := b2.GetSubscription(sub.SubscriptionID); !found {
		t.Error("subscription not persisted")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Cron helpers
// ──────────────────────────────────────────────────────────────────────────────

func TestNextCronTime(t *testing.T) {
	now := time.Date(2026, 4, 3, 10, 30, 0, 0, time.UTC)
	tests := []struct {
		expr string
		want func(time.Time) bool
	}{
		{"@hourly", func(next time.Time) bool { return next.After(now) && next.Minute() == 0 }},
		{"@daily", func(next time.Time) bool { return next.After(now) && next.Hour() == 0 }},
		{"*/15 * * * *", func(next time.Time) bool { return next.After(now) && next.Sub(now) <= 15*time.Minute }},
		{"*/5 * * * *", func(next time.Time) bool { return next.After(now) && next.Sub(now) <= 5*time.Minute }},
	}
	for _, tc := range tests {
		got := eventbus.NextCronTime(tc.expr, now)
		if !tc.want(got) {
			t.Errorf("NextCronTime(%q, %s) = %s — unexpected", tc.expr, now, got)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// PendingSummary
// ──────────────────────────────────────────────────────────────────────────────

func TestBus_PendingSummary(t *testing.T) {
	b, cleanup := newTestBus(t)
	defer cleanup()

	// Empty.
	if s := b.PendingSummary(); s != "" {
		t.Errorf("expected empty summary, got %q", s)
	}

	// Add a timer.
	triggerAt := time.Now().UTC().Add(time.Hour)
	b.SetTimer(eventbus.SetTimerParams{Name: "t", TriggerAt: &triggerAt, Owner: "test"})
	summary := b.PendingSummary()
	if summary == "" {
		t.Error("expected non-empty summary after adding timer")
	}
	if !containsString(summary, "timer") {
		t.Errorf("summary should mention timer: %q", summary)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := range s {
				if i+len(sub) <= len(s) && s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
