package project

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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
	"github.com/digiogithub/pando/internal/tlsutil"
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
	prevFetchToken := fetchWebToken
	prevRegistry := newInstanceRegistry
	t.Cleanup(func() {
		spawnWebProcess = prevSpawn
		probeWebHealth = prevProbe
		fetchWebToken = prevFetchToken
		newInstanceRegistry = prevRegistry
	})
}

func newWebTestManager(t *testing.T) (*Manager, Service) {
	t.Helper()
	restoreWebHooks(t)
	conn := setupWebTestDB(t)
	svc := NewService(db.New(conn))
	mgr, err := NewManager(context.Background(), svc, ManagerOptions{
		ParentInstanceID: "test-parent",
		WebTLSDataDir:    t.TempDir(),
	})
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

func ensureTestCert(t *testing.T) tlsutil.CertPaths {
	t.Helper()
	paths, err := tlsutil.EnsureCert(t.TempDir())
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	return paths
}

func newPinnedLoopbackServer(t *testing.T, certPaths tlsutil.CertPaths, token string) (*httptest.Server, int) {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(certPaths.CertFile, certPaths.KeyFile)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(webTokenPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + token + `"}`))
	})

	server := httptest.NewUnstartedServer(mux)
	listener, err := net.Listen("tcp", net.JoinHostPort(webLoopbackHost, "0"))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	server.Listener = listener
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	port := listener.Addr().(*net.TCPAddr).Port
	return server, port
}

func TestDefaultSpawnWebProcessSetsPublicBaseEnv(t *testing.T) {
	projDir := t.TempDir()
	writeProjectConfig(t, projDir)

	envFile := filepath.Join(t.TempDir(), "env.txt")
	script := filepath.Join(t.TempDir(), "capture-env.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nenv >"+envFile+"\n"), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	started, err := defaultSpawnWebProcess(context.Background(), script, webProcessConfig{
		ParentInstanceID: "parent-1",
		TLSCertFile:      "cert.pem",
		TLSKeyFile:       "key.pem",
	}, Project{ID: "proj-123", Path: projDir}, 43123)
	if err != nil {
		t.Fatalf("defaultSpawnWebProcess: %v", err)
	}
	if err := started.cmd.Wait(); err != nil {
		t.Fatalf("wait helper script: %v", err)
	}

	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env capture: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "PANDO_PUBLIC_BASE=/api/v1/projects/proj-123/web") {
		t.Fatalf("missing PANDO_PUBLIC_BASE in env: %q", text)
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
	spawnWebProcess = func(_ context.Context, _ string, cfg webProcessConfig, _ Project, _ int) (*webProcessStart, error) {
		spawnCount++
		if cfg.ParentInstanceID != "test-parent" {
			t.Fatalf("ParentInstanceID = %q, want test-parent", cfg.ParentInstanceID)
		}
		if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
			t.Fatal("expected TLS certificate paths for child spawn")
		}
		return startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`), nil
	}
	probeWebHealth = func(context.Context, *http.Client, string) error { return nil }
	fetchWebToken = func(context.Context, *http.Client, string) (string, error) { return "child-token", nil }

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
	if inst1.State() != WebStateRunning {
		t.Fatalf("web state = %q, want %q", inst1.State(), WebStateRunning)
	}
	if inst1.APIToken() != "child-token" {
		t.Fatalf("child token = %q, want child-token", inst1.APIToken())
	}
	snapshot := inst1.Snapshot()
	if snapshot.Port == 0 || snapshot.PID == 0 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
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

	spawnWebProcess = func(_ context.Context, _ string, _ webProcessConfig, _ Project, _ int) (*webProcessStart, error) {
		return startShellCommand(t, `echo "boom from child" >&2; exit 3`), nil
	}
	probeWebHealth = func(context.Context, *http.Client, string) error { return errors.New("not ready") }

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

func TestOpenWebStopsIdleACPInstanceFirst(t *testing.T) {
	mgr, svc := newWebTestManager(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "web-stop-idle-acp", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	cancelled := false
	inst := &Instance{
		Project: Project{ID: proj.ID, Path: dir},
		cmd:     &exec.Cmd{},
		cancel:  func() { cancelled = true },
		errCh:   make(chan error, 1),
		slots:   newDelegationSlotsAt(time.Now()),
	}
	close(inst.errCh)
	mgr.instances[proj.ID] = inst

	spawnCount := 0
	spawnWebProcess = func(_ context.Context, _ string, _ webProcessConfig, _ Project, _ int) (*webProcessStart, error) {
		spawnCount++
		return startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`), nil
	}
	probeWebHealth = func(context.Context, *http.Client, string) error { return nil }
	fetchWebToken = func(context.Context, *http.Client, string) (string, error) { return "child-token", nil }

	if _, err := mgr.OpenWeb(ctx, proj.ID); err != nil {
		t.Fatalf("OpenWeb: %v", err)
	}
	if !cancelled {
		t.Fatal("idle ACP instance was not stopped before opening web child")
	}
	if spawnCount != 1 {
		t.Fatalf("spawn count = %d, want 1", spawnCount)
	}
	if _, ok := mgr.instances[proj.ID]; ok {
		t.Fatal("ACP instance still tracked after OpenWeb")
	}
}

func TestOpenWebRefusesWhenACPDelegationsInFlight(t *testing.T) {
	mgr, svc := newWebTestManager(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "web-busy-acp", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	inst := &Instance{
		Project: Project{ID: proj.ID, Path: dir},
		cmd:     &exec.Cmd{},
		cancel:  func() {},
		errCh:   make(chan error, 1),
		slots:   newDelegationSlotsAt(time.Now()),
	}
	close(inst.errCh)
	if !inst.acquireDelegationSlot(0) {
		t.Fatal("failed to acquire ACP delegation slot")
	}
	mgr.instances[proj.ID] = inst

	spawned := false
	spawnWebProcess = func(_ context.Context, _ string, _ webProcessConfig, _ Project, _ int) (*webProcessStart, error) {
		spawned = true
		return nil, errors.New("should not spawn")
	}

	_, err = mgr.OpenWeb(ctx, proj.ID)
	if !errors.Is(err, ErrDelegationsInFlight) {
		t.Fatalf("err = %v, want ErrDelegationsInFlight", err)
	}
	var inflightErr *DelegationsInFlightError
	if !errors.As(err, &inflightErr) || inflightErr.Count != 1 {
		t.Fatalf("err = %v, want DelegationsInFlightError{Count:1}", err)
	}
	if spawned {
		t.Fatal("web child was spawned despite in-flight ACP delegation")
	}
}

func TestRuntimeReportsManagerOwnedWebChild(t *testing.T) {
	proj := Project{ID: "proj-runtime-web", Path: t.TempDir()}
	cmd := startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`)
	t.Cleanup(func() {
		if cmd.cmd.Process != nil {
			_ = cmd.cmd.Process.Kill()
			_, _ = cmd.cmd.Process.Wait()
		}
	})

	m := &Manager{
		instances:    map[string]*Instance{},
		webInstances: map[string]*WebInstance{proj.ID: newTrackedWebInstance(proj, cmd.cmd.Process.Pid)},
	}

	running, external, pid := m.Runtime(proj.ID, proj.Path)
	if !running || external || pid != cmd.cmd.Process.Pid {
		t.Fatalf("Runtime = (%v,%v,%d), want (true,false,%d)", running, external, pid, cmd.cmd.Process.Pid)
	}
}

func TestActivateWithWebChildSkipsACPSpawn(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj := &Project{ID: "proj-activate-web", Path: dir}
	cmd := startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`)
	t.Cleanup(func() {
		if cmd.cmd.Process != nil {
			_ = cmd.cmd.Process.Kill()
			_, _ = cmd.cmd.Process.Wait()
		}
	})

	m := &Manager{
		service:      &stubService{proj: proj},
		instances:    map[string]*Instance{},
		webInstances: map[string]*WebInstance{proj.ID: newTrackedWebInstance(*proj, cmd.cmd.Process.Pid)},
		broker:       pubsub.NewBroker[ManagerEvent](),
		pandoBin:     filepath.Join(t.TempDir(), "missing-pando"),
	}
	t.Cleanup(m.broker.Shutdown)

	if err := m.Activate(context.Background(), proj.ID); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if m.ActiveID() != proj.ID {
		t.Fatalf("ActiveID = %q, want %q", m.ActiveID(), proj.ID)
	}
	if len(m.instances) != 0 {
		t.Fatalf("ACP instances = %d, want 0", len(m.instances))
	}
}

func TestNewManagerAdoptsOrphanWithPinnedTLSAndTokenHandshake(t *testing.T) {
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

	certPaths := ensureTestCert(t)
	_, port := newPinnedLoopbackServer(t, certPaths, "adopted-token")
	dummy := startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`)
	t.Cleanup(func() {
		_ = dummy.cmd.Process.Kill()
		_, _ = dummy.cmd.Process.Wait()
	})

	if err := svc.UpdateWebRuntime(ctx, proj.ID, dummy.cmd.Process.Pid, port); err != nil {
		t.Fatalf("UpdateWebRuntime: %v", err)
	}

	newInstanceRegistry = func() registryLister {
		return fakeRegistry{entries: []*instanceregistry.Entry{{
			InstanceID:       "child-1",
			Path:             proj.Path,
			PID:              dummy.cmd.Process.Pid,
			WebPort:          port,
			Mode:             instanceregistry.ModeWebUI,
			StartedAt:        time.Now(),
			ParentInstanceID: "dead-parent",
		}}}
	}

	mgr, err := NewManager(context.Background(), svc, ManagerOptions{
		ParentInstanceID: "new-parent",
		WebTLSCertFile:   certPaths.CertFile,
		WebTLSKeyFile:    certPaths.KeyFile,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Shutdown)

	inst, ok := mgr.WebInstance(proj.ID)
	if !ok || inst == nil {
		t.Fatal("expected adopted web instance")
	}
	if inst.Port() != port {
		t.Fatalf("adopted web port = %d, want %d", inst.Port(), port)
	}
	if inst.PID() != dummy.cmd.Process.Pid {
		t.Fatalf("adopted web pid = %d, want %d", inst.PID(), dummy.cmd.Process.Pid)
	}
	if inst.State() != WebStateRunning {
		t.Fatalf("adopted web state = %q, want %q", inst.State(), WebStateRunning)
	}
	if inst.APIToken() != "adopted-token" {
		t.Fatalf("adopted token = %q, want adopted-token", inst.APIToken())
	}

	got, err := svc.Get(ctx, proj.ID)
	if err != nil {
		t.Fatalf("Get after adoption: %v", err)
	}
	if got.WebPID != dummy.cmd.Process.Pid || got.WebPort != port {
		t.Fatalf("unexpected persisted runtime after adoption: pid=%d port=%d", got.WebPID, got.WebPort)
	}
}

func TestNewManagerRejectsChildWithDifferentCertificate(t *testing.T) {
	restoreWebHooks(t)
	conn := setupWebTestDB(t)
	svc := NewService(db.New(conn))
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "web-reject", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	parentCert := ensureTestCert(t)
	childCert := ensureTestCert(t)
	_, port := newPinnedLoopbackServer(t, childCert, "wrong-token")
	dummy := startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`)
	t.Cleanup(func() {
		_ = dummy.cmd.Process.Kill()
		_, _ = dummy.cmd.Process.Wait()
	})

	if err := svc.UpdateWebRuntime(ctx, proj.ID, dummy.cmd.Process.Pid, port); err != nil {
		t.Fatalf("UpdateWebRuntime: %v", err)
	}

	newInstanceRegistry = func() registryLister {
		return fakeRegistry{entries: []*instanceregistry.Entry{{
			InstanceID:       "child-1",
			Path:             proj.Path,
			PID:              dummy.cmd.Process.Pid,
			WebPort:          port,
			Mode:             instanceregistry.ModeWebUI,
			StartedAt:        time.Now(),
			ParentInstanceID: "dead-parent",
		}}}
	}

	mgr, err := NewManager(context.Background(), svc, ManagerOptions{
		ParentInstanceID: "new-parent",
		WebTLSCertFile:   parentCert.CertFile,
		WebTLSKeyFile:    parentCert.KeyFile,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Shutdown)

	if inst, ok := mgr.WebInstance(proj.ID); ok && inst != nil {
		t.Fatal("unexpected adoption of a child presenting another certificate")
	}

	got, err := svc.Get(ctx, proj.ID)
	if err != nil {
		t.Fatalf("Get after rejected adoption: %v", err)
	}
	if got.WebPID != 0 || got.WebPort != 0 {
		t.Fatalf("expected rejected adoption to clear runtime, got pid=%d port=%d", got.WebPID, got.WebPort)
	}
}

func TestNewManagerSkipsChildWhoseParentIsStillAlive(t *testing.T) {
	restoreWebHooks(t)
	conn := setupWebTestDB(t)
	svc := NewService(db.New(conn))
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "web-live-parent", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	certPaths := ensureTestCert(t)
	_, port := newPinnedLoopbackServer(t, certPaths, "adopted-token")
	dummy := startShellCommand(t, `trap 'exit 0' TERM INT; while :; do sleep 1; done`)
	t.Cleanup(func() {
		_ = dummy.cmd.Process.Kill()
		_, _ = dummy.cmd.Process.Wait()
	})

	if err := svc.UpdateWebRuntime(ctx, proj.ID, dummy.cmd.Process.Pid, port); err != nil {
		t.Fatalf("UpdateWebRuntime: %v", err)
	}

	newInstanceRegistry = func() registryLister {
		return fakeRegistry{entries: []*instanceregistry.Entry{
			{
				InstanceID: "live-parent",
				Path:       t.TempDir(),
				PID:        dummy.cmd.Process.Pid,
				Mode:       instanceregistry.ModeTUI,
				StartedAt:  time.Now(),
			},
			{
				InstanceID:       "child-1",
				Path:             proj.Path,
				PID:              dummy.cmd.Process.Pid,
				WebPort:          port,
				Mode:             instanceregistry.ModeWebUI,
				StartedAt:        time.Now(),
				ParentInstanceID: "live-parent",
			},
		}}
	}

	mgr, err := NewManager(context.Background(), svc, ManagerOptions{
		ParentInstanceID: "new-parent",
		WebTLSCertFile:   certPaths.CertFile,
		WebTLSKeyFile:    certPaths.KeyFile,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Shutdown)

	if inst, ok := mgr.WebInstance(proj.ID); ok && inst != nil {
		t.Fatal("unexpected adoption while the recorded parent instance is still alive")
	}

	got, err := svc.Get(ctx, proj.ID)
	if err != nil {
		t.Fatalf("Get after skipped adoption: %v", err)
	}
	if got.WebPID != 0 || got.WebPort != 0 {
		t.Fatalf("expected skipped adoption to clear runtime, got pid=%d port=%d", got.WebPID, got.WebPort)
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

	newInstanceRegistry = func() registryLister { return fakeRegistry{} }

	mgr, err := NewManager(context.Background(), svc, ManagerOptions{
		ParentInstanceID: "new-parent",
		WebTLSDataDir:    t.TempDir(),
	})
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
