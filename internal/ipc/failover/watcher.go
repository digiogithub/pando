// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package failover monitors primary liveness and promotes a secondary instance
// when the primary dies or shuts down gracefully.
package failover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
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
	// ProbeInterval is how often the secondary actively pings the primary via RPC
	// as a complementary liveness check independent of PUB/SUB heartbeats.
	ProbeInterval time.Duration
	// Enabled controls whether the secondary will attempt automatic promotion when
	// the primary appears dead. Defaults to true; set false to observe without acting.
	Enabled bool

	// RetryBackoffMin and RetryBackoffMax bound the (jittered, doubling) delay
	// before a secondary re-checks the primary after a promotion attempt that
	// did not make it primary (lost race, failed promote, transient error).
	// A heartbeat from a live primary resets the backoff. Zero means default.
	RetryBackoffMin time.Duration
	RetryBackoffMax time.Duration

	// ShutdownAcquireWindow is how long a secondary keeps retrying the lock
	// after receiving instance.shutdown: the primary announces its shutdown
	// around the time it releases the lock, so a single immediate attempt can
	// lose to a primary that has not released it yet. ShutdownAcquireInterval
	// is the (jittered) pause between attempts. Zero means default.
	ShutdownAcquireWindow   time.Duration
	ShutdownAcquireInterval time.Duration
}

// Defaults for the optional Config fields.
const (
	defaultRetryBackoffMin         = 1 * time.Second
	defaultRetryBackoffMax         = 30 * time.Second
	defaultShutdownAcquireWindow   = 5 * time.Second
	defaultShutdownAcquireInterval = 100 * time.Millisecond
)

// DefaultConfig returns conservative defaults suitable for production use.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval:       5 * time.Second,
		HeartbeatTimeout:        15 * time.Second,
		ProbeInterval:           60 * time.Second,
		Enabled:                 true,
		RetryBackoffMin:         defaultRetryBackoffMin,
		RetryBackoffMax:         defaultRetryBackoffMax,
		ShutdownAcquireWindow:   defaultShutdownAcquireWindow,
		ShutdownAcquireInterval: defaultShutdownAcquireInterval,
	}
}

func orDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

func (c Config) retryBackoffMin() time.Duration {
	return orDefault(c.RetryBackoffMin, defaultRetryBackoffMin)
}

func (c Config) retryBackoffMax() time.Duration {
	return max(orDefault(c.RetryBackoffMax, defaultRetryBackoffMax), c.retryBackoffMin())
}

func (c Config) shutdownAcquireWindow() time.Duration {
	return orDefault(c.ShutdownAcquireWindow, defaultShutdownAcquireWindow)
}

func (c Config) shutdownAcquireInterval() time.Duration {
	return orDefault(c.ShutdownAcquireInterval, defaultShutdownAcquireInterval)
}

// BusPublisher is the subset of ipc.Bus used by the Watcher for publishing.
type BusPublisher interface {
	Publish(topic string, payload any) error
}

// EventSubscriber is the subset of ipc.Client used by the Watcher for subscribing.
type EventSubscriber interface {
	SubscribeTo(pubEndpoint string, topics ...string) (<-chan ipc.Envelope, error)
}

// PromoteFunc is called when this secondary wins the promotion race.
// The lockFile parameter is the open flock file the caller has already acquired
// (and must keep open for the lifetime of the primary role).
// If PromoteFunc returns an error the lockFile is released and the promotion is
// considered failed.
type PromoteFunc func(ctx context.Context, lockFile *os.File) error

// ProbePrimaryFunc is an optional callback the Watcher uses to actively probe
// the primary via RPC (instance.ping). Set via SetProbePrimary.
type ProbePrimaryFunc func(ctx context.Context) error

// failoverOutcome is the result of one promotion attempt.
type failoverOutcome int

const (
	// outcomeSkipped: no attempt was made (failover disabled, or no promotion
	// callback registered, so taking the lock would create a zombie primary).
	outcomeSkipped failoverOutcome = iota
	// outcomeLostRace: the lock is held by another instance.
	outcomeLostRace
	// outcomeFailed: lock error, or the promotion callback failed (the lock
	// was released again).
	outcomeFailed
	// outcomePromoted: this instance is now the primary.
	outcomePromoted
)

// Watcher monitors primary liveness and coordinates role transitions.
//   - On the primary instance: publishes periodic heartbeats and instance.shutdown on exit.
//   - On secondary instances: watches for heartbeats and triggers promotion when none arrive.
//     The secondary keeps monitoring after any attempt that does not promote it
//     (lost race, failed promote, disabled or transient errors), and after a
//     successful promotion it switches to publishing heartbeats as the primary.
type Watcher struct {
	cfg        Config
	instanceID string
	workdir    string
	pubPort    int
	rpcPort    int

	// client is non-nil on secondary only.
	client EventSubscriber
	// pubEndpoint is the primary PUB address the secondary subscribes to.
	pubEndpoint string

	// mu guards role, bus, lastHeartbeat, the callbacks and cfg.Enabled.
	mu   sync.RWMutex
	role string
	// bus is the publisher used while this instance is the primary. Set by the
	// constructor on a primary, or by SetPrimaryBus after a promotion.
	bus           BusPublisher
	lastHeartbeat time.Time

	// onPromote is called when this secondary wins the promotion race.
	// The lockFile is passed so the callback can hold the lock for the new primary's lifetime.
	onPromote PromoteFunc
	// onDemote is called when this instance loses primary status (future use).
	onDemote func(ctx context.Context) error

	// probePrimary is an optional per-prompt / background probe function.
	probePrimary ProbePrimaryFunc

	// failoverMu serialises promotion attempts: the monitoring goroutine and
	// CheckAndMaybeFailover (prompt path) may both decide the primary is dead
	// at the same moment.
	failoverMu sync.Mutex
	// promotedC is closed when this instance becomes the primary, so the
	// monitoring loop notices a promotion won by CheckAndMaybeFailover.
	promotedC    chan struct{}
	promotedOnce sync.Once

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
	onPromote PromoteFunc,
	onDemote func(ctx context.Context) error,
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
		promotedC:    make(chan struct{}),
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

// SetPromoteCallback replaces the promotion callback. Safe for concurrent use;
// it takes effect for the next promotion attempt. Until a callback is set the
// watcher never takes the IPC lock (see triggerFailover).
func (w *Watcher) SetPromoteCallback(fn PromoteFunc) {
	w.mu.Lock()
	w.onPromote = fn
	w.mu.Unlock()
}

// SetPrimaryBus registers the bus this watcher publishes heartbeats and
// instance.shutdown on once it is the primary. A promotion callback calls it
// with the bus it just started, so the promoted instance behaves like a
// primary started by Bootstrap. Safe for concurrent use.
func (w *Watcher) SetPrimaryBus(bus BusPublisher) {
	w.mu.Lock()
	w.bus = bus
	w.mu.Unlock()
}

// SetProbePrimary registers an active-probe function used by the 60-second background
// ticker and by CheckAndMaybeFailover for per-prompt liveness checks.
func (w *Watcher) SetProbePrimary(fn ProbePrimaryFunc) {
	w.mu.Lock()
	w.probePrimary = fn
	w.mu.Unlock()
}

// Role returns the current role ("primary" or "secondary").
func (w *Watcher) Role() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.role
}

// CheckAndMaybeFailover performs an immediate active probe of the primary and, if
// unreachable and auto-failover is enabled, triggers the promotion sequence.
// Designed to be called before processing each user prompt on a secondary.
// Returns nil if the primary is alive or if this is already the primary.
// Returns an error only if the probe fails AND promotion also fails.
func (w *Watcher) CheckAndMaybeFailover(ctx context.Context) error {
	w.mu.RLock()
	role := w.role
	probe := w.probePrimary
	enabled := w.cfg.Enabled
	w.mu.RUnlock()

	if role == "primary" {
		return nil
	}

	if probe == nil {
		return nil // no probe function registered — skip
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := probe(probeCtx); err != nil {
		logging.Warn("failover: per-prompt probe detected primary is unreachable",
			"error", err,
			"instance_id", w.instanceID,
		)
		if enabled && w.triggerFailover(ctx, "probe", "") == outcomePromoted {
			return nil
		}
		return fmt.Errorf("primary unreachable: %w", err)
	}
	return nil
}

// Start begins monitoring in the background. For the primary it publishes heartbeats;
// for secondaries it watches for them and triggers promotion on absence.
func (w *Watcher) Start(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)
	if w.Role() == "primary" {
		go w.runPrimary(ctx)
	} else {
		go w.runSecondary(ctx)
	}
}

// Shutdown stops the watcher. On primary instances it first publishes instance.shutdown.
// Safe to call even if Start was never called.
func (w *Watcher) Shutdown(ctx context.Context) {
	w.mu.RLock()
	role, bus := w.role, w.bus
	w.mu.RUnlock()
	if role == "primary" && bus != nil {
		payload := protocol.ShutdownPayload{
			InstanceID: w.instanceID,
			Reason:     "graceful shutdown",
		}
		if err := bus.Publish(protocol.TopicInstanceShutdown, payload); err != nil {
			logPublishError("failover: failed to publish shutdown event", err)
		}
	}
	// If Start was never called, cancel is nil and done is never closed — nothing to stop.
	if w.cancel == nil {
		return
	}
	w.cancel()
	<-w.done
}

// logPublishError logs a publish failure. A bus already shut down (the
// primary's ordered handover closes it before the watcher stops) is expected
// and only logged at debug level.
func logPublishError(msg string, err error) {
	if errors.Is(err, ipc.ErrBusClosed) {
		logging.Debug(msg, "error", err)
		return
	}
	logging.Warn(msg, "error", err)
}

// runPrimary publishes a heartbeat every HeartbeatInterval until ctx is cancelled.
func (w *Watcher) runPrimary(ctx context.Context) {
	defer close(w.done)
	w.publishHeartbeats(ctx, nil)
}

// publishHeartbeats publishes a heartbeat every HeartbeatInterval until ctx is
// cancelled. When drain is non-nil it is read and discarded meanwhile: after a
// promotion the old subscription (to what is now this instance's own PUB
// endpoint) must keep being drained, or zmq4 backpressure from the unread SUB
// connection would eventually block the PUB writer for every subscriber.
func (w *Watcher) publishHeartbeats(ctx context.Context, drain <-chan ipc.Envelope) {
	ticker := time.NewTicker(w.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-drain:
			if !ok {
				drain = nil
			}
		case <-ticker.C:
			w.mu.RLock()
			bus := w.bus
			w.mu.RUnlock()
			if bus == nil {
				continue
			}
			payload := protocol.HeartbeatPayload{
				InstanceID: w.instanceID,
				StartedAt:  time.Now(),
			}
			if err := bus.Publish(protocol.TopicInstanceHeartbeat, payload); err != nil {
				logPublishError("failover: primary failed to publish heartbeat", err)
			}
		}
	}
}

// runSecondary subscribes to the primary's PUB socket and watches for heartbeats.
// It also:
//   - listens for instance.shutdown to react immediately on graceful primary exit
//   - listens for instance.promoted to reset the heartbeat timer when another
//     secondary was promoted and resets its connection state
//   - runs a background probe ticker every ProbeInterval (default 60s) as a
//     complementary liveness check independent of PUB/SUB connectivity
//
// It never returns after a single promotion attempt: when an attempt does not
// promote this instance it schedules the next check with a jittered, doubling
// backoff (reset by any heartbeat from a live primary) and keeps monitoring,
// so a later death of the new primary is noticed too. Once promoted it turns
// into the primary's heartbeat loop.
func (w *Watcher) runSecondary(ctx context.Context) {
	defer close(w.done)

	if w.client == nil || w.pubEndpoint == "" {
		logging.Warn("failover: secondary watcher has no IPC client or pubEndpoint; monitoring disabled")
		return
	}

	backoff := w.cfg.retryBackoffMin()
	nextBackoff := func() time.Duration {
		d := jitter(backoff)
		backoff = min(backoff*2, w.cfg.retryBackoffMax())
		return d
	}

	var ch <-chan ipc.Envelope
	subscribe := func() {
		sub, err := w.client.SubscribeTo(w.pubEndpoint,
			protocol.TopicInstanceHeartbeat,
			protocol.TopicInstanceShutdown,
			protocol.TopicInstancePromoted,
		)
		if err != nil {
			logging.Warn("failover: secondary failed to subscribe to primary events; will retry", "error", err)
			ch = nil
			return
		}
		ch = sub
	}
	subscribe()

	w.mu.Lock()
	w.lastHeartbeat = time.Now()
	w.mu.Unlock()

	heartbeatTimer := time.NewTimer(w.cfg.HeartbeatTimeout)
	defer heartbeatTimer.Stop()

	probeTicker := time.NewTicker(w.cfg.ProbeInterval)
	defer probeTicker.Stop()

	// attempt runs one promotion attempt and reports whether this instance is
	// now the primary. Otherwise it schedules the next check.
	attempt := func(trigger, shutdownInstanceID string) bool {
		if w.triggerFailover(ctx, trigger, shutdownInstanceID) == outcomePromoted {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		if ch == nil {
			subscribe()
		}
		d := nextBackoff()
		logging.Info("failover: staying secondary; will re-check the primary",
			"trigger", trigger,
			"retry_in", d,
		)
		resetTimer(heartbeatTimer, d)
		return false
	}

	for {
		if w.Role() == "primary" {
			break
		}
		promoted := false
		select {
		case <-ctx.Done():
			return

		case <-w.promotedC:
			promoted = true

		// Background active probe (every 60 s by default)
		case <-probeTicker.C:
			if w.runProbe(ctx) == outcomePromoted {
				promoted = true
			}

		case env, ok := <-ch:
			if !ok {
				// Subscription channel closed — primary socket gone.
				logging.Warn("failover: heartbeat subscription channel closed; primary may be dead")
				ch = nil
				promoted = attempt("subscription_closed", "")
				break
			}
			switch env.Topic {
			case protocol.TopicInstanceHeartbeat:
				w.mu.Lock()
				w.lastHeartbeat = time.Now()
				w.mu.Unlock()
				backoff = w.cfg.retryBackoffMin()
				resetTimer(heartbeatTimer, w.cfg.HeartbeatTimeout)

			case protocol.TopicInstanceShutdown:
				logging.Info("failover: received graceful shutdown from primary; starting failover immediately")
				promoted = attempt("shutdown", shutdownInstanceID(env))

			case protocol.TopicInstancePromoted:
				// Another secondary became the new primary. Reset the heartbeat
				// timer so we don't immediately try to promote ourselves again.
				logging.Info("failover: received instance.promoted — another secondary took over; resetting heartbeat timer")
				w.mu.Lock()
				w.lastHeartbeat = time.Now()
				w.mu.Unlock()
				backoff = w.cfg.retryBackoffMin()
				resetTimer(heartbeatTimer, w.cfg.HeartbeatTimeout)
			}

		case <-heartbeatTimer.C:
			w.mu.RLock()
			last := w.lastHeartbeat
			w.mu.RUnlock()
			logging.Warn("failover: no heartbeat received within timeout",
				"timeout", w.cfg.HeartbeatTimeout,
				"last_heartbeat", last,
			)
			promoted = attempt("heartbeat_timeout", "")
		}
		if promoted {
			break
		}
	}

	// This instance is the primary now: publish heartbeats on the bus the
	// promotion callback registered (SetPrimaryBus) until the watcher stops.
	w.publishHeartbeats(ctx, ch)
}

// shutdownInstanceID extracts the InstanceID of the primary that announced
// its shutdown, or "" when the payload cannot be decoded.
func shutdownInstanceID(env ipc.Envelope) string {
	var p protocol.ShutdownPayload
	if err := json.Unmarshal(env.Payload, &p); err == nil && p.InstanceID != "" {
		return p.InstanceID
	}
	return env.InstanceID
}

// runProbe executes the registered ProbePrimaryFunc and triggers failover if the
// probe fails and auto-failover is enabled.
func (w *Watcher) runProbe(ctx context.Context) failoverOutcome {
	w.mu.RLock()
	probe := w.probePrimary
	enabled := w.cfg.Enabled
	w.mu.RUnlock()

	if probe == nil {
		return outcomeSkipped
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := probe(probeCtx); err != nil {
		logging.Warn("failover: background probe detected primary is unreachable",
			"error", err,
			"instance_id", w.instanceID,
		)
		if enabled {
			return w.triggerFailover(ctx, "probe", "")
		}
	}
	return outcomeSkipped
}

// triggerFailover is called on the secondary when the primary appears dead.
// It checks the feature flag and the promotion callback, attempts lock
// acquisition, and calls onPromote on success. The acquired lock file is passed
// directly to onPromote so the promotion callback can hold it for the lifetime
// of the new primary role.
//
// shutdownInstanceID is set when the trigger is an instance.shutdown event: the
// lock is then retried for ShutdownAcquireWindow instead of once (see
// acquireLock).
func (w *Watcher) triggerFailover(ctx context.Context, trigger, shutdownInstanceID string) failoverOutcome {
	w.failoverMu.Lock()
	defer w.failoverMu.Unlock()

	w.mu.RLock()
	enabled := w.cfg.Enabled
	role := w.role
	onPromote := w.onPromote
	w.mu.RUnlock()

	if role == "primary" {
		// Another caller (prompt probe vs. monitoring loop) already promoted us.
		return outcomePromoted
	}
	if !enabled {
		logging.Warn("failover: primary appears dead but auto-failover is disabled (enable with --auto-failover)")
		return outcomeSkipped
	}
	if onPromote == nil {
		// Taking the lock without a callback able to open the RW DB and bind the
		// bus would make this process a zombie primary: its PID in the lock file,
		// no ROUTER listening. The next instance to start would probe nothing,
		// then SIGKILL this process. Stay secondary instead.
		logging.Warn("failover: primary appears dead but this instance has no promotion callback; not taking the IPC lock",
			"trigger", trigger,
			"instance_id", w.instanceID,
		)
		return outcomeSkipped
	}

	logging.Info("failover: attempting to promote to primary",
		"trigger", trigger,
		"instance_id", w.instanceID,
		"workdir", w.workdir,
	)

	isPrimary, lockFile, err := w.acquireLock(ctx, shutdownInstanceID)
	if err != nil {
		logging.Warn("failover: lock acquisition error; aborting promotion", "error", err)
		return outcomeFailed
	}

	if !isPrimary {
		// Another secondary acquired the lock first.
		logging.Info("failover: another instance holds the lock; staying secondary")
		return outcomeLostRace
	}

	// We hold the lock. Pass it directly to onPromote so the callback can keep it
	// open for the lifetime of the new primary session.
	logging.Info("failover: acquired lock; invoking promotion callback",
		"instance_id", w.instanceID,
	)

	if promoteErr := onPromote(ctx, lockFile); promoteErr != nil {
		logging.Warn("failover: promotion callback returned error; releasing lock", "error", promoteErr)
		ipc.ReleaseLock(lockFile)
		return outcomeFailed
	}

	w.mu.Lock()
	oldRole := w.role
	w.role = "primary"
	w.mu.Unlock()
	w.promotedOnce.Do(func() { close(w.promotedC) })

	w.notify(RoleChanged{OldRole: oldRole, NewRole: "primary"})
	logging.Info("failover: promotion complete", "instance_id", w.instanceID)
	return outcomePromoted
}

// acquireLock tries to take the IPC lock. Normally it makes a single attempt.
// After an instance.shutdown (shutdownInstanceID != "") it keeps retrying for
// ShutdownAcquireWindow with jittered pauses, because the old primary may still
// hold the lock for a moment after (or, with an older binary, well after) the
// announcement. It stops early when the lock file names an instance other than
// the one that shut down: another secondary already won the handover.
func (w *Watcher) acquireLock(ctx context.Context, shutdownInstanceID string) (bool, *os.File, error) {
	if shutdownInstanceID == "" {
		isPrimary, _, lockFile, err := ipc.AcquireLock(w.workdir, w.instanceID, w.pubPort, w.rpcPort)
		return isPrimary, lockFile, err
	}

	deadline := time.Now().Add(w.cfg.shutdownAcquireWindow())
	for {
		isPrimary, info, lockFile, err := ipc.AcquireLock(w.workdir, w.instanceID, w.pubPort, w.rpcPort)
		switch {
		case err != nil:
			// Transient (e.g. the lock file is being rewritten by the winner);
			// retry within the window.
			logging.Debug("failover: lock attempt after shutdown failed; retrying", "error", err)
		case isPrimary:
			return true, lockFile, nil
		case info != nil && info.InstanceID != "" && info.InstanceID != shutdownInstanceID:
			return false, nil, nil
		}
		if time.Now().After(deadline) {
			return false, nil, err
		}
		select {
		case <-ctx.Done():
			return false, nil, ctx.Err()
		case <-time.After(jitter(w.cfg.shutdownAcquireInterval())):
		}
	}
}

// jitter returns d scaled by a random factor in [0.5, 1.5), so secondaries that
// saw the same event do not retry in lockstep.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return d/2 + time.Duration(rand.Int64N(int64(d)))
}

// notify sends a RoleChanged event on RoleChangedC without blocking.
func (w *Watcher) notify(rc RoleChanged) {
	select {
	case w.RoleChangedC <- rc:
	default:
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
// onPromote may be nil; it can be set later via SetPromoteCallback. While it is
// nil the watcher monitors but never takes the IPC lock.
func NewWatcherForSecondary(
	cfg Config,
	instanceID, workdir string,
	pubPort, rpcPort int,
	client EventSubscriber,
	pubEndpoint string,
	onPromote PromoteFunc,
) *Watcher {
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
	return fmt.Sprintf("role=secondary enabled=%v promote_callback=%v heartbeat_timeout=%s probe_interval=%s last_heartbeat=%s",
		enabled, w.onPromote != nil, w.cfg.HeartbeatTimeout, w.cfg.ProbeInterval, w.lastHeartbeat.Format(time.RFC3339))
}

// resetTimer drains and resets a timer safely.
func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}
