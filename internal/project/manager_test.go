package project_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/project"
)

// newManager creates a Manager backed by an in-memory SQLite DB for testing.
func newManager(t *testing.T) (*project.Manager, project.Service) {
	t.Helper()
	conn := setupDB(t)
	svc := project.NewService(db.New(conn))
	mgr, err := project.NewManager(context.Background(), svc)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { mgr.Shutdown() })
	return mgr, svc
}

// TestManagerRegisterAndList registers a path and verifies it appears in List.
func TestManagerRegisterAndList(t *testing.T) {
	mgr, _ := newManager(t)
	ctx := context.Background()

	tmpDir := t.TempDir()

	p, err := mgr.Register(ctx, "test-project", tmpDir)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if p == nil {
		t.Fatal("Register returned nil project")
	}
	if p.Name != "test-project" {
		t.Fatalf("expected name %q, got %q", "test-project", p.Name)
	}

	// Resolve the real path for comparison (TempDir may return symlinks on macOS).
	realTmp, _ := filepath.EvalSymlinks(tmpDir)
	if p.Path != realTmp {
		t.Fatalf("expected path %q, got %q", realTmp, p.Path)
	}

	projects, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].ID != p.ID {
		t.Fatalf("List returned unexpected project ID")
	}
}

// TestManagerRegisterNonExistentPath expects an error when registering a path
// that does not exist on disk.
func TestManagerRegisterNonExistentPath(t *testing.T) {
	mgr, _ := newManager(t)
	ctx := context.Background()

	nonExistent := filepath.Join(os.TempDir(), "pando-test-nonexistent-xyz-12345")

	_, err := mgr.Register(ctx, "ghost", nonExistent)
	if err == nil {
		t.Fatal("expected error when registering nonexistent path, got nil")
	}
}

// TestManagerActivateNeedsInit activates a directory without .pando.toml and
// expects ErrProjectNeedsInit to be returned.
func TestManagerActivateNeedsInit(t *testing.T) {
	mgr, svc := newManager(t)
	ctx := context.Background()

	// Create a temp dir that has NO pando config.
	tmpDir := t.TempDir()

	p, err := svc.Create(ctx, "no-init", tmpDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = mgr.Activate(ctx, p.ID)
	if !errors.Is(err, project.ErrProjectNeedsInit) {
		t.Fatalf("expected ErrProjectNeedsInit, got: %v", err)
	}
}

// TestManagerCompleteInit calls CompleteInit on a temp dir and verifies that
// .pando.toml is created (i.e. HasConfigFileAt returns true).
func TestManagerCompleteInit(t *testing.T) {
	mgr, svc := newManager(t)
	ctx := context.Background()

	tmpDir := t.TempDir()

	p, err := svc.Create(ctx, "to-init", tmpDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// CompleteInit creates the config and then calls Activate, which tries to
	// spawn a child pando process. We only care that the config file was written
	// before the spawn is attempted. CompleteInit may return an error from
	// Activate (e.g. executable not found in tests) — we accept that.
	// What matters is that HasConfigFileAt returns true afterward.
	_ = mgr.CompleteInit(ctx, p.ID)

	if !config.HasConfigFileAt(tmpDir) {
		t.Fatalf("expected .pando.toml to exist in %s after CompleteInit", tmpDir)
	}
}

// TestManagerUnregister registers a project, then unregisters it and verifies
// it is no longer returned by List.
func TestManagerUnregister(t *testing.T) {
	mgr, _ := newManager(t)
	ctx := context.Background()

	tmpDir := t.TempDir()

	p, err := mgr.Register(ctx, "to-remove", tmpDir)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := mgr.Unregister(ctx, p.ID); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	projects, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List after Unregister: %v", err)
	}
	for _, proj := range projects {
		if proj.ID == p.ID {
			t.Fatalf("project %s still in list after Unregister", p.ID)
		}
	}
}
