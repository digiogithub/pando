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

	// delegationSpawned is true when this instance was auto-started by the
	// delegation router (warm reuse) rather than activated by the user. Used by
	// the Projects panel to distinguish user-focused vs delegation-spawned
	// instances. Guarded by mu.
	delegationSpawned bool

	slots delegationSlots
}

// acquireDelegationSlot reserves a delegated-session slot, enforcing the
// per-instance concurrency cap. max <= 0 means unlimited. It returns false when
// the cap is already reached or when the instance is closing (claimed by the
// idle auto-GC), in which case no slot is taken and the caller falls back to the
// cold path.
func (i *Instance) acquireDelegationSlot(max int) bool {
	return i.slots.acquire(max)
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
	return i.slots.acquireOrQueue(ctx, max, queueDepth)
}

// releaseDelegationSlot returns a slot acquired by acquireDelegationSlot or
// acquireDelegationSlotOrQueue and wakes any delegation queued for a slot (A3).
func (i *Instance) releaseDelegationSlot() {
	i.slots.release()
}

// tryBeginClose atomically claims an idle instance for teardown by the idle
// auto-GC. It returns true (and sets closing) only when no delegated session is
// in flight and the instance was not already claimed, so a delegation that is
// acquiring a slot concurrently either wins the slot (GC sees inflight>0 and
// skips) or is refused by acquireDelegationSlot once closing is set.
func (i *Instance) tryBeginClose() bool {
	return i.slots.tryBeginClose()
}

// beginCloseAndWake marks the instance as closing and wakes any delegations
// queued for a slot (A3) so they immediately fall back to the cold path. Unlike
// tryBeginClose it does not require inflight==0 — it is used by an explicit Stop,
// which cancels the in-flight sessions through the process teardown separately.
func (i *Instance) beginCloseAndWake() {
	i.slots.beginCloseAndWake()
}

// idleFor reports how long the instance has had no delegated-slot activity as of
// now. Combined with an inflight==0 check it identifies idle warm instances.
func (i *Instance) idleFor(now time.Time) time.Duration {
	return i.slots.idleFor(now)
}

// InflightDelegations reports how many delegated sessions are currently running
// inside this instance. Used by the Projects panel to show "N delegated loops".
func (i *Instance) InflightDelegations() int {
	return i.slots.inflightCount()
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
