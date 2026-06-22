package project

import (
	"context"
	"os/exec"
	"sync"

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

	// delegationSpawned is true when this instance was auto-started by the
	// delegation router (warm reuse) rather than activated by the user. Used by
	// the Projects panel to distinguish user-focused vs delegation-spawned
	// instances. Guarded by mu.
	delegationSpawned bool
}

// acquireDelegationSlot reserves a delegated-session slot, enforcing the
// per-instance concurrency cap. max <= 0 means unlimited. It returns false when
// the cap is already reached, in which case no slot is taken.
func (i *Instance) acquireDelegationSlot(max int) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if max > 0 && i.inflight >= max {
		return false
	}
	i.inflight++
	return true
}

// releaseDelegationSlot returns a slot acquired by acquireDelegationSlot.
func (i *Instance) releaseDelegationSlot() {
	i.mu.Lock()
	if i.inflight > 0 {
		i.inflight--
	}
	i.mu.Unlock()
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
