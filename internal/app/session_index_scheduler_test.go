// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond every 2ms until it returns true or timeout elapses,
// failing the test on timeout. Kept short (bounded by the caller's timeout)
// to avoid flaky sleeps while still not depending on exact scheduling.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// runRecorder is a thread-safe recorder of sessionIndexScheduler.indexFn
// invocations, used by every scheduler test below.
type runRecorder struct {
	mu      sync.Mutex
	starts  []time.Time
	fn      func(ctx context.Context, sessionID string) error
	running int32 // guarded by atomic ops; used to assert non-overlap
	overlap bool
}

func newRunRecorder() *runRecorder {
	return &runRecorder{}
}

func (r *runRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.starts)
}

// indexFn is the function under test passes to newSessionIndexScheduler.
func (r *runRecorder) indexFn(ctx context.Context, sessionID string) error {
	if atomic.AddInt32(&r.running, 1) > 1 {
		r.mu.Lock()
		r.overlap = true
		r.mu.Unlock()
	}
	defer atomic.AddInt32(&r.running, -1)

	r.mu.Lock()
	r.starts = append(r.starts, time.Now())
	fn := r.fn
	r.mu.Unlock()

	if fn != nil {
		return fn(ctx, sessionID)
	}
	return nil
}

func TestSessionIndexSchedulerDebouncesBurstsIntoOneRun(t *testing.T) {
	rec := newRunRecorder()
	sched := newSessionIndexScheduler(rec.indexFn, 20*time.Millisecond, time.Hour)
	ctx := context.Background()

	// A burst of notifications well within the debounce window must collapse
	// into exactly one run.
	for i := 0; i < 5; i++ {
		sched.notify(ctx, "sess-1")
		time.Sleep(3 * time.Millisecond)
	}

	waitFor(t, time.Second, func() bool { return rec.count() >= 1 })
	time.Sleep(60 * time.Millisecond) // let any (unwanted) extra run happen
	if got := rec.count(); got != 1 {
		t.Fatalf("expected exactly 1 run for a debounced burst, got %d", got)
	}
}

func TestSessionIndexSchedulerMinIntervalCapsRunRate(t *testing.T) {
	rec := newRunRecorder()
	debounce := 5 * time.Millisecond
	minInterval := 100 * time.Millisecond
	sched := newSessionIndexScheduler(rec.indexFn, debounce, minInterval)
	ctx := context.Background()

	start := time.Now()
	sched.notify(ctx, "sess-1")
	waitFor(t, time.Second, func() bool { return rec.count() >= 1 })

	// Immediately request more runs; none of them may start before
	// minInterval has elapsed since the first run.
	sched.notify(ctx, "sess-1")

	waitFor(t, time.Second, func() bool { return rec.count() >= 2 })
	rec.mu.Lock()
	elapsed := rec.starts[1].Sub(start)
	rec.mu.Unlock()
	if elapsed < minInterval {
		t.Fatalf("second run started only %s after the first, want >= minInterval (%s)", elapsed, minInterval)
	}
}

func TestSessionIndexSchedulerTrailingRunGuaranteed(t *testing.T) {
	rec := newRunRecorder()
	// A long-running indexFn so a notify() arriving mid-run is forced onto
	// the "dirty" (trailing-run) path rather than the normal debounce path.
	block := make(chan struct{})
	rec.fn = func(ctx context.Context, sessionID string) error {
		<-block
		return nil
	}
	sched := newSessionIndexScheduler(rec.indexFn, time.Millisecond, 10*time.Millisecond)
	ctx := context.Background()

	sched.notify(ctx, "sess-1")
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&rec.running) == 1 })

	// Arrives while the first run is in flight — must not be dropped.
	sched.notify(ctx, "sess-1")
	close(block) // let the first run finish

	waitFor(t, time.Second, func() bool { return rec.count() >= 2 })

	rec.mu.Lock()
	overlap := rec.overlap
	rec.mu.Unlock()
	if overlap {
		t.Fatal("two runs for the same session overlapped; expected the trailing run to wait for the first to finish")
	}
}

func TestSessionIndexSchedulerNeverOverlapsRunsUnderConcurrentNotify(t *testing.T) {
	rec := newRunRecorder()
	rec.fn = func(ctx context.Context, sessionID string) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	}
	sched := newSessionIndexScheduler(rec.indexFn, time.Millisecond, 2*time.Millisecond)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sched.notify(ctx, "sess-1")
			time.Sleep(time.Millisecond)
		}()
	}
	wg.Wait()

	waitFor(t, 2*time.Second, func() bool { return rec.count() >= 1 })
	time.Sleep(100 * time.Millisecond) // let any trailing runs settle

	rec.mu.Lock()
	overlap := rec.overlap
	rec.mu.Unlock()
	if overlap {
		t.Fatal("concurrent notify() calls caused overlapping runs")
	}
}

func TestSessionIndexSchedulerIndependentSessionsRunIndependently(t *testing.T) {
	rec := newRunRecorder()
	sched := newSessionIndexScheduler(rec.indexFn, 5*time.Millisecond, time.Hour)
	ctx := context.Background()

	sched.notify(ctx, "sess-1")
	sched.notify(ctx, "sess-2")

	waitFor(t, time.Second, func() bool { return rec.count() >= 2 })
}

func TestSessionIndexSchedulerStopAllStopsPendingTimers(t *testing.T) {
	rec := newRunRecorder()
	sched := newSessionIndexScheduler(rec.indexFn, 30*time.Millisecond, time.Hour)
	ctx := context.Background()

	sched.notify(ctx, "sess-1")
	sched.stopAll()
	time.Sleep(60 * time.Millisecond)

	if got := rec.count(); got != 0 {
		t.Fatalf("expected stopAll to prevent the pending run, got %d runs", got)
	}
}
