package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

func TestExternalRunsBusyAndTTL(t *testing.T) {
	r := newExternalRuns(10 * time.Second)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }

	if got := r.update("f1", []string{"s1"}, []string{"s1"}); !slices.Equal(got, []string{"s1"}) {
		t.Fatalf("flipped on start = %v, want [s1]", got)
	}
	if !r.isBusy("s1") || r.isBusy("s2") {
		t.Fatalf("busy state wrong after start")
	}
	if !r.isForeign("s1") {
		t.Fatalf("s1 should be remembered as follower-owned")
	}

	// A heartbeat with the same set flips nothing.
	if got := r.update("f1", []string{"s1"}, nil); len(got) != 0 {
		t.Fatalf("flipped on heartbeat = %v, want none", got)
	}

	// The follower reports the run finished.
	if got := r.update("f1", nil, nil); !slices.Equal(got, []string{"s1"}) {
		t.Fatalf("flipped on end = %v, want [s1]", got)
	}
	if r.isBusy("s1") {
		t.Fatalf("s1 still busy after the follower reported it idle")
	}

	// A follower that stops syncing (crash) expires after the TTL.
	r.update("f2", []string{"s2"}, nil)
	now = now.Add(9 * time.Second)
	if !r.isBusy("s2") {
		t.Fatalf("s2 expired before the TTL")
	}
	now = now.Add(2 * time.Second)
	if r.isBusy("s2") {
		t.Fatalf("s2 still busy after the TTL")
	}

	var nilRuns *externalRuns
	if nilRuns.isBusy("s1") || nilRuns.isForeign("s1") || nilRuns.update("x", []string{"a"}, nil) != nil {
		t.Fatalf("nil registry must be inert")
	}
}

func TestExternalRunsForeignIsBounded(t *testing.T) {
	r := newExternalRuns(time.Minute)
	base := time.Unix(0, 0)
	for i := 0; i < foreignSessionLimit+10; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		r.now = func() time.Time { return at }
		r.update("f", nil, []string{fmt.Sprintf("s%d", i)})
	}
	if len(r.foreign) != foreignSessionLimit {
		t.Fatalf("foreign size = %d, want %d", len(r.foreign), foreignSessionLimit)
	}
	if r.isForeign("s0") || !r.isForeign(fmt.Sprintf("s%d", foreignSessionLimit+9)) {
		t.Fatalf("pruning must drop the oldest entries")
	}
}

func TestFollowerSyncCoalescesAndReportsBusy(t *testing.T) {
	busy := map[string]bool{}
	var mu sync.Mutex
	isBusy := func(id string) bool { mu.Lock(); defer mu.Unlock(); return busy[id] }
	fs := newFollowerSync("f1", isBusy, nil)
	now := time.Unix(100, 0)

	if _, ok := fs.take(now, false); ok {
		t.Fatalf("nothing to send on an idle follower")
	}

	mu.Lock()
	busy["s1"] = true
	mu.Unlock()
	fs.noteSession(pubsub.Event[session.Session]{Type: pubsub.CreatedEvent, Payload: session.Session{ID: "s1"}})
	fs.noteSession(pubsub.Event[session.Session]{Type: pubsub.UpdatedEvent, Payload: session.Session{ID: "s1"}})
	for i := 0; i < 50; i++ { // streaming deltas
		fs.noteMessage(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: message.Message{ID: "m1", SessionID: "s1"}})
	}
	fs.noteMessage(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: message.Message{ID: "m2", SessionID: "s1"}})

	p, ok := fs.take(now, false)
	if !ok {
		t.Fatalf("expected a batch")
	}
	if len(p.Sessions) != 1 || p.Sessions[0].Kind != protocol.ChangeCreated {
		t.Fatalf("sessions = %+v, want one created", p.Sessions)
	}
	if len(p.Messages) != 2 || p.Messages[0].ID != "m1" || p.Messages[1].ID != "m2" {
		t.Fatalf("messages = %+v, want m1,m2 coalesced", p.Messages)
	}
	if !slices.Equal(p.BusySessions, []string{"s1"}) {
		t.Fatalf("busy = %v, want [s1]", p.BusySessions)
	}
	fs.ack(p, nil, now)

	// Nothing changed and the heartbeat is not due yet.
	if _, ok := fs.take(now.Add(time.Second), false); ok {
		t.Fatalf("no batch expected before the heartbeat")
	}
	// Heartbeat while busy.
	hb, ok := fs.take(now.Add(followerSyncHeartbeat), false)
	if !ok || !slices.Equal(hb.BusySessions, []string{"s1"}) || len(hb.Messages) != 0 {
		t.Fatalf("heartbeat = %+v ok=%v", hb, ok)
	}
	fs.ack(hb, nil, now.Add(followerSyncHeartbeat))

	// Run ends: the empty busy set is reported once.
	mu.Lock()
	busy["s1"] = false
	mu.Unlock()
	end, ok := fs.take(now.Add(3*time.Second), false)
	if !ok || len(end.BusySessions) != 0 || end.BusySessions == nil {
		t.Fatalf("end batch = %+v ok=%v, want empty (non-nil) busy set", end, ok)
	}
	fs.ack(end, nil, now.Add(3*time.Second))
	if _, ok := fs.take(now.Add(10*time.Second), false); ok {
		t.Fatalf("idle follower must stay silent")
	}
}

func TestFollowerSyncRequeuesOnFailureAndFinalClearsBusy(t *testing.T) {
	fs := newFollowerSync("f1", func(string) bool { return true }, nil)
	now := time.Unix(100, 0)
	fs.noteMessage(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: message.Message{ID: "m1", SessionID: "s1"}})
	p, _ := fs.take(now, false)
	fs.ack(p, errors.New("leader down"), now)

	// A newer update of the same row keeps the created kind.
	fs.noteMessage(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: message.Message{ID: "m1", SessionID: "s1"}})
	retry, ok := fs.take(now, false)
	if !ok || len(retry.Messages) != 1 || retry.Messages[0].Kind != protocol.ChangeCreated {
		t.Fatalf("retry = %+v, want m1 created", retry)
	}
	if !slices.Equal(retry.BusySessions, []string{"s1"}) {
		t.Fatalf("busy = %v, want [s1] (never acknowledged)", retry.BusySessions)
	}
	fs.ack(retry, nil, now)

	final, ok := fs.take(now, true)
	if !ok || len(final.BusySessions) != 0 {
		t.Fatalf("final = %+v ok=%v, want an empty busy set", final, ok)
	}
}

func TestMergeChangeDeletionWins(t *testing.T) {
	got := mergeChange(protocol.FollowerChange{ID: "x", Kind: protocol.ChangeCreated}, true, protocol.FollowerChange{ID: "x", Kind: protocol.ChangeDeleted})
	if got.Kind != protocol.ChangeDeleted {
		t.Fatalf("kind = %q, want deleted", got.Kind)
	}
}

// migratedDBPath returns a migrated pando.db file both "processes" open.
func migratedDBPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pando.db")
	conn, err := sql.Open("pando-sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	goose.SetBaseFS(db.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return path
}

func openQuerier(t *testing.T, path string) db.Querier {
	t.Helper()
	conn, err := sql.Open("pando-sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return db.New(conn)
}

// TestFollowerChangesReachLeaderBrokers drives the whole path without ZMQ: a
// follower's services write the shared database, followerSync batches the
// events, and the leader's follower.sync handler republishes them on the
// leader's own brokers and marks the session busy elsewhere.
func TestFollowerChangesReachLeaderBrokers(t *testing.T) {
	path := migratedDBPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	leader := &App{
		Sessions:      session.NewService(openQuerier(t, path)),
		Messages:      message.NewService(openQuerier(t, path)),
		externalRuns:  newExternalRuns(externalRunTTL),
		ipcInstanceID: "leader",
	}
	leaderSess := leader.Sessions.Subscribe(ctx)
	leaderMsgs := leader.Messages.Subscribe(ctx)

	fq := openQuerier(t, path)
	fSessions := session.NewService(fq)
	fMessages := message.NewService(fq)
	running := true
	send := func(ctx context.Context, p protocol.FollowerSyncParams) error {
		raw, _ := json.Marshal(p)
		_, err := leader.handleFollowerSync(ctx, protocol.MethodFollowerSync, raw)
		return err
	}
	fs := newFollowerSync("follower", func(string) bool { return running }, send)
	fSessCh := fSessions.Subscribe(ctx)
	fMsgCh := fMessages.Subscribe(ctx)

	sess, err := fSessions.Create(ctx, "subagent")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	msg, err := fMessages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: "hel"}},
	})
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	msg.Parts = []message.ContentPart{message.TextContent{Text: "hello"}}
	if err := fMessages.Update(ctx, msg); err != nil {
		t.Fatalf("update message: %v", err)
	}
	fs.drain(fSessCh, fMsgCh)
	if err := fs.flush(ctx, false); err != nil {
		t.Fatalf("flush: %v", err)
	}

	select {
	case ev := <-leaderSess:
		if ev.Type != pubsub.CreatedEvent || ev.Payload.ID != sess.ID || ev.Payload.Title != "subagent" {
			t.Fatalf("leader session event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("leader got no session event")
	}
	select {
	case ev := <-leaderMsgs:
		if ev.Type != pubsub.CreatedEvent || ev.Payload.ID != msg.ID || ev.Payload.Content().Text != "hello" {
			t.Fatalf("leader message event = %+v (want the row as stored)", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("leader got no message event")
	}
	if !leader.SessionBusyElsewhere(sess.ID) {
		t.Fatalf("leader should see the session busy elsewhere")
	}

	// The run ends: the leader unlocks the session and republishes it.
	running = false
	if err := fs.flush(ctx, false); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if leader.SessionBusyElsewhere(sess.ID) {
		t.Fatalf("session still busy elsewhere after the run ended")
	}
	select {
	case ev := <-leaderSess:
		if ev.Payload.ID != sess.ID {
			t.Fatalf("unexpected session event %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("leader did not republish the session when its run ended")
	}

	// A sync carrying the leader's own instance id is ignored (no loop).
	raw, _ := json.Marshal(protocol.FollowerSyncParams{InstanceID: "leader", BusySessions: []string{sess.ID}})
	if _, err := leader.handleFollowerSync(ctx, protocol.MethodFollowerSync, raw); err != nil {
		t.Fatalf("own sync: %v", err)
	}
	if leader.SessionBusyElsewhere(sess.ID) {
		t.Fatalf("the leader must not treat its own sync as external")
	}
}

// TestFollowerSyncOverIPC checks the RPC wiring: follower.sync registered on a
// real bus and called through an ipc.Client.
func TestFollowerSyncOverIPC(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := &App{externalRuns: newExternalRuns(externalRunTTL), ipcInstanceID: "leader"}
	bus := ipc.NewBus("leader")
	bus.RegisterMethod(protocol.MethodFollowerSync, leader.handleFollowerSync)
	pub, rpc, err := ipc.FindFreePorts()
	if err != nil {
		t.Skipf("no free ports: %v", err)
	}
	if err := bus.Start(ctx, pub, rpc); err != nil {
		t.Skipf("cannot start bus: %v", err)
	}
	defer func() { _ = bus.Shutdown() }()

	client, err := ipc.NewClient(ctx)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer func() { _ = client.Close() }()
	p := protocol.FollowerSyncParams{InstanceID: "f1", BusySessions: []string{"s1"}}
	if _, err := client.CallWithTimeout(ctx, fmt.Sprintf("tcp://127.0.0.1:%d", rpc), protocol.MethodFollowerSync, p, 3*time.Second); err != nil {
		t.Fatalf("call: %v", err)
	}
	if !leader.SessionBusyElsewhere("s1") {
		t.Fatalf("s1 should be busy elsewhere after the RPC")
	}
}

func TestExternalRunsExpireReportsDeadFollowerSessions(t *testing.T) {
	r := newExternalRuns(10 * time.Second)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }
	r.update("dead", []string{"s1", "shared"}, nil)
	now = now.Add(5 * time.Second)
	r.update("alive", []string{"shared"}, nil)

	if got := r.expire(); len(got) != 0 {
		t.Fatalf("nothing should expire yet, got %v", got)
	}
	now = now.Add(6 * time.Second) // "dead" is past its TTL, "alive" is not
	if got := r.expire(); !slices.Equal(got, []string{"s1"}) {
		t.Fatalf("expired = %v, want [s1] (shared is still run by a live follower)", got)
	}
	if r.isBusy("s1") || !r.isBusy("shared") {
		t.Fatalf("busy state wrong after expiry")
	}
	if got := r.expire(); len(got) != 0 {
		t.Fatalf("second expire = %v, want none", got)
	}
}
