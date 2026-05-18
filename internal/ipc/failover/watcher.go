// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package failover monitors primary liveness and promotes a secondary instance
// when the primary dies or shuts down gracefully.
package failover

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/logging"
)

// RoleChanged is emitted on RoleChangedC when the instance's role changes.
type RoleChanged struct {
	OldRole string // "primary" | "secondary"
	NewRole string
}

// Config holds tunable failover parameters.
type Config struct {
	// HeartbeatInterval controls how often the primary publishes its heartbeat.
	HeartbeatInterval time.Duration
	// HeartbeatTimeout is the maximum silence before a secondary declares the primary dead.
	HeartbeatTimeout time.Duration
	// Enabled is the feature flag; false by default to ensure safe opt-in.
	Enabled bool
}

// DefaultConfig returns conservative defaults suitable for production use.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval: 5 * time.Second,
		HeartbeatTimeout:  15 * time.Second,
		Enabled:           false,
	}
}

// BusPublisher is the subset of ipc.Bus used by the Watcher for publishing.
type BusPublisher interface {
	Publish(topic string, payload any) error
}

// EventSubscriber is the subset of ipc.Client used by the Watcher for subscribing.
type EventSubscriber interface {
	SubscribeTo(pubEndpoint string, topics ...string) (<-chan ipc.Envelope, error)
}

// Watcher monitors primary liveness and coordinates role transitions.
//   - On the primary instance: publishes periodic heartbeats and instance.shutdown on exit.
//   - On secondary instances: watches for heartbeats and triggers promotion when none arrive.
type Watcher struct {
	cfg        Config
	role       string
	instanceID string
	workdir    string
	pubPort    int
	rpcPort    int

	// bus is non-nil on primary only.
	bus BusPublisher
	// client is non-nil on secondary only.
	client EventSubscriber
	// pubEndpoint is the primary PUB address the secondary subscribes to.
	pubEndpoint string

	mu            sync.RWMutex
	lastHeartbeat time.Time

	// onPromote is called when this secondary wins the promotion race.
	// Kept as a stub in Phase 5a; Phase 5b will implement full DB reconnect.
	onPromote func(ctx context.Context) error
	// onDemote is called when this instance loses primary status (future use).
	onDemote func(ctx context.Context) error

	// RoleChangedC receives non-blocking notifications on every role transition.
	RoleChangedC chan RoleChanged

	cancel context.CancelFunc
	done   chan struct{}
}

// NewWatcher creates a Watcher. Exactly one of bus or client must be non-nil:
// primary instances pass bus, secondary instances pass client with the primary's pubEndpoint.
func NewWatcher(
	cfg Config,
	role, instanceID, workdir string,
	pubPort, rpcPort int,
	bus BusPublisher,
	client EventSubscriber,
	pubEndpoint string,
	onPromote, onDemote func(ctx context.Context) error,
) *Watcher {
	return &Watcher{
		cfg:          cfg,
		role:         role,
		instanceID:   instanceID,
		workdir:      workdir,
		pubPort:      pubPort,
		rpcPort:      rpcPort,
		bus:          bus,
		client:       client,
		pubEndpoint:  pubEndpoint,
		onPromote:    onPromote,
		onDemote:     onDemote,
		RoleChangedC: make(chan RoleChanged, 4),
		done:         make(chan struct{}),
	}
}

// SetEnabled enables or disables automatic failover at runtime. Safe for concurrent use.
func (w *Watcher) SetEnabled(enabled bool) {
	w.mu.Lock()
	w.cfg.Enabled = enabled
	w.mu.Unlock()
}

// Start begins monitoring in the background. For the primary it publishes heartbeats;
// for secondaries it watches for them and triggers promotion on absence.
func (w *Watcher) Start(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)
	if w.role == "primary" {
		go w.runPrimary(ctx)
	} else {
		go w.runSecondary(ctx)
	}
}

// Shutdown stops the watcher. On primary instances it first publishes instance.shutdown.
// Safe to call even if Start was never called.
func (w *Watcher) Shutdown(ctx context.Context) {
	if w.role == "primary" && w.bus != nil {
		payload := protocol.ShutdownPayload{
			InstanceID: w.instanceID,
			Reason:     "graceful shutdown",
		}
		if err := w.bus.Publish(protocol.TopicInstanceShutdown, payload); err != nil {
			logging.Warn("failover: failed to publish shutdown event", "error", err)
		}
	}
	// If Start was never called, cancel is nil and done is never closed — nothing to stop.
	if w.cancel == nil {
		return
	}
	w.cancel()
	<-w.done
}

// runPrimary publishes a heartbeat every HeartbeatInterval until ctx is cancelled.
func (w *Watcher) runPrimary(ctx context.Context) {
	defer close(w.done)

	ticker := time.NewTicker(w.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			payload := protocol.HeartbeatPayload{
				InstanceID: w.instanceID,
				StartedAt:  time.Now(),
			}
			if err := w.bus.Publish(protocol.TopicInstanceHeartbeat, payload); err != nil {
				logging.Warn("failover: primary failed to publish heartbeat", "error", err)
			}
		}
	}
}

// runSecondary subscribes to the primary's PUB socket and watches for heartbeats.
// It also listens for instance.shutdown so it can react immediately instead of waiting.
func (w *Watcher) runSecondary(ctx context.Context) {
	defer close(w.done)

	if w.client == nil || w.pubEndpoint == "" {
		logging.Warn("failover: secondary watcher has no IPC client or pubEndpoint; monitoring disabled")
		return
	}

	ch, err := w.client.SubscribeTo(w.pubEndpoint,
		protocol.TopicInstanceHeartbeat,
		protocol.TopicInstanceShutdown,
	)
	if err != nil {
		logging.Warn("failover: secondary failed to subscribe to primary events", "error", err)
		return
	}

	w.mu.Lock()
	w.lastHeartbeat = time.Now()
	w.mu.Unlock()

	timeout := time.NewTimer(w.cfg.HeartbeatTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case env, ok := <-ch:
			if !ok {
				// Subscription channel closed — primary socket gone.
				logging.Warn("failover: heartbeat subscription channel closed; primary may be dead")
				w.triggerFailover(ctx)
				return
			}
			switch env.Topic {
			case protocol.TopicInstanceHeartbeat:
				w.mu.Lock()
				w.lastHeartbeat = time.Now()
				w.mu.Unlock()
				// Reset the timer on each received heartbeat.
				if !timeout.Stop() {
					select {
					case <-timeout.C:
					default:
					}
				}
				timeout.Reset(w.cfg.HeartbeatTimeout)

			case protocol.TopicInstanceShutdown:
				logging.Info("failover: received graceful shutdown from primary; starting failover immediately")
				w.triggerFailover(ctx)
				return
			}

		case <-timeout.C:
			w.mu.Lock()
			last := w.lastHeartbeat
			w.mu.Unlock()
			logging.Warn("failover: no heartbeat received within timeout",
				"timeout", w.cfg.HeartbeatTimeout,
				"last_heartbeat", last,
			)
			w.triggerFailover(ctx)
			return
		}
	}
}

// triggerFailover is called on the secondary when the primary appears dead.
// It checks the feature flag, attempts lock acquisition, and calls onPromote on success.
func (w *Watcher) triggerFailover(ctx context.Context) {
	w.mu.RLock()
	enabled := w.cfg.Enabled
	w.mu.RUnlock()

	if !enabled {
		logging.Warn("failover: primary appears dead but auto-failover is disabled (enable with --auto-failover)")
		return
	}

	logging.Info("failover: attempting to promote to primary",
		"instance_id", w.instanceID,
		"workdir", w.workdir,
	)

	isPrimary, _, lockFile, err := ipc.AcquireLock(w.workdir, w.instanceID, w.pubPort, w.rpcPort)
	if err != nil {
		logging.Warn("failover: lock acquisition error; aborting promotion", "error", err)
		return
	}

	if !isPrimary {
		// Another secondary acquired the lock first.
		logging.Info("failover: another secondary promoted first; staying secondary")
		return
	}

	// We hold the lock — release it back to the bootstrap sequence so onPromote
	// can re-open the RW DB and start the Bus properly. The stub below logs and
	// returns without actually setting up services; Phase 5b implements the full flow.
	ipc.ReleaseLock(lockFile)

	logging.Info("failover: acquired lock; invoking promotion callback",
		"instance_id", w.instanceID,
	)

	var promoteErr error
	if w.onPromote != nil {
		promoteErr = w.onPromote(ctx)
	}

	if promoteErr != nil {
		logging.Warn("failover: promotion callback returned error", "error", promoteErr)
		return
	}

	w.mu.Lock()
	oldRole := w.role
	w.role = "primary"
	w.mu.Unlock()

	w.notify(RoleChanged{OldRole: oldRole, NewRole: "primary"})
	logging.Info("failover: promotion complete", "instance_id", w.instanceID)
}

// notify sends a RoleChanged event on RoleChangedC without blocking.
func (w *Watcher) notify(rc RoleChanged) {
	select {
	case w.RoleChangedC <- rc:
	default:
		// Drop if nobody is reading; callers must drain the channel.
		logging.Warn("failover: RoleChangedC full; event dropped",
			"old_role", rc.OldRole,
			"new_role", rc.NewRole,
		)
	}
}

// LastHeartbeat returns the time of the most recent heartbeat seen by this secondary.
// Returns zero time on primary instances.
func (w *Watcher) LastHeartbeat() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastHeartbeat
}

// defaultPromoteStub is used when onPromote is nil. It logs a message and returns nil
// so that the watcher infrastructure can be exercised without a real DB reconnect.
func defaultPromoteStub(instanceID string) func(ctx context.Context) error {
	return func(_ context.Context) error {
		logging.Warn("failover: PROMOTE stub called — full DB reconnect not yet implemented",
			"instance_id", instanceID,
		)
		return nil
	}
}

// NewWatcherForPrimary is a convenience constructor that wires a primary Watcher
// with the Bus and no client. onPromote/onDemote are unused for the primary role.
func NewWatcherForPrimary(
	cfg Config,
	instanceID, workdir string,
	pubPort, rpcPort int,
	bus BusPublisher,
) *Watcher {
	return NewWatcher(cfg, "primary", instanceID, workdir, pubPort, rpcPort,
		bus, nil, "",
		nil, nil,
	)
}

// NewWatcherForSecondary is a convenience constructor that wires a secondary Watcher
// with the IPC client subscribed to the primary's PUB endpoint.
func NewWatcherForSecondary(
	cfg Config,
	instanceID, workdir string,
	pubPort, rpcPort int,
	client EventSubscriber,
	pubEndpoint string,
	onPromote func(ctx context.Context) error,
) *Watcher {
	if onPromote == nil {
		onPromote = defaultPromoteStub(instanceID)
	}
	return NewWatcher(cfg, "secondary", instanceID, workdir, pubPort, rpcPort,
		nil, client, pubEndpoint,
		onPromote, nil,
	)
}

// FormatStatus returns a human-readable description of the watcher state for diagnostics.
func (w *Watcher) FormatStatus() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	enabled := w.cfg.Enabled
	if w.role == "primary" {
		return fmt.Sprintf("role=primary enabled=%v heartbeat_interval=%s", enabled, w.cfg.HeartbeatInterval)
	}
	return fmt.Sprintf("role=secondary enabled=%v heartbeat_timeout=%s last_heartbeat=%s",
		enabled, w.cfg.HeartbeatTimeout, w.lastHeartbeat.Format(time.RFC3339))
}
