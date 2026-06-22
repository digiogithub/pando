package project

import (
	"context"
	"os/exec"
	"sync"
	"time"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// Instance represents a running (or stopped) child Pando ACP process
// for a registered project directory.
type Instance struct {
	Project   Project
	cmd       *exec.Cmd
	conn      *acpsdk.ClientSideConnection
	delClient *delegationClient // capturing ACP client backing conn (for delegation)
	cancel    context.CancelFunc
	mu        sync.RWMutex
	sessions  []sessionEntry // cached from last session/list call
	ready     chan struct{}  // closed after ACP handshake succeeds
	errCh     chan error     // receives process exit errors

	initOnce sync.Once // guards the one-time ACP Initialize handshake
	initErr  error     // result of the Initialize handshake

	inflight int // in-flight delegated sessions (warm reuse); guarded by mu

	// cond signals waiters queued for a delegated slot (item A3). Lazily created
	// under mu on first queueing attempt; broadcast on every slot release so a
	// freed slot is picked up by a queued delegation. Guarded by mu.
	cond *sync.Cond
	// waiters counts delegations currently blocked in the bounded FIFO queue
	// waiting for a slot. Bounds the queue to WarmQueueDepth. Guarded by mu.
	waiters int

	// delegationSpawned is true when this instance was auto-started by the
	// delegation router (warm reuse) rather than activated by the user. Used by
	// the Projects panel to distinguish user-focused vs delegation-spawned
	// instances. Guarded by mu.
	delegationSpawned bool

	// lastActiveAt records the last time a delegated slot was acquired or
	// released (or the spawn time). The idle auto-GC (C1) measures idleness as
	// time-since-lastActiveAt while inflight is zero. Guarded by mu.
	lastActiveAt time.Time

	// closing is set once the idle auto-GC has claimed this instance for
	// teardown. It prevents a new delegated slot from being acquired on an
	// instance that is about to be stopped (closes the GC vs acquire race).
	// Guarded by mu.
	closing bool
}

// acquireDelegationSlot reserves a delegated-session slot, enforcing the
// per-instance concurrency cap. max <= 0 means unlimited. It returns false when
// the cap is already reached or when the instance is closing (claimed by the
// idle auto-GC), in which case no slot is taken and the caller falls back to the
// cold path.
func (i *Instance) acquireDelegationSlot(max int) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closing {
		return false
	}
	if max > 0 && i.inflight >= max {
		return false
	}
	i.inflight++
	i.lastActiveAt = time.Now()
	return true
}

// acquireDelegationSlotOrQueue reserves a delegated-session slot like
// acquireDelegationSlot, but when the cap is reached it optionally waits in a
// bounded FIFO queue (item A3) instead of returning false (cold fallback)
// immediately. It blocks until a slot is released, ctx is cancelled, or the
// instance begins closing — provided fewer than queueDepth callers are already
// queued. queueDepth <= 0 disables queueing (today's behaviour: cap reached =>
// cold fallback). Ordering is best-effort (a released slot wakes all waiters,
// which re-race) but the queue depth is strictly bounded. Returns false when the
// caller should fall back to the cold path (cap+queue full or disabled, closing,
// or ctx cancelled while queued).
func (i *Instance) acquireDelegationSlotOrQueue(ctx context.Context, max, queueDepth int) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cond == nil {
		i.cond = sync.NewCond(&i.mu)
	}

	// Immediate acquire when under the cap and not closing.
	if !i.closing && (max <= 0 || i.inflight < max) {
		i.inflight++
		i.lastActiveAt = time.Now()
		return true
	}

	// Cap reached (or closing). Fall back to the cold path unless queueing is
	// enabled and the bounded FIFO still has room.
	if i.closing || queueDepth <= 0 || i.waiters >= queueDepth {
		return false
	}

	// Enter the bounded FIFO queue and wait for a freed slot.
	i.waiters++
	defer func() { i.waiters-- }()

	// sync.Cond has no ctx-aware Wait; a watcher broadcasts on cancellation so the
	// wait loop can observe ctx.Err(). It exits as soon as the wait completes.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			i.mu.Lock()
			i.cond.Broadcast()
			i.mu.Unlock()
		case <-done:
		}
	}()

	for {
		if ctx.Err() != nil || i.closing {
			return false
		}
		if max <= 0 || i.inflight < max {
			i.inflight++
			i.lastActiveAt = time.Now()
			return true
		}
		i.cond.Wait()
	}
}

// releaseDelegationSlot returns a slot acquired by acquireDelegationSlot or
// acquireDelegationSlotOrQueue and wakes any delegation queued for a slot (A3).
func (i *Instance) releaseDelegationSlot() {
	i.mu.Lock()
	if i.inflight > 0 {
		i.inflight--
	}
	i.lastActiveAt = time.Now()
	if i.cond != nil {
		i.cond.Broadcast()
	}
	i.mu.Unlock()
}

// tryBeginClose atomically claims an idle instance for teardown by the idle
// auto-GC. It returns true (and sets closing) only when no delegated session is
// in flight and the instance was not already claimed, so a delegation that is
// acquiring a slot concurrently either wins the slot (GC sees inflight>0 and
// skips) or is refused by acquireDelegationSlot once closing is set.
func (i *Instance) tryBeginClose() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closing || i.inflight > 0 {
		return false
	}
	i.closing = true
	return true
}

// beginCloseAndWake marks the instance as closing and wakes any delegations
// queued for a slot (A3) so they immediately fall back to the cold path. Unlike
// tryBeginClose it does not require inflight==0 — it is used by an explicit Stop,
// which cancels the in-flight sessions through the process teardown separately.
func (i *Instance) beginCloseAndWake() {
	i.mu.Lock()
	i.closing = true
	if i.cond != nil {
		i.cond.Broadcast()
	}
	i.mu.Unlock()
}

// idleFor reports how long the instance has had no delegated-slot activity as of
// now. Combined with an inflight==0 check it identifies idle warm instances.
func (i *Instance) idleFor(now time.Time) time.Duration {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return now.Sub(i.lastActiveAt)
}

// InflightDelegations reports how many delegated sessions are currently running
// inside this instance. Used by the Projects panel to show "N delegated loops".
func (i *Instance) InflightDelegations() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.inflight
}

// markDelegationSpawned records whether this instance was started by the
// delegation router (true) or focused by the user (false).
func (i *Instance) markDelegationSpawned(v bool) {
	i.mu.Lock()
	i.delegationSpawned = v
	i.mu.Unlock()
}

// isDelegationSpawned reports whether this instance was auto-started by the
// delegation router rather than activated by the user.
func (i *Instance) isDelegationSpawned() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.delegationSpawned
}

// sessionEntry is a lightweight session descriptor fetched from the child.
type sessionEntry struct {
	ID        string
	Title     string
	UpdatedAt string
}
