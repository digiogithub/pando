package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

// Follower → leader propagation of session/message changes.
//
// Every process writes pando.db directly, but only the leader's in-process
// brokers feed its UIs (WebUI/desktop, TUI). A follower (e.g. a mesnada
// `pando -p` subagent) therefore reports, over IPC, the ids of the sessions and
// messages it changed plus the sessions it is running; the leader re-reads the
// rows and republishes them locally, and treats those sessions as busy
// elsewhere until the follower reports the run finished or stops syncing.
const (
	// followerSyncFlushInterval coalesces the per-delta message updates of a
	// streaming run into at most one sync per interval.
	followerSyncFlushInterval = 150 * time.Millisecond
	// followerSyncHeartbeat re-sends the busy set while a run is active, which
	// keeps the leader's entry alive (see externalRunTTL).
	followerSyncHeartbeat = 2 * time.Second
	// followerSyncCallTimeout bounds one follower.sync RPC.
	followerSyncCallTimeout = 2 * time.Second
	// followerSyncRetryDelay pauses syncing after a failed RPC (leader gone,
	// older leader without the method) instead of retrying every flush.
	followerSyncRetryDelay = 2 * time.Second
	// followerSyncFinalTimeout bounds the last sync sent at shutdown.
	followerSyncFinalTimeout = time.Second
	// externalRunTTL is how long the leader trusts a follower's busy set
	// without a new sync, so a crashed follower never leaves a session locked.
	externalRunTTL = 10 * time.Second
	// foreignSessionLimit bounds the set of session ids the leader remembers as
	// owned by followers.
	foreignSessionLimit = 2000
)

// ---- leader side ----

// externalRuns is the leader's view of the sessions other processes run.
type externalRuns struct {
	mu        sync.Mutex
	ttl       time.Duration
	now       func() time.Time
	instances map[string]*externalInstance
	// foreign remembers sessions followers reported changes for, with the time
	// of the last report. The leader leaves their indexing to the follower.
	foreign map[string]time.Time
}

type externalInstance struct {
	busy map[string]struct{}
	seen time.Time
}

func newExternalRuns(ttl time.Duration) *externalRuns {
	return &externalRuns{
		ttl:       ttl,
		now:       time.Now,
		instances: make(map[string]*externalInstance),
		foreign:   make(map[string]time.Time),
	}
}

// update replaces what is known about instanceID with busy, records touched as
// follower-owned sessions, and returns the sessions whose busy-elsewhere state
// changed (so the caller can republish them).
func (r *externalRuns) update(instanceID string, busy, touched []string) []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	before := r.busySetLocked(now)

	set := make(map[string]struct{}, len(busy))
	for _, id := range busy {
		if id != "" {
			set[id] = struct{}{}
		}
	}
	if len(set) == 0 {
		delete(r.instances, instanceID)
	} else {
		r.instances[instanceID] = &externalInstance{busy: set, seen: now}
	}
	for _, id := range append(slices.Clone(busy), touched...) {
		if id != "" {
			r.foreign[id] = now
		}
	}
	r.pruneForeignLocked()

	after := r.busySetLocked(now)
	var changed []string
	for id := range before {
		if _, ok := after[id]; !ok {
			changed = append(changed, id)
		}
	}
	for id := range after {
		if _, ok := before[id]; !ok {
			changed = append(changed, id)
		}
	}
	slices.Sort(changed)
	return changed
}

// busySetLocked returns the sessions busy in a live (not expired) instance and
// drops the expired ones.
func (r *externalRuns) busySetLocked(now time.Time) map[string]struct{} {
	out := make(map[string]struct{})
	for id, inst := range r.instances {
		if now.Sub(inst.seen) > r.ttl {
			delete(r.instances, id)
			continue
		}
		for sid := range inst.busy {
			out[sid] = struct{}{}
		}
	}
	return out
}

func (r *externalRuns) pruneForeignLocked() {
	if len(r.foreign) <= foreignSessionLimit {
		return
	}
	type entry struct {
		id   string
		seen time.Time
	}
	entries := make([]entry, 0, len(r.foreign))
	for id, seen := range r.foreign {
		entries = append(entries, entry{id, seen})
	}
	slices.SortFunc(entries, func(a, b entry) int { return a.seen.Compare(b.seen) })
	for _, e := range entries[:len(entries)-foreignSessionLimit] {
		delete(r.foreign, e.id)
	}
}

// expire drops the instances whose TTL ran out and returns the sessions that
// are no longer busy anywhere because of it (a follower that died mid-run).
func (r *externalRuns) expire() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	var stale []string
	for id, inst := range r.instances {
		if now.Sub(inst.seen) > r.ttl {
			for sid := range inst.busy {
				stale = append(stale, sid)
			}
			delete(r.instances, id)
		}
	}
	live := r.busySetLocked(now)
	out := stale[:0]
	for _, sid := range stale {
		if _, ok := live[sid]; !ok && !slices.Contains(out, sid) {
			out = append(out, sid)
		}
	}
	slices.Sort(out)
	return out
}

// startExternalRunSweeper republishes, as session updates, the sessions whose
// follower stopped syncing (crash), so UIs following the session broker see
// them unlock without waiting for an unrelated event. Leader only; started once.
func (app *App) startExternalRunSweeper() {
	app.externalSweepOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		app.cancelFuncsMutex.Lock()
		app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
		app.cancelFuncsMutex.Unlock()
		go func() {
			ticker := time.NewTicker(externalRunTTL / 4)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					for _, id := range app.externalRuns.expire() {
						if app.Sessions != nil {
							_ = session.Republish(ctx, app.Sessions, id, pubsub.UpdatedEvent)
						}
					}
				}
			}
		}()
	})
}

// isBusy reports whether a live follower is running sessionID.
func (r *externalRuns) isBusy(sessionID string) bool {
	if r == nil || sessionID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.busySetLocked(r.now())[sessionID]
	return ok
}

// isForeign reports whether a follower reported changes for sessionID.
func (r *externalRuns) isForeign(sessionID string) bool {
	if r == nil || sessionID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.foreign[sessionID]
	return ok
}

// SessionBusyElsewhere reports whether another Pando process (an IPC follower,
// typically a mesnada subagent) is running the session right now. Only the
// leader collects this; it is always false on followers.
func (app *App) SessionBusyElsewhere(sessionID string) bool {
	return app.externalRuns.isBusy(sessionID)
}

// RecordExternalRuns replaces the set of sessions instanceID reports running,
// the same bookkeeping follower.sync does. It lets a leader be driven without
// IPC (tests, embedders); an App built without New gets its registry here.
func (app *App) RecordExternalRuns(instanceID string, busy []string) {
	if app.externalRuns == nil {
		app.externalRuns = newExternalRuns(externalRunTTL)
	}
	app.externalRuns.update(instanceID, busy, nil)
}

// changeEventType maps a FollowerChange kind to the pubsub event to publish.
func changeEventType(kind string) pubsub.EventType {
	switch kind {
	case protocol.ChangeCreated:
		return pubsub.CreatedEvent
	case protocol.ChangeDeleted:
		return pubsub.DeletedEvent
	default:
		return pubsub.UpdatedEvent
	}
}

// handleFollowerSync is the leader's follower.sync handler. It never forwards
// anything back over IPC: republished events only reach this process's brokers
// (the bridge mirrors session events on the PUB socket, which followers do not
// consume), so a change cannot loop.
func (app *App) handleFollowerSync(ctx context.Context, _ string, raw json.RawMessage) (json.RawMessage, error) {
	var p protocol.FollowerSyncParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("follower.sync: invalid params: %w", err)
	}
	if p.InstanceID == "" {
		return nil, fmt.Errorf("follower.sync: instance_id is required")
	}
	if p.InstanceID != app.ipcInstanceID {
		app.applyFollowerSync(ctx, p)
	}
	return json.Marshal(protocol.OKResult{OK: true})
}

func (app *App) applyFollowerSync(ctx context.Context, p protocol.FollowerSyncParams) {
	touched := make([]string, 0, len(p.Sessions)+len(p.Messages))
	for _, c := range p.Sessions {
		touched = append(touched, c.ID)
	}
	for _, c := range p.Messages {
		touched = append(touched, c.SessionID)
	}
	flipped := app.externalRuns.update(p.InstanceID, p.BusySessions, touched)

	republished := make(map[string]bool, len(p.Sessions))
	if app.Sessions != nil {
		for _, c := range p.Sessions {
			if err := session.Republish(ctx, app.Sessions, c.ID, changeEventType(c.Kind)); err != nil {
				logging.Debug("follower.sync: republish session", "session_id", c.ID, "error", err)
			}
			republished[c.ID] = true
		}
		// A run that started or ended changes how UIs show the session.
		for _, id := range flipped {
			if !republished[id] {
				_ = session.Republish(ctx, app.Sessions, id, pubsub.UpdatedEvent)
			}
		}
	}
	if app.Messages != nil {
		for _, c := range p.Messages {
			if err := message.Republish(ctx, app.Messages, c.ID, c.SessionID, changeEventType(c.Kind)); err != nil {
				logging.Debug("follower.sync: republish message", "message_id", c.ID, "error", err)
			}
		}
	}
}

// ---- follower side ----

// followerSync collects the local session/message events of a follower and
// turns them into follower.sync batches.
type followerSync struct {
	instanceID string
	isBusy     func(sessionID string) bool
	send       func(ctx context.Context, p protocol.FollowerSyncParams) error

	mu         sync.Mutex
	sessions   map[string]protocol.FollowerChange
	messages   map[string]protocol.FollowerChange
	order      []string // session change ids in arrival order
	msgOrder   []string // message change ids in arrival order
	candidates map[string]struct{}
	// reported is the busy set the leader last acknowledged.
	reported []string
	lastSent time.Time
}

func newFollowerSync(instanceID string, isBusy func(string) bool, send func(context.Context, protocol.FollowerSyncParams) error) *followerSync {
	return &followerSync{
		instanceID: instanceID,
		isBusy:     isBusy,
		send:       send,
		sessions:   make(map[string]protocol.FollowerChange),
		messages:   make(map[string]protocol.FollowerChange),
		candidates: make(map[string]struct{}),
	}
}

func eventKind(t pubsub.EventType) string {
	switch t {
	case pubsub.CreatedEvent:
		return protocol.ChangeCreated
	case pubsub.DeletedEvent:
		return protocol.ChangeDeleted
	default:
		return protocol.ChangeUpdated
	}
}

// mergeChange records c over prev: a row created and then updated in the same
// batch is still reported as created; a deletion always wins.
func mergeChange(prev protocol.FollowerChange, had bool, c protocol.FollowerChange) protocol.FollowerChange {
	if had && prev.Kind == protocol.ChangeCreated && c.Kind == protocol.ChangeUpdated {
		c.Kind = protocol.ChangeCreated
	}
	return c
}

func (f *followerSync) noteSession(ev pubsub.Event[session.Session]) {
	id := ev.Payload.ID
	if id == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	prev, had := f.sessions[id]
	if !had {
		f.order = append(f.order, id)
	}
	f.sessions[id] = mergeChange(prev, had, protocol.FollowerChange{ID: id, Kind: eventKind(ev.Type)})
	if ev.Type != pubsub.DeletedEvent {
		f.candidates[id] = struct{}{}
	}
}

func (f *followerSync) noteMessage(ev pubsub.Event[message.Message]) {
	id := ev.Payload.ID
	if id == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	prev, had := f.messages[id]
	if !had {
		f.msgOrder = append(f.msgOrder, id)
	}
	f.messages[id] = mergeChange(prev, had, protocol.FollowerChange{ID: id, SessionID: ev.Payload.SessionID, Kind: eventKind(ev.Type)})
	if ev.Payload.SessionID != "" {
		f.candidates[ev.Payload.SessionID] = struct{}{}
	}
}

// take builds the next batch, or reports false when there is nothing to say.
// It sends when rows changed, when the busy set differs from what the leader
// acknowledged, or as a heartbeat while sessions are busy. final reports an
// empty busy set (the process is going away).
func (f *followerSync) take(now time.Time, final bool) (protocol.FollowerSyncParams, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	busy := []string{}
	for id := range f.candidates {
		if !final && f.isBusy != nil && f.isBusy(id) {
			busy = append(busy, id)
			continue
		}
		delete(f.candidates, id)
	}
	slices.Sort(busy)

	hasChanges := len(f.sessions) > 0 || len(f.messages) > 0
	busyChanged := !slices.Equal(busy, f.reported)
	heartbeat := len(busy) > 0 && now.Sub(f.lastSent) >= followerSyncHeartbeat
	if !hasChanges && !busyChanged && !heartbeat {
		return protocol.FollowerSyncParams{}, false
	}

	p := protocol.FollowerSyncParams{InstanceID: f.instanceID, BusySessions: busy}
	for _, id := range f.order {
		p.Sessions = append(p.Sessions, f.sessions[id])
	}
	for _, id := range f.msgOrder {
		p.Messages = append(p.Messages, f.messages[id])
	}
	f.sessions = make(map[string]protocol.FollowerChange)
	f.messages = make(map[string]protocol.FollowerChange)
	f.order, f.msgOrder = nil, nil
	return p, true
}

// ack records the outcome of sending p. On failure the changes are queued
// again (behind anything newer) so they are not lost while the leader is
// unreachable; the busy set is recomputed on the next take anyway.
func (f *followerSync) ack(p protocol.FollowerSyncParams, err error, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		f.reported = p.BusySessions
		f.lastSent = now
		return
	}
	for _, c := range p.Sessions {
		if cur, ok := f.sessions[c.ID]; ok {
			f.sessions[c.ID] = mergeChange(c, true, cur)
			continue
		}
		f.sessions[c.ID] = c
		f.order = append(f.order, c.ID)
	}
	for _, c := range p.Messages {
		if cur, ok := f.messages[c.ID]; ok {
			f.messages[c.ID] = mergeChange(c, true, cur)
			continue
		}
		f.messages[c.ID] = c
		f.msgOrder = append(f.msgOrder, c.ID)
	}
}

// flush sends the pending batch, if any.
func (f *followerSync) flush(ctx context.Context, final bool) error {
	now := time.Now()
	p, ok := f.take(now, final)
	if !ok {
		return nil
	}
	err := f.send(ctx, p)
	f.ack(p, err, now)
	return err
}

// run consumes local events and flushes them periodically until ctx ends or
// stop reports true (this process was promoted to leader: from then on its
// own brokers are the ones the UIs follow).
func (f *followerSync) run(ctx context.Context, sessCh <-chan pubsub.Event[session.Session], msgCh <-chan pubsub.Event[message.Message], stop func() bool) {
	ticker := time.NewTicker(followerSyncFlushInterval)
	defer ticker.Stop()
	var retryAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sessCh:
			if !ok {
				sessCh = nil
				continue
			}
			f.noteSession(ev)
		case ev, ok := <-msgCh:
			if !ok {
				msgCh = nil
				continue
			}
			f.noteMessage(ev)
		case now := <-ticker.C:
			if stop != nil && stop() {
				return
			}
			if now.Before(retryAt) {
				continue
			}
			if err := f.flush(ctx, false); err != nil {
				logging.Debug("follower.sync: leader unreachable", "error", err)
				retryAt = now.Add(followerSyncRetryDelay)
			}
		}
	}
}

// drain records the events still buffered in the (cancelled) subscriptions.
// The broker closes them once their context ends, which bounds the loop.
func (f *followerSync) drain(sessCh <-chan pubsub.Event[session.Session], msgCh <-chan pubsub.Event[message.Message]) {
	for sessCh != nil || msgCh != nil {
		select {
		case ev, ok := <-sessCh:
			if !ok {
				sessCh = nil
				continue
			}
			f.noteSession(ev)
		case ev, ok := <-msgCh:
			if !ok {
				msgCh = nil
				continue
			}
			f.noteMessage(ev)
		case <-time.After(50 * time.Millisecond):
			return
		}
	}
}

// StartFollowerSync starts reporting this follower's session/message changes
// and running sessions to the IPC leader. It is a no-op on the leader and on
// instances outside IPC. The final report (no busy sessions) is sent by
// Shutdown, or when ctx ends.
func (app *App) StartFollowerSync(ctx context.Context) {
	if !app.ipcSecondary || app.IPCIsPrimary || app.ipcWorkdir == "" || app.Sessions == nil || app.Messages == nil {
		return
	}
	// A dedicated client: promotion closes app.ipcClient under our feet.
	client, err := ipc.NewClient(ctx)
	if err != nil {
		logging.Warn("follower.sync: cannot create IPC client; the leader will not see this instance's sessions live", "error", err)
		return
	}
	workdir, fallbackPort := app.ipcWorkdir, app.ipcRPCPort
	send := func(ctx context.Context, p protocol.FollowerSyncParams) error {
		// Read the leader's port each time: a failover moves it.
		port := fallbackPort
		if info, err := ipc.ReadLockForPath(workdir); err == nil && info.RPCPort > 0 {
			port = info.RPCPort
		}
		_, err := client.CallWithTimeout(ctx, fmt.Sprintf("tcp://127.0.0.1:%d", port), protocol.MethodFollowerSync, p, followerSyncCallTimeout)
		return err
	}
	isBusy := func(sessionID string) bool {
		a := app.CoderAgent
		return a != nil && a.IsSessionBusy(sessionID)
	}
	fs := newFollowerSync(app.ipcInstanceID, isBusy, send)

	runCtx, cancel := context.WithCancel(ctx)
	sessCh := app.Sessions.Subscribe(runCtx)
	msgCh := app.Messages.Subscribe(runCtx)
	done := make(chan struct{})
	promoted := func() bool { return app.IPCIsPrimary }
	go func() {
		defer close(done)
		defer func() { _ = client.Close() }()
		fs.run(runCtx, sessCh, msgCh, promoted)
		if promoted() {
			return
		}
		fs.drain(sessCh, msgCh)
		// Tell the leader this instance runs nothing any more and hand over the
		// last changes, so the session unlocks now instead of after the TTL.
		finalCtx, finalCancel := context.WithTimeout(context.Background(), followerSyncFinalTimeout)
		defer finalCancel()
		if err := fs.flush(finalCtx, true); err != nil {
			logging.Debug("follower.sync: final sync failed", "error", err)
		}
	}()

	var once sync.Once
	app.followerSyncStop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(followerSyncFinalTimeout + followerSyncCallTimeout):
			}
		})
	}
}

// stopFollowerSync stops the follower sync loop after its final report.
func (app *App) stopFollowerSync() {
	if app.followerSyncStop != nil {
		app.followerSyncStop()
	}
}
