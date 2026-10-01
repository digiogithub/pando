package project

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/pubsub"
)

const webTestSchema = `
CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    path        TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL DEFAULT 'stopped',
    initialized INTEGER NOT NULL DEFAULT 0,
    acp_pid     INTEGER,
    acp_port    INTEGER,
    web_pid     INTEGER,
    web_port    INTEGER NOT NULL DEFAULT 0,
    last_opened INTEGER,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
`

type fakeRegistry struct {
	entries []*instanceregistry.Entry
	err     error
}

func (f fakeRegistry) List() ([]*instanceregistry.Entry, error) {
	return f.entries, f.err
}

func setupWebTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	if _, err := conn.Exec(webTestSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func writeProjectConfig(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte("[Mesnada]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func restoreWebHooks(t *testing.T) {
	t.Helper()
	prevSpawn := spawnWebProcess
	prevProbe := probeWebHealth
	prevRegistry := newInstanceRegistry
	t.Cleanup(func() {
		spawnWebProcess = prevSpawn
		probeWebHealth = prevProbe
		newInstanceRegistry = prevRegistry
	})
}

func newWebTestManager(t *testing.T) (*Manager, Service) {
	t.Helper()
	restoreWebHooks(t)
	conn := setupWebTestDB(t)
	svc := NewService(db.New(conn))
	mgr, err := NewManager(context.Background(), svc)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Shutdown)
	return mgr, svc
}

func waitManagerEvent(t *testing.T, ch <-chan pubsub.Event[ManagerEvent], want ManagerEventType) ManagerEvent {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case evt := <-ch:
			if evt.Payload.Type == want {
				return evt.Payload
			}
		case <-timeout:
			t.Fatalf("timed out waiting for manager event %q", want)
		}
	}
}

func startShellCommand(t *testing.T, script string) *webProcessStart {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	stdout := newTailBuffer(webOutputBufferLimit)
	stderr := newTailBuffer(webOutputBufferLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shell command: %v", err)
	}
	return &webProcessStart{
		cmd:    cmd,
		cancel: func() {},
		stdout: stdout,
		stderr: stderr,
	}
}

func TestOpenWebReuseAndClose(t *testing.T) {
	mgr, svc := newWebTestManager(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "web-project", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	spawnCount := 0
	spawnWebProcess = func(_ context.Context, _ string, _ string, _ Project, _ int) (*webProcessStart, error) {
		spawnCount++
		return startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`), nil
	}
	probeWebHealth = func(context.Context, int) error { return nil }

	events := mgr.Subscribe(context.Background())

	inst1, err := mgr.OpenWeb(ctx, proj.ID)
	if err != nil {
		t.Fatalf("OpenWeb first call: %v", err)
	}
	inst2, err := mgr.OpenWeb(ctx, proj.ID)
	if err != nil {
		t.Fatalf("OpenWeb second call: %v", err)
	}

	if spawnCount != 1 {
		t.Fatalf("spawn count = %d, want 1", spawnCount)
	}
	if inst1 != inst2 {
		t.Fatal("OpenWeb did not reuse the existing instance")
	}
	if inst1.State != WebStateRunning {
		t.Fatalf("web state = %q, want %q", inst1.State, WebStateRunning)
	}

	got, err := svc.Get(ctx, proj.ID)
	if err != nil {
		t.Fatalf("Get after OpenWeb: %v", err)
	}
	if got.WebPID == 0 {
		t.Fatal("expected persisted WebPID to be non-zero")
	}
	if got.WebPort == 0 {
		t.Fatal("expected persisted WebPort to be non-zero")
	}

	if err := mgr.CloseWeb(ctx, proj.ID); err != nil {
		t.Fatalf("CloseWeb: %v", err)
	}

	evt := waitManagerEvent(t, events, EvWebStopped)
	if evt.ProjectID != proj.ID {
		t.Fatalf("EvWebStopped project = %q, want %q", evt.ProjectID, proj.ID)
	}

	got, err = svc.Get(ctx, proj.ID)
	if err != nil {
		t.Fatalf("Get after CloseWeb: %v", err)
	}
	if got.WebPID != 0 || got.WebPort != 0 {
		t.Fatalf("expected cleared web runtime after CloseWeb, got pid=%d port=%d", got.WebPID, got.WebPort)
	}
}

func TestOpenWebCrashPublishesError(t *testing.T) {
	mgr, svc := newWebTestManager(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "web-crash", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	spawnWebProcess = func(_ context.Context, _ string, _ string, _ Project, _ int) (*webProcessStart, error) {
		return startShellCommand(t, `echo "boom from child" >&2; exit 3`), nil
	}
	probeWebHealth = func(context.Context, int) error { return errors.New("not ready") }

	events := mgr.Subscribe(context.Background())

	if _, err := mgr.OpenWeb(ctx, proj.ID); err == nil {
		t.Fatal("OpenWeb succeeded for a crashing child, want error")
	}

	evt := waitManagerEvent(t, events, EvWebError)
	if evt.ProjectID != proj.ID {
		t.Fatalf("EvWebError project = %q, want %q", evt.ProjectID, proj.ID)
	}
	if !strings.Contains(evt.Error, "boom from child") {
		t.Fatalf("EvWebError did not include stderr tail: %q", evt.Error)
	}

	got, err := svc.Get(ctx, proj.ID)
	if err != nil {
		t.Fatalf("Get after crash: %v", err)
	}
	if got.WebPID != 0 || got.WebPort != 0 {
		t.Fatalf("expected cleared web runtime after crash, got pid=%d port=%d", got.WebPID, got.WebPort)
	}
}

func TestNewManagerAdoptsRunningWebChild(t *testing.T) {
	restoreWebHooks(t)
	conn := setupWebTestDB(t)
	svc := NewService(db.New(conn))
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "web-adopt", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	dummy := startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`)
	t.Cleanup(func() {
		_ = dummy.cmd.Process.Kill()
		_, _ = dummy.cmd.Process.Wait()
	})

	if err := svc.UpdateWebRuntime(ctx, proj.ID, dummy.cmd.Process.Pid, 9443); err != nil {
		t.Fatalf("UpdateWebRuntime: %v", err)
	}

	probeWebHealth = func(_ context.Context, port int) error {
		if port != 9443 {
			t.Fatalf("probeWebHealth port = %d, want 9443", port)
		}
		return nil
	}
	newInstanceRegistry = func() registryLister {
		return fakeRegistry{entries: []*instanceregistry.Entry{{
			InstanceID: "child-1",
			Path:       proj.Path,
			PID:        dummy.cmd.Process.Pid,
			Mode:       instanceregistry.ModeWebUI,
			StartedAt:  time.Now(),
		}}}
	}

	mgr, err := NewManager(context.Background(), svc)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Shutdown)

	inst, ok := mgr.WebInstance(proj.ID)
	if !ok || inst == nil {
		t.Fatal("expected adopted web instance")
	}
	if inst.Port != 9443 {
		t.Fatalf("adopted web port = %d, want 9443", inst.Port)
	}
	if inst.PID != dummy.cmd.Process.Pid {
		t.Fatalf("adopted web pid = %d, want %d", inst.PID, dummy.cmd.Process.Pid)
	}
	if inst.State != WebStateRunning {
		t.Fatalf("adopted web state = %q, want %q", inst.State, WebStateRunning)
	}
}

func TestNewManagerClearsStaleWebRuntime(t *testing.T) {
	restoreWebHooks(t)
	conn := setupWebTestDB(t)
	svc := NewService(db.New(conn))
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "web-stale", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.UpdateWebRuntime(ctx, proj.ID, 12345, 9555); err != nil {
		t.Fatalf("UpdateWebRuntime: %v", err)
	}

	probeWebHealth = func(context.Context, int) error { return errors.New("stale") }
	newInstanceRegistry = func() registryLister { return fakeRegistry{} }

	mgr, err := NewManager(context.Background(), svc)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Shutdown)

	got, err := svc.Get(ctx, proj.ID)
	if err != nil {
		t.Fatalf("Get after startup cleanup: %v", err)
	}
	if got.WebPID != 0 || got.WebPort != 0 {
		t.Fatalf("expected stale web runtime to be cleared, got pid=%d port=%d", got.WebPID, got.WebPort)
	}
}
