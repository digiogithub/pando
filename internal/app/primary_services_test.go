package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// serviceRecorder registers counting primary-only services on an App and
// records the context each one was started with.
type serviceRecorder struct {
	mu    sync.Mutex
	calls map[string]int
	ctxs  map[string]context.Context
}

func newServiceRecorder(a *App, names ...string) *serviceRecorder {
	r := &serviceRecorder{calls: map[string]int{}, ctxs: map[string]context.Context{}}
	for _, name := range names {
		a.registerPrimaryService(name, func(ctx context.Context) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.calls[name]++
			r.ctxs[name] = ctx
		})
	}
	return r
}

func (r *serviceRecorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[name]
}

func (r *serviceRecorder) ctx(name string) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctxs[name]
}

func (r *serviceRecorder) assertCounts(t *testing.T, want int, names ...string) {
	t.Helper()
	for _, name := range names {
		if got := r.count(name); got != want {
			t.Errorf("service %q started %d times, want %d", name, got, want)
		}
	}
}

func TestResolveIPCRole(t *testing.T) {
	client, err := ipc.NewClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	remote := dbproxy.New(db.New(nil), client, deadPrimaryRPC)
	promoted := dbproxy.New(db.New(nil), client, deadPrimaryRPC)
	promoted.Promote()

	cases := []struct {
		name string
		opt  AppOptions
		want ipcruntime.Role
	}{
		{"no IPC", AppOptions{}, ipcruntime.RolePrimary},
		{"direct querier", AppOptions{DBQuerier: db.New(nil)}, ipcruntime.RolePrimary},
		{"remote proxy", AppOptions{DBQuerier: remote}, ipcruntime.RoleSecondary},
		{"promoted proxy", AppOptions{DBQuerier: promoted}, ipcruntime.RolePrimary},
		{"explicit primary wins", AppOptions{DBQuerier: remote, IPCRole: ipcruntime.RolePrimary}, ipcruntime.RolePrimary},
		{"explicit secondary wins", AppOptions{IPCRole: ipcruntime.RoleSecondary}, ipcruntime.RoleSecondary},
		{"unknown role falls back", AppOptions{DBQuerier: remote, IPCRole: "bogus"}, ipcruntime.RoleSecondary},
	}
	for _, tc := range cases {
		if got := resolveIPCRole(tc.opt); got != tc.want {
			t.Errorf("%s: resolveIPCRole = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSecondaryRoleStartsNoPrimaryServices(t *testing.T) {
	a := &App{lifetimeCtx: context.Background()}
	rec := newServiceRecorder(a, "code-index-watcher", "kb-watch", "cron")

	a.applyStartupIPCRole(ipcruntime.RoleSecondary, "test")

	rec.assertCounts(t, 0, "code-index-watcher", "kb-watch", "cron")
}

func TestPrimaryRoleStartsPrimaryServicesOnce(t *testing.T) {
	a := &App{lifetimeCtx: context.Background()}
	rec := newServiceRecorder(a, "code-index-watcher", "kb-watch", "cron")

	a.applyStartupIPCRole(ipcruntime.RolePrimary, "test")
	rec.assertCounts(t, 1, "code-index-watcher", "kb-watch", "cron")

	// A spurious later promotion of an instance that already runs them.
	if a.startPrimaryServices("promotion") {
		t.Error("second startPrimaryServices reported that it started the services")
	}
	rec.assertCounts(t, 1, "code-index-watcher", "kb-watch", "cron")
}

// TestPromoteToPrimaryStartsPrimaryServicesExactlyOnce drives a real in-place
// promotion of a secondary: the services it deferred at startup start exactly
// once, on the app's lifetime context (not the watcher's promote context), and
// a second call is a no-op.
func TestPromoteToPrimaryStartsPrimaryServicesExactlyOnce(t *testing.T) {
	f := newSecondaryFixture(t)
	lifeCtx, cancelLife := context.WithCancel(context.Background())
	defer cancelLife()
	f.app.lifetimeCtx = lifeCtx
	rec := newServiceRecorder(f.app, "code-index-watcher", "cron")

	f.app.applyStartupIPCRole(ipcruntime.RoleSecondary, "test")
	rec.assertCounts(t, 0, "code-index-watcher", "cron")

	promoteCtx, cancelPromote := context.WithCancel(context.Background())
	if err := f.app.PromoteToPrimary(promoteCtx, f.lockFile); err != nil {
		t.Fatalf("PromoteToPrimary: %v", err)
	}
	if !f.app.IsIPCPrimary() {
		t.Fatal("app is not primary after promotion")
	}
	rec.assertCounts(t, 1, "code-index-watcher", "cron")

	// The watcher's context ending must not stop the services.
	cancelPromote()
	if err := rec.ctx("cron").Err(); err != nil {
		t.Fatalf("service context ended with the promote context: %v", err)
	}

	if f.app.startPrimaryServices("promotion") {
		t.Error("second startPrimaryServices reported that it started the services")
	}
	rec.assertCounts(t, 1, "code-index-watcher", "cron")

	// They end with the app's lifetime.
	cancelLife()
	if rec.ctx("cron").Err() == nil {
		t.Fatal("service context did not end with the app lifetime context")
	}
}

func TestPrimaryServicesNotStartedOnceShutdownBegan(t *testing.T) {
	a := &App{lifetimeCtx: context.Background()}
	rec := newServiceRecorder(a, "cron")

	a.closePrimaryServices()
	if a.startPrimaryServices("promotion") {
		t.Error("startPrimaryServices started services after closePrimaryServices")
	}
	rec.assertCounts(t, 0, "cron")
}

func TestStartPrimaryServicesConcurrentCallsStartOnce(t *testing.T) {
	a := &App{lifetimeCtx: context.Background()}
	rec := newServiceRecorder(a, "cron", "memory-gc")

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.startPrimaryServices("promotion")
		}()
	}
	wg.Wait()
	rec.assertCounts(t, 1, "cron", "memory-gc")
}

// TestKBWatchIsPrimaryOnly checks a real registration path: the KB directory
// watcher is only registered by initRemembrancesKBSync (nothing is spawned for
// a secondary), starts on promotion, and is stopped through the same
// watcherCancelFuncs/watcherWG Shutdown uses.
func TestKBWatchIsPrimaryOnly(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.ConnectAt(filepath.Join(dir, "pando.db"))
	if err != nil {
		t.Fatalf("ConnectAt: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	kbDir := filepath.Join(dir, "kb")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := &rag.RemembrancesService{KB: kb.NewKBStore(conn, &recordingEmbedder{}, 1000, 100)}

	lifeCtx, cancelLife := context.WithCancel(context.Background())
	defer cancelLife()
	a := &App{lifetimeCtx: lifeCtx}
	a.initRemembrancesKBSync(svc, &config.RemembrancesConfig{KBPath: kbDir, KBWatch: true})

	a.primarySvcMu.Lock()
	names := a.primaryServiceNamesLocked()
	a.primarySvcMu.Unlock()
	if !slices.Equal(names, []string{"kb-watch"}) {
		t.Fatalf("registered services = %v, want [kb-watch]", names)
	}

	a.applyStartupIPCRole(ipcruntime.RoleSecondary, "test")
	if n := len(a.watcherCancelFuncs); n != 0 {
		t.Fatalf("secondary spawned %d background goroutines, want 0", n)
	}

	if !a.startPrimaryServices("promotion") {
		t.Fatal("promotion did not start the primary-only services")
	}
	a.cancelFuncsMutex.Lock()
	n := len(a.watcherCancelFuncs)
	a.cancelFuncsMutex.Unlock()
	if n != 1 {
		t.Fatalf("promotion spawned %d background goroutines, want 1", n)
	}

	// Stop it exactly like Shutdown does.
	a.closePrimaryServices()
	a.cancelFuncsMutex.Lock()
	for _, cancel := range a.watcherCancelFuncs {
		cancel()
	}
	a.cancelFuncsMutex.Unlock()
	done := make(chan struct{})
	go func() {
		a.watcherWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("KB watcher did not stop after its context was cancelled")
	}
}
