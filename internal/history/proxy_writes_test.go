package history_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/ipc/dbproxy/proxytest"
)

func newSession(t *testing.T, tp *proxytest.Topology) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := db.New(tp.PrimaryDB).CreateSession(context.Background(), db.CreateSessionParams{
		ID: id, Title: "p5 history",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return id
}

// File history is the hottest direct writer (one row per file edit). On an
// IPC secondary it now writes through the DBProxy, so a version created while
// the primary holds the write lock is forwarded instead of failing on the
// secondary's 200 ms busy timeout.
func TestHistoryWritesThroughProxyOnSecondary(t *testing.T) {
	tp := proxytest.New(t)
	ctx := context.Background()
	sessionID := newSession(t, tp)
	svc := history.NewService(tp.Proxy)

	tp.HoldWriteLock(t, 700*time.Millisecond)
	started := time.Now()
	f, err := svc.Create(ctx, sessionID, "main.go", "v0")
	if err != nil {
		t.Fatalf("Create under primary lock: %v", err)
	}
	if time.Since(started) < 500*time.Millisecond {
		t.Fatalf("Create returned in %s while the lock was held (not forwarded?)", time.Since(started))
	}
	if f.Version != history.InitialVersion {
		t.Fatalf("version = %q", f.Version)
	}

	tp.HoldWriteLock(t, 500*time.Millisecond)
	v1, err := svc.CreateVersion(ctx, sessionID, "main.go", "v1")
	if err != nil {
		t.Fatalf("CreateVersion under primary lock: %v", err)
	}
	if v1.Version != "v1" {
		t.Fatalf("version = %q, want v1", v1.Version)
	}

	tp.HoldWriteLock(t, 400*time.Millisecond)
	if _, err := svc.Update(ctx, history.File{ID: v1.ID, Content: "v1b", Version: v1.Version}); err != nil {
		t.Fatalf("Update under primary lock: %v", err)
	}

	files, err := svc.ListBySession(ctx, sessionID)
	if err != nil || len(files) != 2 {
		t.Fatalf("ListBySession = %d files, err=%v", len(files), err)
	}

	tp.HoldWriteLock(t, 400*time.Millisecond)
	if err := svc.DeleteSessionFiles(ctx, sessionID); err != nil {
		t.Fatalf("DeleteSessionFiles under primary lock: %v", err)
	}
	if files, err = svc.ListBySession(ctx, sessionID); err != nil || len(files) != 0 {
		t.Fatalf("files after delete = %d, err=%v", len(files), err)
	}
}

// The primary path is unchanged: the same service on a direct querier still
// versions files and enforces the per-path uniqueness retry.
func TestHistoryDirectOnPrimary(t *testing.T) {
	tp := proxytest.New(t)
	ctx := context.Background()
	sessionID := newSession(t, tp)
	svc := history.NewService(db.New(tp.PrimaryDB))

	if _, err := svc.Create(ctx, sessionID, "a.go", "0"); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"v1", "v2", "v3"} {
		got, err := svc.CreateVersion(ctx, sessionID, "a.go", "content")
		if err != nil {
			t.Fatalf("version %d: %v", i, err)
		}
		if got.Version != want {
			t.Fatalf("version %d = %q, want %q", i, got.Version, want)
		}
	}
	all, err := svc.ListBySession(ctx, sessionID)
	if err != nil || len(all) != 4 {
		t.Fatalf("ListBySession = %d rows err=%v", len(all), err)
	}
	versions := make(map[string]bool, len(all))
	for _, f := range all {
		versions[f.Version] = true
	}
	for _, want := range []string{history.InitialVersion, "v1", "v2", "v3"} {
		if !versions[want] {
			t.Fatalf("version %q missing from %v", want, versions)
		}
	}
}
