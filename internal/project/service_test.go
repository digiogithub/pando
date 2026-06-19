package project_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/project"
)

// schema is the minimal DDL needed for project tests without running Goose.
const schema = `
CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    path        TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL DEFAULT 'stopped',
    initialized INTEGER NOT NULL DEFAULT 0,
    acp_pid     INTEGER,
    acp_port    INTEGER,
    last_opened INTEGER,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
`

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	if _, err := conn.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestCreateAndGet(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	wd, _ := os.Getwd()
	p, err := svc.Create(ctx, "my-project", wd)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if p.Status != project.StatusStopped {
		t.Fatalf("expected status %q, got %q", project.StatusStopped, p.Status)
	}
	if p.Initialized {
		t.Fatal("expected initialized=false on creation")
	}

	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("Get ID mismatch: want %q, got %q", p.ID, got.ID)
	}
	if got.Name != "my-project" {
		t.Fatalf("Get Name mismatch: want %q, got %q", "my-project", got.Name)
	}
}

func TestCreateDefaultName(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	wd, _ := os.Getwd()
	p, err := svc.Create(ctx, "", wd)
	if err != nil {
		t.Fatalf("Create with empty name: %v", err)
	}
	// Name should default to the last path component.
	if p.Name == "" {
		t.Fatal("expected a non-empty default name")
	}
}

func TestGetByPath(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	wd, _ := os.Getwd()
	p, err := svc.Create(ctx, "test", wd)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := svc.GetByPath(ctx, wd)
	if err != nil {
		t.Fatalf("GetByPath: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("GetByPath ID mismatch: want %q, got %q", p.ID, got.ID)
	}
}

// TestGetByPathResolvesSymlink verifies that a directory reachable both via a
// symlink and via its resolved target maps to the same project entry, so it is
// never registered twice.
func TestGetByPathResolvesSymlink(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	// Real target directory.
	target := t.TempDir()
	// A symlink that points at the target's parent, so the project path is
	// reachable through the link as well.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// Register using the resolved (real) path.
	p, err := svc.Create(ctx, "viaTarget", target)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Looking it up through the symlink must return the same project.
	got, err := svc.GetByPath(ctx, link)
	if err != nil {
		t.Fatalf("GetByPath via symlink: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("symlink path resolved to a different project: want %q, got %q", p.ID, got.ID)
	}

	// Creating again through the symlink must collide on the unique path
	// constraint (i.e. it is treated as the same directory), not create a
	// second row.
	if _, err := svc.Create(ctx, "viaLink", link); err == nil {
		t.Fatal("expected duplicate Create via symlink to fail on unique path constraint")
	}

	projects, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected exactly 1 project after symlink+target registration, got %d", len(projects))
	}
}

func TestList(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	dirs := []string{t.TempDir(), t.TempDir()}
	for _, d := range dirs {
		if _, err := svc.Create(ctx, "", d); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	projects, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestUpdateStatus(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	wd, _ := os.Getwd()
	p, err := svc.Create(ctx, "test", wd)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.UpdateStatus(ctx, p.ID, project.StatusRunning, 1234, 9000); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get after UpdateStatus: %v", err)
	}
	if got.Status != project.StatusRunning {
		t.Fatalf("expected status %q, got %q", project.StatusRunning, got.Status)
	}
	if got.ACPPID != 1234 {
		t.Fatalf("expected ACPPID 1234, got %d", got.ACPPID)
	}
	if got.ACPPort != 9000 {
		t.Fatalf("expected ACPPort 9000, got %d", got.ACPPort)
	}
}

func TestMarkInitialized(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	wd, _ := os.Getwd()
	p, err := svc.Create(ctx, "test", wd)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.MarkInitialized(ctx, p.ID); err != nil {
		t.Fatalf("MarkInitialized: %v", err)
	}

	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get after MarkInitialized: %v", err)
	}
	if !got.Initialized {
		t.Fatal("expected initialized=true")
	}
}

func TestTouchLastOpened(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	wd, _ := os.Getwd()
	p, err := svc.Create(ctx, "test", wd)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.LastOpened != nil {
		t.Fatal("expected LastOpened to be nil initially")
	}

	if err := svc.TouchLastOpened(ctx, p.ID); err != nil {
		t.Fatalf("TouchLastOpened: %v", err)
	}

	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get after TouchLastOpened: %v", err)
	}
	if got.LastOpened == nil {
		t.Fatal("expected LastOpened to be set")
	}
}

func TestDelete(t *testing.T) {
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	ctx := context.Background()

	wd, _ := os.Getwd()
	p, err := svc.Create(ctx, "test", wd)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = svc.Get(ctx, p.ID)
	if err == nil {
		t.Fatal("expected error after Delete, got nil")
	}
}
