package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func quietLog(t *testing.T) {
	t.Helper()
	prev := logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)
	t.Cleanup(func() { logrus.SetOutput(prev) })
}

func TestWaitForStoreReadyRetriesThenSucceeds(t *testing.T) {
	quietLog(t)

	const fails = 3
	var calls int
	var sleeps []time.Duration
	// 2s, 4s, then 8s capped at 5s — three failures exercise growth and the cap.
	opts := storeWaitOpts{
		perAttempt:     time.Second,
		initialBackoff: 2 * time.Second,
		multiplier:     2,
		maxBackoff:     5 * time.Second,
		sleep: func(ctx context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		},
		now: time.Now,
	}
	err := waitForStoreReady(context.Background(), func(ctx context.Context) error {
		calls++
		if calls <= fails {
			return errors.New("store not ready")
		}
		return nil
	}, opts)
	if err != nil {
		t.Fatalf("waitForStoreReady: %v", err)
	}
	if calls != fails+1 {
		t.Fatalf("probe calls = %d, want %d", calls, fails+1)
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 5 * time.Second}
	if len(sleeps) != len(want) {
		t.Fatalf("sleeps = %v, want %v", sleeps, want)
	}
	for i := range want {
		if sleeps[i] != want[i] {
			t.Fatalf("sleep[%d] = %s, want %s (all %v)", i, sleeps[i], want[i], sleeps)
		}
	}
}

func TestWaitForStoreReadyPerAttemptDeadline(t *testing.T) {
	quietLog(t)

	const perAttempt = 10 * time.Second
	var sawDeadline bool
	err := waitForStoreReady(context.Background(), func(ctx context.Context) error {
		dl, ok := ctx.Deadline()
		if !ok {
			t.Fatal("probe ctx has no deadline")
		}
		remain := time.Until(dl)
		if remain > perAttempt {
			t.Fatalf("deadline remaining %s exceeds per-attempt %s", remain, perAttempt)
		}
		if remain < perAttempt-250*time.Millisecond {
			t.Fatalf("deadline remaining %s is shorter than per-attempt %s", remain, perAttempt)
		}
		sawDeadline = true
		return nil
	}, storeWaitOpts{
		perAttempt:     perAttempt,
		initialBackoff: time.Hour,
		multiplier:     2,
		maxBackoff:     time.Hour,
		sleep: func(ctx context.Context, d time.Duration) error {
			t.Fatalf("sleep %s called on first-attempt success", d)
			return nil
		},
		now: time.Now,
	})
	if err != nil {
		t.Fatalf("waitForStoreReady: %v", err)
	}
	if !sawDeadline {
		t.Fatal("probe was not called")
	}
}

func TestWaitForStoreReadyCancelDuringBackoff(t *testing.T) {
	quietLog(t)

	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	var sleeps int
	start := time.Now()
	err := waitForStoreReady(ctx, func(ctx context.Context) error {
		calls++
		return errors.New("not yet")
	}, storeWaitOpts{
		perAttempt:     time.Second,
		initialBackoff: time.Hour,
		multiplier:     2,
		maxBackoff:     time.Hour,
		sleep: func(ctx context.Context, d time.Duration) error {
			sleeps++
			cancel()
			return sleepUntil(ctx, d)
		},
		now: time.Now,
	})
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
	if sleeps != 1 {
		t.Fatalf("sleeps = %d, want 1", sleeps)
	}
	if elapsed > time.Second {
		t.Fatalf("returned in %s, want promptly without the backoff wait", elapsed)
	}
}

func TestWaitForStoreReadyCancelBeforeStart(t *testing.T) {
	quietLog(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int
	start := time.Now()
	err := waitForStoreReady(ctx, func(ctx context.Context) error {
		calls++
		return nil
	}, storeWaitOpts{
		perAttempt:     time.Hour,
		initialBackoff: time.Hour,
		multiplier:     2,
		maxBackoff:     time.Hour,
		sleep: func(ctx context.Context, d time.Duration) error {
			t.Fatalf("sleep %s called after pre-cancelled ctx", d)
			return nil
		},
		now: time.Now,
	})
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("probe calls = %d, want 0", calls)
	}
	if elapsed > time.Second {
		t.Fatalf("returned in %s, want immediately", elapsed)
	}
}

func TestRunStoreReadinessThenCollab(t *testing.T) {
	quietLog(t)

	opts := storeWaitOpts{
		perAttempt:     time.Second,
		initialBackoff: time.Second,
		multiplier:     2,
		maxBackoff:     time.Second,
		sleep: func(ctx context.Context, d time.Duration) error {
			t.Fatalf("unexpected sleep %s", d)
			return nil
		},
		now: time.Now,
	}

	t.Run("success once", func(t *testing.T) {
		var ready int
		runStoreReadinessThenCollab(context.Background(), func(ctx context.Context) error {
			return waitForStoreReady(ctx, func(ctx context.Context) error { return nil }, opts)
		}, func() { ready++ })
		if ready != 1 {
			t.Fatalf("onReady calls = %d, want 1", ready)
		}
	})

	t.Run("cancel skips", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var ready int
		var probes int
		runStoreReadinessThenCollab(ctx, func(ctx context.Context) error {
			return waitForStoreReady(ctx, func(ctx context.Context) error {
				probes++
				return nil
			}, opts)
		}, func() { ready++ })
		if ready != 0 || probes != 0 {
			t.Fatalf("onReady=%d probes=%d, want 0 and 0", ready, probes)
		}
	})
}
