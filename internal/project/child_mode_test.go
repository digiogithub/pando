package project

import (
	"context"
	"errors"
	"testing"

	"github.com/digiogithub/pando/internal/db"
)

func newChildModeManager(t *testing.T) (*Manager, Service) {
	t.Helper()
	restoreWebHooks(t)
	conn := setupWebTestDB(t)
	svc := NewService(db.New(conn))
	mgr, err := NewManager(context.Background(), svc, ManagerOptions{
		ParentInstanceID: "parent-instance",
		SpawnDisabled:    true,
		WebTLSDataDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Shutdown)
	return mgr, svc
}

func TestChildModeManagerRefusesNestedSpawns(t *testing.T) {
	mgr, svc := newChildModeManager(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	proj, err := svc.Create(ctx, "child-workspace", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.Activate(ctx, proj.ID); !errors.Is(err, ErrChildInstance) {
		t.Fatalf("Activate error = %v, want %v", err, ErrChildInstance)
	}

	if _, err := mgr.EnsureInstance(ctx, proj.ID, true); !errors.Is(err, ErrChildInstance) {
		t.Fatalf("EnsureInstance error = %v, want %v", err, ErrChildInstance)
	}

	if _, err := mgr.OpenWeb(ctx, proj.ID); !errors.Is(err, ErrChildInstance) {
		t.Fatalf("OpenWeb error = %v, want %v", err, ErrChildInstance)
	}

	if err := mgr.CompleteInit(ctx, proj.ID); !errors.Is(err, ErrChildInstance) {
		t.Fatalf("CompleteInit error = %v, want %v", err, ErrChildInstance)
	}
}
