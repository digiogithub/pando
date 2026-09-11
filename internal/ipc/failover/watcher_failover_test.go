// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package failover_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/failover"
	"github.com/digiogithub/pando/internal/ipc/protocol"
)

// fakeSubscriber hands the watcher a channel the test controls, standing in
// for the primary's PUB socket.
type fakeSubscriber struct{ ch chan ipc.Envelope }

func newFakeSubscriber() *fakeSubscriber { return &fakeSubscriber{ch: make(chan ipc.Envelope, 16)} }

func (f *fakeSubscriber) SubscribeTo(string, ...string) (<-chan ipc.Envelope, error) {
	return f.ch, nil
}

func (f *fakeSubscriber) sendShutdown(t *testing.T, instanceID string) {
	t.Helper()
	raw, err := json.Marshal(protocol.ShutdownPayload{InstanceID: instanceID, Reason: "graceful shutdown"})
	if err != nil {
		t.Fatal(err)
	}
	f.ch <- ipc.Envelope{InstanceID: instanceID, Topic: protocol.TopicInstanceShutdown, Payload: raw}
}

// recordingPublisher records published topics.
type recordingPublisher struct {
	mu     sync.Mutex
	topics []string
}

func (r *recordingPublisher) Publish(topic string, _ any) error {
	r.mu.Lock()
	r.topics = append(r.topics, topic)
	r.mu.Unlock()
	return nil
}

func (r *recordingPublisher) count(topic string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, tp := range r.topics {
		if tp == topic {
			n++
		}
	}
	return n
}

// fastConfig makes heartbeat timeouts and retries fire within milliseconds.
func fastConfig() failover.Config {
	return failover.Config{
		HeartbeatInterval:       20 * time.Millisecond,
		HeartbeatTimeout:        80 * time.Millisecond,
		ProbeInterval:           time.Hour,
		Enabled:                 true,
		RetryBackoffMin:         40 * time.Millisecond,
		RetryBackoffMax:         80 * time.Millisecond,
		ShutdownAcquireWindow:   3 * time.Second,
		ShutdownAcquireInterval: 20 * time.Millisecond,
	}
}

// promoteRecorder is a PromoteFunc that counts calls, can fail the first
// failures calls, and keeps the lock file it was handed so the test can
// release it.
type promoteRecorder struct {
	calls    atomic.Int32
	failures int32
	mu       sync.Mutex
	lock     *os.File
	onOK     func()
}

func (p *promoteRecorder) promote(_ context.Context, lockFile *os.File) error {
	n := p.calls.Add(1)
	if n <= p.failures {
		return errors.New("simulated promotion failure")
	}
	p.mu.Lock()
	p.lock = lockFile
	p.mu.Unlock()
	if p.onOK != nil {
		p.onOK()
	}
	return nil
}

func (p *promoteRecorder) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	ipc.ReleaseLock(p.lock)
	p.lock = nil
}

// startWatcher starts a secondary watcher and stops it at test end.
func startWatcher(t *testing.T, cfg failover.Config, dir string, sub *fakeSubscriber, fn failover.PromoteFunc) *failover.Watcher {
	t.Helper()
	w := failover.NewWatcherForSecondary(cfg, "secondary-under-test", dir, 45000, 45001, sub, "tcp://127.0.0.1:1", fn)
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)
	t.Cleanup(func() {
		cancel()
		w.Shutdown(context.Background())
	})
	return w
}

// lockIsFree reports whether the test can take (and immediately drop) the lock.
func lockIsFree(t *testing.T, dir string) bool {
	t.Helper()
	isPrimary, _, f, err := ipc.AcquireLock(dir, "observer", 45000, 45001)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if isPrimary {
		ipc.ReleaseLock(f)
	}
	return isPrimary
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

// TestWatcherWithoutPromoteCallbackNeverTakesLock guards G3: a secondary with no
// promotion callback (serve/desktop/app today) must never grab the IPC lock,
// or it becomes a zombie primary that the next instance SIGKILLs.
func TestWatcherWithoutPromoteCallbackNeverTakesLock(t *testing.T) {
	dir := t.TempDir()
	sub := newFakeSubscriber()
	w := startWatcher(t, fastConfig(), dir, sub, nil)

	// Several heartbeat timeouts and a graceful-shutdown announcement.
	sub.sendShutdown(t, "dead-primary")
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !lockIsFree(t, dir) {
			t.Fatal("watcher without a promotion callback took the IPC lock")
		}
		time.Sleep(15 * time.Millisecond)
	}
	if got := w.Role(); got != "secondary" {
		t.Fatalf("role = %q, want secondary", got)
	}
}

// TestWatcherKeepsMonitoringAfterLostRace guards G4: losing the lock race must
// not end monitoring; once the lock frees up later, the watcher promotes.
func TestWatcherKeepsMonitoringAfterLostRace(t *testing.T) {
	dir := t.TempDir()
	isPrimary, _, other, err := ipc.AcquireLock(dir, "other-primary", 45000, 45001)
	if err != nil || !isPrimary {
		t.Fatalf("test could not take the lock first: primary=%v err=%v", isPrimary, err)
	}

	rec := &promoteRecorder{}
	t.Cleanup(rec.release)
	w := startWatcher(t, fastConfig(), dir, newFakeSubscriber(), rec.promote)

	// Several timeouts elapse while the lock is held elsewhere: lost races.
	time.Sleep(400 * time.Millisecond)
	if n := rec.calls.Load(); n != 0 {
		t.Fatalf("promote called %d times while another instance held the lock", n)
	}
	if got := w.Role(); got != "secondary" {
		t.Fatalf("role = %q after lost races, want secondary", got)
	}

	ipc.ReleaseLock(other)
	waitFor(t, 3*time.Second, "promotion after the lock was released", func() bool {
		return w.Role() == "primary"
	})
	if n := rec.calls.Load(); n != 1 {
		t.Fatalf("promote called %d times, want 1", n)
	}
	if lockIsFree(t, dir) {
		t.Fatal("promoted watcher does not hold the lock")
	}
}

// TestWatcherRetriesAfterFailedPromote guards G4: a failed promotion releases
// the lock and the watcher tries again later instead of giving up.
func TestWatcherRetriesAfterFailedPromote(t *testing.T) {
	dir := t.TempDir()
	rec := &promoteRecorder{failures: 2}
	t.Cleanup(rec.release)
	w := startWatcher(t, fastConfig(), dir, newFakeSubscriber(), rec.promote)

	waitFor(t, 5*time.Second, "promotion after two failed attempts", func() bool {
		return w.Role() == "primary"
	})
	if n := rec.calls.Load(); n != 3 {
		t.Fatalf("promote called %d times, want 3 (2 failures + 1 success)", n)
	}
}

// TestWatcherKeepsMonitoringWhileDisabled guards G4: with failover disabled the
// watcher keeps watching, and promotes once it is re-enabled.
func TestWatcherKeepsMonitoringWhileDisabled(t *testing.T) {
	dir := t.TempDir()
	cfg := fastConfig()
	cfg.Enabled = false
	rec := &promoteRecorder{}
	t.Cleanup(rec.release)
	w := startWatcher(t, cfg, dir, newFakeSubscriber(), rec.promote)

	time.Sleep(300 * time.Millisecond)
	if rec.calls.Load() != 0 || w.Role() != "secondary" {
		t.Fatal("disabled watcher attempted a promotion")
	}
	w.SetEnabled(true)
	waitFor(t, 3*time.Second, "promotion after re-enabling", func() bool {
		return w.Role() == "primary"
	})
}

// TestWatcherRetriesLockAfterShutdownAnnouncement guards G5: the old primary
// announces instance.shutdown but still holds the lock for a moment; a single
// AcquireLock would lose and give up, the retry window must win once the lock
// is released.
func TestWatcherRetriesLockAfterShutdownAnnouncement(t *testing.T) {
	dir := t.TempDir()
	isPrimary, _, old, err := ipc.AcquireLock(dir, "old-primary", 45000, 45001)
	if err != nil || !isPrimary {
		t.Fatalf("test could not take the lock first: primary=%v err=%v", isPrimary, err)
	}

	cfg := fastConfig()
	// Only the shutdown path may explain a promotion within this test.
	cfg.HeartbeatTimeout = time.Hour
	rec := &promoteRecorder{}
	t.Cleanup(rec.release)
	sub := newFakeSubscriber()
	w := startWatcher(t, cfg, dir, sub, rec.promote)

	sub.sendShutdown(t, "old-primary")
	time.Sleep(300 * time.Millisecond)
	if w.Role() != "secondary" {
		t.Fatal("promoted while the old primary still held the lock")
	}
	ipc.ReleaseLock(old)

	waitFor(t, 3*time.Second, "promotion once the old primary released the lock", func() bool {
		return w.Role() == "primary"
	})
	if n := rec.calls.Load(); n != 1 {
		t.Fatalf("promote called %d times, want 1", n)
	}
}

// TestPromotedWatcherPublishesOnPrimaryBus verifies that after a promotion the
// watcher heartbeats on the bus registered by the promotion callback and
// announces instance.shutdown on it when stopped.
func TestPromotedWatcherPublishesOnPrimaryBus(t *testing.T) {
	dir := t.TempDir()
	pub := &recordingPublisher{}
	rec := &promoteRecorder{}
	t.Cleanup(rec.release)

	w := failover.NewWatcherForSecondary(fastConfig(), "secondary-under-test", dir, 45000, 45001,
		newFakeSubscriber(), "tcp://127.0.0.1:1", nil)
	rec.onOK = func() { w.SetPrimaryBus(pub) }
	w.SetPromoteCallback(rec.promote)
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)

	waitFor(t, 3*time.Second, "heartbeats on the promoted bus", func() bool {
		return pub.count(protocol.TopicInstanceHeartbeat) >= 2
	})
	cancel()
	w.Shutdown(context.Background())
	if pub.count(protocol.TopicInstanceShutdown) != 1 {
		t.Fatalf("instance.shutdown published %d times, want 1", pub.count(protocol.TopicInstanceShutdown))
	}
}
