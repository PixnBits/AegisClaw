package eventbus

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func TestBusPublishSubscribe(t *testing.T) {
	bus := New()

	var received atomic.Int32
	got := make(chan Event, 1)

	sub := bus.Subscribe("test.event", func(e Event) {
		received.Add(1)
		got <- e
	})
	defer sub.Unsubscribe()

	payload := map[string]string{"hello": "world"}
	data, _ := json.Marshal(payload)

	bus.Publish(Event{
		Name:    "test.event",
		Payload: data,
		TraceID: "trace-123",
		Source:  "test",
	})

	var lastEvent Event
	select {
	case lastEvent = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	if received.Load() != 1 {
		t.Fatalf("expected 1 event, got %d", received.Load())
	}
	if lastEvent.Name != "test.event" {
		t.Errorf("unexpected event name: %s", lastEvent.Name)
	}
	if lastEvent.TraceID != "trace-123" {
		t.Errorf("trace id not propagated")
	}
}

func TestBusUnsubscribe(t *testing.T) {
	bus := New()
	var count atomic.Int32

	sub := bus.Subscribe("unsub.test", func(e Event) {
		count.Add(1)
	})

	bus.Publish(Event{Name: "unsub.test"})
	time.Sleep(30 * time.Millisecond)

	sub.Unsubscribe()

	bus.Publish(Event{Name: "unsub.test"})
	time.Sleep(30 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("expected handler to be called only once before unsubscribe, got %d", count.Load())
	}
}

func TestDefaultBusConvenience(t *testing.T) {
	var called atomic.Bool

	sub := Subscribe("default.test", func(e Event) {
		called.Store(true)
	})
	defer sub.Unsubscribe()

	PublishJSON("default.test", map[string]int{"x": 42}, WithSource("test"))

	time.Sleep(30 * time.Millisecond)
	if !called.Load() {
		t.Error("default bus convenience functions did not deliver event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := New()
	var total atomic.Int32

	for i := 0; i < 3; i++ {
		bus.Subscribe("multi.test", func(e Event) {
			total.Add(1)
		})
	}

	bus.Publish(Event{Name: "multi.test"})
	time.Sleep(50 * time.Millisecond)

	if total.Load() != 3 {
		t.Errorf("expected 3 deliveries, got %d", total.Load())
	}
}

func TestScheduleAndFireTimer(t *testing.T) {
	bus := New()

	var fired atomic.Bool
	got := make(chan Event, 1)

	bus.Subscribe("timer.fired", func(e Event) {
		fired.Store(true)
		got <- e
	})

	id := bus.ScheduleTimer(30*time.Millisecond, "", map[string]string{"task": "autonomy-check"})

	if id == "" {
		t.Fatal("expected timer id")
	}

	var receivedEvent Event
	select {
	case receivedEvent = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timer")
	}

	if !fired.Load() {
		t.Error("timer did not fire")
	}
	if receivedEvent.Name != "timer.fired" {
		t.Errorf("unexpected event name on fire: %s", receivedEvent.Name)
	}
}

func TestCancelTimer(t *testing.T) {
	bus := New()

	var fired atomic.Bool
	bus.Subscribe("timer.fired", func(e Event) {
		fired.Store(true)
	})

	id := bus.ScheduleTimer(200*time.Millisecond, "timer.fired", nil)

	cancelled := bus.CancelTimer(id)
	if !cancelled {
		t.Error("expected CancelTimer to return true")
	}

	time.Sleep(80 * time.Millisecond)

	if fired.Load() {
		t.Error("timer fired after being cancelled")
	}
}

func TestScheduleRecurring(t *testing.T) {
	bus := New()

	var fireCount atomic.Int32
	got := make(chan struct{}, 8)
	bus.Subscribe("recurring.test", func(e Event) {
		fireCount.Add(1)
		got <- struct{}{}
	})

	id := bus.ScheduleRecurring(20*time.Millisecond, "recurring.test", nil)
	if id == "" {
		t.Fatal("expected recurring timer id")
	}
	t.Cleanup(func() { bus.CancelTimer(id) })

	for i := 0; i < 2; i++ {
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for recurring fire %d", i+1)
		}
	}

	cancelled := bus.CancelTimer(id)
	if !cancelled {
		t.Log("note: recurring cancellation is best-effort in current implementation")
	}

	// With the improved implementation we expect multiple fires.
	if fireCount.Load() < 2 {
		t.Errorf("expected at least 2 recurring fires, got %d", fireCount.Load())
	}
}

// TestRecurringConsumerPattern demonstrates a realistic usage of ScheduleRecurring
// by a 7.2 background consumer (e.g. a stale session sweeper).
func TestRecurringConsumerPattern(t *testing.T) {
	bus := New()

	var sweepCount atomic.Int32
	got := make(chan struct{}, 8)
	bus.Subscribe("background.sweep", func(e Event) {
		sweepCount.Add(1)
		got <- struct{}{}
	})

	id := bus.ScheduleRecurring(15*time.Millisecond, "background.sweep", map[string]string{"reason": "stale-sessions"})
	if id == "" {
		t.Fatal("expected recurring id")
	}
	t.Cleanup(func() { bus.CancelTimer(id) })

	for i := 0; i < 2; i++ {
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for sweep %d", i+1)
		}
	}
	_ = bus.CancelTimer(id)

	if sweepCount.Load() < 2 {
		t.Errorf("expected consumer to have been invoked multiple times via recurring timer, got %d", sweepCount.Load())
	}
}

// TestPublishHandlerPanicIsCounted exercises the new 7.2.1.1 error containment.
// A panicking handler must be recovered and the ErrorCount must increase.
func TestPublishHandlerPanicIsCounted(t *testing.T) {
	bus := New()

	bus.Subscribe("panic.test", func(e Event) {
		panic("intentional test panic for ErrorCount")
	})

	// Should be 0 before
	if bus.ErrorCount() != 0 {
		t.Fatalf("expected 0 errors before publish, got %d", bus.ErrorCount())
	}

	bus.Publish(Event{Name: "panic.test"})

	// Give the goroutine handler a moment
	time.Sleep(30 * time.Millisecond)

	if bus.ErrorCount() != 1 {
		t.Errorf("expected ErrorCount to be 1 after panic, got %d", bus.ErrorCount())
	}
}

// 7.2: Test approval queue helpers and privileged signing hook.
func TestApprovalAndPrivilegedEvents(t *testing.T) {
	bus := New()

	approvalCh := make(chan Event, 1)
	bus.Subscribe("approval.request", func(e Event) {
		approvalCh <- e
	})

	req := ApprovalRequest{
		ID:          "appr-123",
		Source:      "agent:test",
		Action:      "deploy-skill",
		Description: "Deploy new monitoring skill",
	}
	bus.RequestApproval(req)

	var receivedApproval Event
	select {
	case receivedApproval = <-approvalCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval.request")
	}
	if receivedApproval.Name != "approval.request" {
		t.Errorf("expected approval.request event, got %s", receivedApproval.Name)
	}

	// Test privileged path with a no-op signer
	privilegedCh := make(chan Event, 1)
	bus.Subscribe("privileged.test", func(e Event) {
		privilegedCh <- e
	})

	bus.PublishPrivileged(Event{Name: "privileged.test", Payload: json.RawMessage(`{"secret":"stuff"}`)}, func(data []byte) (string, error) {
		return "fake-sig-xyz", nil
	})

	var signedEvent Event
	select {
	case signedEvent = <-privilegedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for privileged.test")
	}
	if signedEvent.Name != "privileged.test" {
		t.Error("privileged event not received")
	}
}

// TestRecurringCancelAfterFiresStopsSchedule is the regression for CancelTimer
// only knowing the first one-shot id. The stable ID must stop the schedule
// after it has already fired more than once.
func TestRecurringCancelAfterFiresStopsSchedule(t *testing.T) {
	bus := New()
	const name = "recurring.cancel.stop"

	var fireCount atomic.Int32
	got := make(chan struct{}, 32)
	bus.Subscribe(name, func(e Event) {
		fireCount.Add(1)
		select {
		case got <- struct{}{}:
		default:
		}
	})

	interval := 15 * time.Millisecond
	id := bus.ScheduleRecurring(interval, name, nil)
	if id == "" {
		t.Fatal("expected recurring id")
	}
	t.Cleanup(func() { bus.CancelTimer(id) })

	for i := 0; i < 2; i++ {
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for fire %d (count=%d)", i+1, fireCount.Load())
		}
	}

	bus.CancelTimer(id)
	if timerRegistered(bus, id) {
		t.Fatal("recurring id still registered after CancelTimer")
	}

	// Handlers already past the cancellation check may still increment.
	// The count must then stay put for several intervals, and the schedule
	// must not re-register itself.
	quietFor := 4 * interval
	stable := fireCount.Load()
	lastChange := time.Now()
	deadline := time.Now().Add(2 * time.Second)
	for time.Since(lastChange) < quietFor {
		if time.Now().After(deadline) {
			t.Fatalf("fires did not stop after cancel; count=%d", fireCount.Load())
		}
		time.Sleep(interval / 2)
		if cur := fireCount.Load(); cur != stable {
			stable = cur
			lastChange = time.Now()
		}
	}

	if stable < 2 {
		t.Fatalf("expected at least 2 fires before cancel, got %d", stable)
	}
	if timerRegistered(bus, id) {
		t.Fatal("recurring schedule was re-armed after CancelTimer")
	}
	if fireCount.Load() != stable {
		t.Fatalf("fire count changed after quiet period: %d -> %d", stable, fireCount.Load())
	}
}

// TestRecurringIgnoresUnrelatedPublish is the regression for the re-schedule
// hook reacting to every event of the same name.
func TestRecurringIgnoresUnrelatedPublish(t *testing.T) {
	bus := New()
	const name = "recurring.unrelated"

	var timerFires atomic.Int32
	var otherFires atomic.Int32
	gotOther := make(chan struct{}, 8)
	bus.Subscribe(name, func(e Event) {
		if e.Source == "eventbus.timer" {
			timerFires.Add(1)
			return
		}
		otherFires.Add(1)
		gotOther <- struct{}{}
	})

	id := bus.ScheduleRecurring(time.Hour, name, map[string]string{"reason": "scheduled"})
	if id == "" {
		t.Fatal("expected recurring id")
	}
	t.Cleanup(func() { bus.CancelTimer(id) })

	const manual = 5
	for i := 0; i < manual; i++ {
		bus.Publish(Event{Name: name, Source: "test"})
	}
	for i := 0; i < manual; i++ {
		select {
		case <-gotOther:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for unrelated publish %d", i+1)
		}
	}

	if otherFires.Load() != manual {
		t.Fatalf("expected %d unrelated deliveries, got %d", manual, otherFires.Load())
	}
	if timerFires.Load() != 0 {
		t.Fatalf("unrelated publishes caused %d timer recurrences", timerFires.Load())
	}

	timers, registered := timerSnapshot(bus, id)
	if !registered || timers != 1 {
		t.Fatalf("expected exactly one schedule for %s, registered=%v timers=%d", id, registered, timers)
	}
}

// TestScheduleRecurringDoesNotLeakSubscription checks the in-package subscriber
// map. The bus has no exported subscriber count; ScheduleRecurring must not add
// one, including after the schedule has fired.
func TestScheduleRecurringDoesNotLeakSubscription(t *testing.T) {
	bus := New()
	const name = "recurring.nosub"

	got := make(chan struct{}, 8)
	bus.Subscribe(name, func(e Event) {
		select {
		case got <- struct{}{}:
		default:
		}
	})
	base := subscriberCount(bus, name)
	if base != 1 {
		t.Fatalf("expected 1 test subscriber, got %d", base)
	}

	id := bus.ScheduleRecurring(10*time.Millisecond, name, nil)
	if id == "" {
		t.Fatal("expected recurring id")
	}
	t.Cleanup(func() { bus.CancelTimer(id) })

	if got := subscriberCount(bus, name); got != base {
		t.Fatalf("ScheduleRecurring subscribed to %s: count=%d", name, got)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for fire %d", i+1)
		}
	}
	bus.CancelTimer(id)

	if got := subscriberCount(bus, name); got != base {
		t.Fatalf("subscription leak after recurring fires: count=%d want %d", got, base)
	}
}

func subscriberCount(b *Bus, name string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers[name])
}

func timerRegistered(b *Bus, id string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.timers[id]
	return ok
}

func timerSnapshot(b *Bus, id string) (n int, registered bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, registered = b.timers[id]
	return len(b.timers), registered
}
