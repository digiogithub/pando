package project

import (
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
)

// gcMinInterval bounds how frequently the idle auto-GC scans, regardless of how
// short the configured idle timeout is, so a tiny timeout cannot spin the loop.
const gcMinInterval = 30 * time.Second

// StartIdleGC launches a background janitor that stops warm instances which were
// auto-started by the delegation router (delegationSpawned), have no in-flight
// delegated sessions, are not the user's active project, and have been idle for
// at least idleTimeout. User-activated instances are never touched. A
// non-positive idleTimeout disables the janitor (today's behaviour: warm
// instances persist until stopped from the Projects panel).
//
// The janitor runs for the lifetime of the manager (m.ctx) and is a no-op when
// already running for this manager is not tracked — callers should invoke it
// once after construction.
func (m *Manager) StartIdleGC(idleTimeout time.Duration) {
	if idleTimeout <= 0 {
		return
	}
	interval := idleTimeout / 2
	if interval < gcMinInterval {
		interval = gcMinInterval
	}
	if interval > idleTimeout {
		interval = idleTimeout
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.gcIdleInstances(idleTimeout)
			}
		}
	}()
}

// gcIdleInstances performs one sweep: it claims every idle delegation-spawned
// instance (atomically, so a concurrent delegation either keeps the instance or
// is refused a slot) and tears it down. It is exported-for-test via the package
// and safe to call directly. Returns the project ids that were stopped.
func (m *Manager) gcIdleInstances(idleTimeout time.Duration) []string {
	now := time.Now()

	var stopped []*Instance
	var ids []string

	m.mu.Lock()
	for id, inst := range m.instances {
		if inst == nil || id == m.activeID {
			continue
		}
		if !inst.isDelegationSpawned() {
			continue
		}
		if inst.idleFor(now) < idleTimeout {
			continue
		}
		// tryBeginClose also rejects instances with in-flight delegations, so a
		// long-running delegated session keeps its warm instance alive even if
		// lastActiveAt is old.
		if !inst.tryBeginClose() {
			continue
		}
		delete(m.instances, id)
		stopped = append(stopped, inst)
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for k, inst := range stopped {
		m.teardownIdleInstance(ids[k], inst)
	}
	return ids
}

// teardownIdleInstance stops a single instance already removed from the live map
// and claimed via tryBeginClose. The spawnChild monitor goroutine records the
// persistent stopped status and publishes EvStatusChanged on exit.
func (m *Manager) teardownIdleInstance(projectID string, inst *Instance) {
	inst.cancel()
	if inst.cmd != nil && inst.cmd.Process != nil {
		_ = inst.cmd.Process.Signal(syscall.SIGTERM)
	}

	select {
	case <-inst.errCh:
	case <-time.After(5 * time.Second):
		if inst.cmd != nil && inst.cmd.Process != nil {
			_ = inst.cmd.Process.Kill()
		}
	}

	logging.Debug("project manager: idle auto-GC stopped delegation-spawned instance", "project", projectID)

	// Announce the delegation-count change so the panel drops the row's badges.
	m.publishDelegationChanged(projectID, 0)
	if m.broker != nil {
		m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
			Type:      EvStatusChanged,
			ProjectID: projectID,
			Status:    StatusStopped,
		})
	}
}
