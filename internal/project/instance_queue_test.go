package project

import (
	"context"
	"testing"
	"time"
)

// waitForWaiters blocks until the instance reports the expected number of
// delegations queued for a slot, failing the test on timeout.
func waitForWaiters(t *testing.T, inst *Instance, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		inst.mu.Lock()
		got := inst.waiters
		inst.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiters = %d, want %d", got, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestAcquireSlotOrQueueImmediate verifies an under-cap acquisition takes a slot
// without queueing.
func TestAcquireSlotOrQueueImmediate(t *testing.T) {
	inst := &Instance{}
	if !inst.acquireDelegationSlotOrQueue(context.Background(), 2, 0) {
		t.Fatal("under-cap acquire should succeed")
	}
	if got := inst.InflightDelegations(); got != 1 {
		t.Fatalf("inflight = %d, want 1", got)
	}
}

// TestAcquireSlotOrQueueColdFallbackWhenQueueDisabled verifies that with
// queueDepth=0 an over-cap acquisition falls back immediately (today's behaviour).
func TestAcquireSlotOrQueueColdFallbackWhenQueueDisabled(t *testing.T) {
	inst := &Instance{}
	if !inst.acquireDelegationSlotOrQueue(context.Background(), 1, 0) {
		t.Fatal("first acquire should succeed")
	}
	if inst.acquireDelegationSlotOrQueue(context.Background(), 1, 0) {
		t.Fatal("over-cap acquire with queueDepth=0 should fall back (false)")
	}
}

// TestAcquireSlotOrQueueQueuesAndProceeds verifies a queued delegation blocks
// (instead of cold-falling-back) while the cap is full, that the bounded FIFO is
// respected (a third over cap+queueDepth still falls back), and that a freed slot
// is handed to the queued waiter (item A3).
func TestAcquireSlotOrQueueQueuesAndProceeds(t *testing.T) {
	inst := &Instance{}
	if !inst.acquireDelegationSlotOrQueue(context.Background(), 1, 1) {
		t.Fatal("first acquire should succeed")
	}

	acquired := make(chan bool, 1)
	go func() {
		acquired <- inst.acquireDelegationSlotOrQueue(context.Background(), 1, 1)
	}()

	// The second delegation must queue (block) rather than return.
	waitForWaiters(t, inst, 1)
	select {
	case <-acquired:
		t.Fatal("queued waiter returned before a slot was freed")
	case <-time.After(50 * time.Millisecond):
	}

	// A third delegation over cap+queueDepth must be refused immediately.
	if inst.acquireDelegationSlotOrQueue(context.Background(), 1, 1) {
		t.Fatal("acquire over cap+queueDepth should fall back (false)")
	}

	// Free the only slot; the queued waiter should pick it up.
	inst.releaseDelegationSlot()
	select {
	case ok := <-acquired:
		if !ok {
			t.Fatal("queued waiter returned false after a slot was freed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued waiter never acquired the freed slot")
	}
	if got := inst.InflightDelegations(); got != 1 {
		t.Fatalf("inflight = %d after handoff, want 1", got)
	}
}

// TestAcquireSlotOrQueueCtxCancel verifies a queued delegation whose context is
// cancelled unblocks and falls back to the cold path without taking a slot.
func TestAcquireSlotOrQueueCtxCancel(t *testing.T) {
	inst := &Instance{}
	if !inst.acquireDelegationSlotOrQueue(context.Background(), 1, 1) {
		t.Fatal("first acquire should succeed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	acquired := make(chan bool, 1)
	go func() { acquired <- inst.acquireDelegationSlotOrQueue(ctx, 1, 1) }()

	waitForWaiters(t, inst, 1)
	cancel()
	select {
	case ok := <-acquired:
		if ok {
			t.Fatal("queued waiter should return false on ctx cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued waiter did not unblock on ctx cancel")
	}
	if got := inst.InflightDelegations(); got != 1 {
		t.Fatalf("inflight = %d, want 1 (original slot still held)", got)
	}
}

// TestAcquireSlotOrQueueClosingWakes verifies that beginCloseAndWake (used by an
// explicit Stop) unblocks a queued delegation so it falls back to the cold path.
func TestAcquireSlotOrQueueClosingWakes(t *testing.T) {
	inst := &Instance{}
	if !inst.acquireDelegationSlotOrQueue(context.Background(), 1, 1) {
		t.Fatal("first acquire should succeed")
	}

	acquired := make(chan bool, 1)
	go func() { acquired <- inst.acquireDelegationSlotOrQueue(context.Background(), 1, 1) }()

	waitForWaiters(t, inst, 1)
	inst.beginCloseAndWake()
	select {
	case ok := <-acquired:
		if ok {
			t.Fatal("queued waiter should return false once the instance is closing")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued waiter did not unblock on close")
	}
}
