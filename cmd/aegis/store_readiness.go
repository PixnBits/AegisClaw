package main

import (
	"context"
	"errors"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	storeProbePerAttempt     = 45 * time.Second
	storeProbeInitialBackoff = 2 * time.Second
	storeProbeBackoffFactor  = 2
	storeProbeMaxBackoff     = 30 * time.Second
)

// storeWaitOpts configures waitForStoreReady. Zero values select the
// production defaults: 45s per attempt, 2s initial backoff, 2x, 30s cap.
type storeWaitOpts struct {
	perAttempt     time.Duration
	initialBackoff time.Duration
	multiplier     float64
	maxBackoff     time.Duration
	// sleep waits for d, or returns ctx.Err() when ctx is cancelled.
	sleep func(ctx context.Context, d time.Duration) error
	now   func() time.Time
}

func (o storeWaitOpts) withDefaults() storeWaitOpts {
	if o.perAttempt <= 0 {
		o.perAttempt = storeProbePerAttempt
	}
	if o.initialBackoff <= 0 {
		o.initialBackoff = storeProbeInitialBackoff
	}
	if o.multiplier == 0 {
		o.multiplier = storeProbeBackoffFactor
	}
	if o.maxBackoff <= 0 {
		o.maxBackoff = storeProbeMaxBackoff
	}
	if o.sleep == nil {
		o.sleep = sleepUntil
	}
	if o.now == nil {
		o.now = time.Now
	}
	return o
}

// sleepUntil waits for d unless ctx is already done or finishes first.
func sleepUntil(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func growBackoff(current, max time.Duration, multiplier float64) time.Duration {
	if multiplier <= 1 {
		if current > max {
			return max
		}
		return current
	}
	if current >= max {
		return max
	}
	grown := time.Duration(float64(current) * multiplier)
	if grown <= current || grown > max {
		return max
	}
	return grown
}

// waitForStoreReady probes until success or ctx cancellation. It never gives
// up on a per-attempt timeout: that case is logged and retried with backoff.
func waitForStoreReady(ctx context.Context, probe func(context.Context) error, opts storeWaitOpts) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	opts = opts.withDefaults()
	started := opts.now()
	backoff := opts.initialBackoff
	if backoff > opts.maxBackoff {
		backoff = opts.maxBackoff
	}
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt++
		attemptCtx, cancel := context.WithTimeout(ctx, opts.perAttempt)
		err := probe(attemptCtx)
		cancel()
		if err == nil {
			logrus.Infof("Store channel.list ready (attempts %d, elapsed %s)", attempt, opts.now().Sub(started))
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		elapsed := opts.now().Sub(started)
		logrus.Warnf("Store channel.list attempt %d failed (elapsed %s, next backoff %s): %v", attempt, elapsed, backoff, err)
		if sleepErr := opts.sleep(ctx, backoff); sleepErr != nil {
			return sleepErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		backoff = growBackoff(backoff, opts.maxBackoff, opts.multiplier)
	}
}

// probeStoreChannelList is one production attempt. ctx carries the per-attempt
// deadline; sendToComponentViaHubRetry keeps the fast destination-not-found
// loop inside that budget. Parent cancellation is reported when this attempt
// returns (the retry helper has no context of its own).
func probeStoreChannelList(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wait := storeProbePerAttempt
	if dl, ok := ctx.Deadline(); ok {
		rem := time.Until(dl)
		if rem <= 0 {
			return ctx.Err()
		}
		wait = rem
	}
	_, err := sendToComponentViaHubRetry("store", "channel.list", nil, wait)
	if cerr := ctx.Err(); errors.Is(cerr, context.Canceled) {
		return cerr
	}
	return err
}

// runStoreReadinessThenCollab starts channels and Court only after wait
// succeeds. Cancellation logs at Info and does not call onReady.
func runStoreReadinessThenCollab(ctx context.Context, wait func(context.Context) error, onReady func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := wait(ctx); err != nil {
		logrus.Infof("store readiness wait cancelled; not starting channels or Court: %v", err)
		return
	}
	if err := ctx.Err(); err != nil {
		logrus.Infof("store readiness wait cancelled; not starting channels or Court: %v", err)
		return
	}
	if onReady != nil {
		onReady()
	}
}
