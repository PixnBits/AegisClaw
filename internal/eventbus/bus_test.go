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
	var lastEvent Event

	sub := bus.Subscribe("test.event", func(e Event) {
		received.Add(1)
		lastEvent = e
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

	// Give the goroutine handler a moment
	time.Sleep(50 * time.Millisecond)

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
	var receivedEvent Event

	bus.Subscribe("timer.fired", func(e Event) {
		fired.Store(true)
		receivedEvent = e
	})

	id := bus.ScheduleTimer(30*time.Millisecond, "", map[string]string{"task": "autonomy-check"})

	if id == "" {
		t.Fatal("expected timer id")
	}

	time.Sleep(80 * time.Millisecond)

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