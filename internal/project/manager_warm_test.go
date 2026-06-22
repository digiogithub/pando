package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/digiogithub/pando/internal/project"
)

// TestEnsureInstanceReuseOnlyNotRunning verifies that with auto-start disabled and
// nothing running, EnsureInstance returns ErrInstanceNotRunning (cold path).
func TestEnsureInstanceReuseOnlyNotRunning(t *testing.T) {
	mgr, _ := newManager(t)
	ctx := context.Background()

	p, err := mgr.Register(ctx, "warm-reuse-only", t.TempDir())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = mgr.EnsureInstance(ctx, p.ID, false /*autoStart*/)
	if !errors.Is(err, project.ErrInstanceNotRunning) {
		t.Fatalf("err = %v, want ErrInstanceNotRunning", err)
	}
}

// TestEnsureInstanceAutoStartNoConfig verifies that auto-start on a project path
// without a Pando configuration file returns ErrProjectNeedsInit rather than
// spawning a child that would fail to initialize.
func TestEnsureInstanceAutoStartNoConfig(t *testing.T) {
	mgr, _ := newManager(t)
	ctx := context.Background()

	p, err := mgr.Register(ctx, "warm-autostart", t.TempDir())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = mgr.EnsureInstance(ctx, p.ID, true /*autoStart*/)
	if !errors.Is(err, project.ErrProjectNeedsInit) {
		t.Fatalf("err = %v, want ErrProjectNeedsInit", err)
	}
}

// TestWarmDelegateUnregisteredProject verifies that an empty project id with an
// unregistered path resolves to ErrProjectNotRegistered (cold path).
func TestWarmDelegateUnregisteredProject(t *testing.T) {
	mgr, _ := newManager(t)
	ctx := context.Background()

	_, err := mgr.WarmDelegate(ctx, "", t.TempDir(), "prompt", true, 4)
	if !errors.Is(err, project.ErrProjectNotRegistered) {
		t.Fatalf("err = %v, want ErrProjectNotRegistered", err)
	}
}
