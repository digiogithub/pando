package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/failover"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/ipc/writecoordinator"
	"github.com/digiogithub/pando/internal/message"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/events"
	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/internal/session"
)

// deadPrimaryRPC is where the (dead) old primary served; nothing listens there.
const deadPrimaryRPC = "tcp://127.0.0.1:1"

// testBusSetup mirrors the busSetupFunc cmd/root.go passes to
// SetIPCSecondaryContext (minus the bridge, which needs a coder agent).
func testBusSetup(ctx context.Context, bus *ipc.Bus, rwConn *sql.DB) (PrimaryWriteCoordinator, error) {
	coord := writecoordinator.New(ctx, db.New(rwConn), 64)
	dbproxy.RegisterHandlersWithCoordinator(bus, coord)
	return coord, nil
}

func resetIPCGlobals(t *testing.T) {
	t.Cleanup(func() {
		session.SetIPCPublisher(nil)
		dbproxy.RegisterRemembrancesDispatcher(nil)
	})
}

// secondaryFixture is an App wired exactly like a secondary after Bootstrap:
// a 1-connection 200 ms pool, a DBProxy whose client points at a dead primary,
// services and remembrances stores built on that pool/proxy, and the IPC lock
// already taken (the failover watcher acquires it before calling
// PromoteToPrimary).
type secondaryFixture struct {
	app              *App
	proxy            *dbproxy.DBProxy
	sec              *sql.DB
	kbStore          *kb.KBStore
	evStore          *events.EventStore
	emb              *recordingEmbedder
	dir              string
	pubPort, rpcPort int
	lockFile         *os.File
}

func newSecondaryFixture(t *testing.T) *secondaryFixture {
	t.Helper()
	resetIPCGlobals(t)
	f := &secondaryFixture{dir: t.TempDir(), emb: &recordingEmbedder{}}
	dbPath := filepath.Join(f.dir, "pando.db")

	// A primary created the schema, then died.
	primary, err := db.ConnectAt(dbPath)
	if err != nil {
		t.Fatalf("ConnectAt: %v", err)
	}
	_ = primary.Close()

	f.sec, err = db.ConnectRWSecondaryAt(dbPath)
	if err != nil {
		t.Fatalf("ConnectRWSecondaryAt: %v", err)
	}
	t.Cleanup(func() { _ = f.sec.Close() })
	client, err := ipc.NewClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	f.proxy = dbproxy.New(db.New(f.sec), client, deadPrimaryRPC)

	f.kbStore = kb.NewKBStore(f.sec, f.emb, 1000, 100)
	f.kbStore.SetWriteProxy(f.proxy)
	f.evStore = events.NewEventStore(f.sec, f.emb)
	f.evStore.SetWriteProxy(f.proxy)

	f.pubPort, f.rpcPort, err = ipc.FindFreePorts()
	if err != nil {
		t.Fatal(err)
	}
	isPrimary, _, lockFile, err := ipc.AcquireLock(f.dir, "promote-test", f.pubPort, f.rpcPort)
	if err != nil || !isPrimary {
		t.Fatalf("AcquireLock: primary=%v err=%v", isPrimary, err)
	}
	f.lockFile = lockFile

	f.app = &App{
		Sessions:     session.NewService(f.proxy),
		Messages:     message.NewService(f.proxy),
		DBQuerier:    f.proxy,
		rwConn:       f.sec,
		Remembrances: &rag.RemembrancesService{KB: f.kbStore, Events: f.evStore},
	}
	watcher := failover.NewWatcherForSecondary(failover.DefaultConfig(), "promote-test", f.dir,
		f.pubPort, f.rpcPort, client, deadPrimaryRPC, nil)
	f.app.SetIPCSecondaryContext(client, f.sec, f.dir, "promote-test", f.pubPort, f.rpcPort, watcher, testBusSetup)
	t.Cleanup(f.app.releasePrimaryRole)
	return f
}

// busyTimeoutOnEveryConn checks out n connections at once and returns the
// busy_timeout (ms) each reports.
func busyTimeoutOnEveryConn(t *testing.T, sqlDB *sql.DB, n int) []int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var conns []*sql.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	var out []int
	for range n {
		c, err := sqlDB.Conn(ctx)
		if err != nil {
			t.Fatalf("check out connection: %v", err)
		}
		conns = append(conns, c)
		var v int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&v); err != nil {
			t.Fatalf("PRAGMA busy_timeout: %v", err)
		}
		out = append(out, v)
	}
	return out
}

// TestPromoteToPrimaryInPlaceKeepsServicesWritable is the G1 regression: a
// secondary is promoted, after which every service built on its pool/proxy
// must write directly. The pre-fix PromoteToPrimary closed the pool under the
// services (session/message writes then failed with "sql: database is
// closed") and left the remembrances stores forwarding to a closed client.
func TestPromoteToPrimaryInPlaceKeepsServicesWritable(t *testing.T) {
	f := newSecondaryFixture(t)
	a, ctx := f.app, context.Background()

	if !f.proxy.IsRemote() || a.IsIPCPrimary() {
		t.Fatal("precondition: app must start as a secondary with a remote proxy")
	}

	if err := a.PromoteToPrimary(ctx, f.lockFile); err != nil {
		t.Fatalf("PromoteToPrimary: %v", err)
	}

	// In place: the very same pool and proxy, now the local writer.
	if a.rwConn != f.sec {
		t.Fatal("promotion replaced the DB pool")
	}
	if got, ok := a.DBQuerier.(*dbproxy.DBProxy); !ok || got != f.proxy {
		t.Fatal("promotion replaced the querier")
	}
	if f.proxy.IsRemote() {
		t.Fatal("proxy still forwards after promotion")
	}
	if !a.IsIPCPrimary() {
		t.Fatal("app does not report the primary role after promotion")
	}

	// Pool: 8 connections, every one at the primary's 10 s busy_timeout.
	if got := f.sec.Stats().MaxOpenConnections; got != 8 {
		t.Fatalf("MaxOpenConnections = %d, want 8", got)
	}
	for i, v := range busyTimeoutOnEveryConn(t, f.sec, 8) {
		if v != 10000 {
			t.Errorf("connection %d busy_timeout = %d, want 10000", i, v)
		}
	}

	// Session / message writes through the services built before promotion.
	sess, err := a.Sessions.Create(ctx, "after promotion")
	if err != nil {
		t.Fatalf("session write after promotion: %v", err)
	}
	if _, err := a.Messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hello"}},
	}); err != nil {
		t.Fatalf("message write after promotion: %v", err)
	}

	// Remembrances writes must be direct: a forwarded write would hit the dead
	// primary and fail within this deadline.
	writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := f.kbStore.AddDocument(writeCtx, "notes/promoted.md", "written after promotion", nil); err != nil {
		t.Fatalf("KB add after promotion: %v", err)
	}
	if doc, err := f.kbStore.GetDocument(ctx, "notes/promoted.md"); err != nil || doc == nil {
		t.Fatalf("KB document not stored: doc=%v err=%v", doc, err)
	}
	if err := f.evStore.ReplaceSessionEvents(writeCtx, sess.ID, "session",
		map[string]interface{}{"session_id": sess.ID}, []string{"session chunk"}, [][]float32{{1, 2}}); err != nil {
		t.Fatalf("event replace after promotion: %v", err)
	}
	if err := f.evStore.ReplaceMessageEvents(writeCtx, sess.ID, "msg-1", "session",
		map[string]interface{}{"session_id": sess.ID, "message_id": "msg-1", "content_hash": "h1"},
		[]string{"message chunk"}, [][]float32{{3, 4}}); err != nil {
		t.Fatalf("message-event replace after promotion: %v", err)
	}

	// Another secondary forwards writes to the promoted primary over IPC: the
	// db.write handlers and the remembrances dispatcher must be registered.
	client2, err := ipc.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client2.Close() })
	proxy2 := dbproxy.New(db.New(f.sec), client2, fmt.Sprintf("tcp://127.0.0.1:%d", f.rpcPort))
	fwdCtx, cancelFwd := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFwd()
	if _, err := dbproxy.ProxyWriteWithResult[db.Session](fwdCtx, proxy2, "CreateSession",
		db.CreateSessionParams{ID: "forwarded-session", Title: "via promoted bus"}); err != nil {
		t.Fatalf("sqlc write forwarded to the promoted primary: %v", err)
	}
	ev2 := events.NewEventStore(f.sec, f.emb)
	ev2.SetWriteProxy(proxy2)
	if err := ev2.ReplaceMessageEvents(fwdCtx, sess.ID, "msg-2", "session",
		map[string]interface{}{"session_id": sess.ID, "message_id": "msg-2", "content_hash": "h2"},
		[]string{"forwarded chunk"}, [][]float32{{5, 6}}); err != nil {
		t.Fatalf("remembrances write forwarded to the promoted primary: %v", err)
	}
	markers, err := f.evStore.MessageEventMarkers(ctx, sess.ID, "session")
	if err != nil {
		t.Fatalf("MessageEventMarkers: %v", err)
	}
	if markers["msg-1"] != "h1" || markers["msg-2"] != "h2" {
		t.Fatalf("message markers = %v, want msg-1=h1 and msg-2=h2", markers)
	}
}

// TestPromoteToPrimaryWaitsForPortsToFree covers the graceful-handover timing
// seen in the two-process smoke test: the old primary releases the lock and
// announces its shutdown while its sockets are still bound for a moment.
// Promotion must wait for the ports instead of failing.
func TestPromoteToPrimaryWaitsForPortsToFree(t *testing.T) {
	f := newSecondaryFixture(t)
	occupied, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", f.pubPort))
	if err != nil {
		t.Fatalf("occupy PUB port: %v", err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = occupied.Close()
	}()

	if err := f.app.PromoteToPrimary(context.Background(), f.lockFile); err != nil {
		t.Fatalf("PromoteToPrimary with a briefly busy port: %v", err)
	}
	if !f.app.IsIPCPrimary() || f.proxy.IsRemote() {
		t.Fatal("not promoted after the port freed up")
	}
}

// TestPromoteToPrimaryRefusedAfterHandover verifies a promotion racing a
// shutdown is refused instead of resurrecting the primary role.
func TestPromoteToPrimaryRefusedAfterHandover(t *testing.T) {
	a := &App{}
	a.releasePrimaryRole()
	if err := a.PromoteToPrimary(context.Background(), nil); err == nil {
		t.Fatal("PromoteToPrimary succeeded after the handover ran")
	}
}

// TestPrimaryHandoverReleasesLockBeforeShutdownAnnouncement is the G5 ordering
// check: by the time a subscriber observes instance.shutdown, the lock must
// already be free (pre-fix, App.Shutdown announced first and the lock was only
// released by the runtime cleanup at the very end of process shutdown).
func TestPrimaryHandoverReleasesLockBeforeShutdownAnnouncement(t *testing.T) {
	resetIPCGlobals(t)
	dir := t.TempDir()
	conn, err := db.ConnectAt(filepath.Join(dir, "pando.db"))
	if err != nil {
		t.Fatalf("ConnectAt: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	pubPort, rpcPort, err := ipc.FindFreePorts()
	if err != nil {
		t.Fatal(err)
	}
	isPrimary, _, lockFile, err := ipc.AcquireLock(dir, "handover-test", pubPort, rpcPort)
	if err != nil || !isPrimary {
		t.Fatalf("AcquireLock: primary=%v err=%v", isPrimary, err)
	}

	ctx := context.Background()
	bus := ipcruntime.NewPrimaryBus("handover-test")
	coord := writecoordinator.New(ctx, db.New(conn), 16)
	dbproxy.RegisterHandlersWithCoordinator(bus, coord)
	if err := bus.Start(ctx, pubPort, rpcPort); err != nil {
		t.Fatalf("bus.Start: %v", err)
	}
	a := &App{rwConn: conn}
	a.SetupIPC(bus)
	a.SetIPCPrimaryHandover(coord, sync.OnceFunc(func() { ipc.ReleaseLock(lockFile) }))

	sub, err := ipc.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	ch, err := sub.SubscribeTo(fmt.Sprintf("tcp://127.0.0.1:%d", pubPort),
		protocol.TopicInstanceShutdown, protocol.TopicInstancePromoted)
	if err != nil {
		t.Fatal(err)
	}
	// Wait out ZMQ's slow-joiner window: publish probes until one arrives.
	joined := false
	for deadline := time.Now().Add(5 * time.Second); !joined && time.Now().Before(deadline); {
		_ = bus.Publish(protocol.TopicInstancePromoted, protocol.PromotedPayload{InstanceID: "probe"})
		select {
		case <-ch:
			joined = true
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !joined {
		t.Skip("subscriber never joined the PUB socket; cannot observe ordering")
	}

	lockFreeAtShutdown := make(chan bool, 1)
	go func() {
		for env := range ch {
			if env.Topic != protocol.TopicInstanceShutdown {
				continue
			}
			isPrimary, _, f, err := ipc.AcquireLock(dir, "observer", pubPort, rpcPort)
			if err == nil && isPrimary {
				ipc.ReleaseLock(f)
			}
			lockFreeAtShutdown <- err == nil && isPrimary
			return
		}
	}()

	a.releasePrimaryRole()

	select {
	case free := <-lockFreeAtShutdown:
		if !free {
			t.Fatal("instance.shutdown was observed while the IPC lock was still held")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("instance.shutdown never observed")
	}

	// The coordinator was drained and stopped, the bus is closed, and a second
	// handover is a no-op.
	params := []byte(`{"id":"late","title":"late"}`)
	if _, err := coord.Submit(ctx, dbproxy.WriteRequest{Method: "CreateSession", Params: params}); err == nil {
		t.Fatal("coordinator still accepts writes after the handover")
	}
	if err := bus.Publish(protocol.TopicInstanceHeartbeat, nil); !errors.Is(err, ipc.ErrBusClosed) {
		t.Fatalf("Publish after handover = %v, want ErrBusClosed", err)
	}
	a.releasePrimaryRole()
}
