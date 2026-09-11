package runtime

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/ipc"
)

// loadIsolatedConfig loads a fresh config for a throwaway project with an
// isolated $HOME, so Bootstrap's db.Connect writes only under t.TempDir().
func loadIsolatedConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	project := t.TempDir()
	t.Chdir(project)
	config.ResetForTests()
	t.Cleanup(config.ResetForTests)
	if _, err := config.Load(project, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return project
}

// TestBootstrapPrimaryReleaseLockAndCleanupAreIdempotent covers the primary's
// early lock release (used by the ordered handover) and a Cleanup that may run
// after it, possibly twice.
func TestBootstrapPrimaryReleaseLockAndCleanupAreIdempotent(t *testing.T) {
	project := loadIsolatedConfig(t)

	rt, err := Bootstrap(context.Background(), project, "cleanup-test")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if rt.Role != RolePrimary {
		t.Fatalf("role = %s, want primary", rt.Role)
	}
	lockHeld := func() bool {
		isPrimary, _, f, err := ipc.AcquireLock(project, "observer", rt.PubPort, rt.RPCPort)
		if err != nil {
			t.Fatalf("AcquireLock: %v", err)
		}
		if isPrimary {
			ipc.ReleaseLock(f)
		}
		return !isPrimary
	}
	if !lockHeld() {
		t.Fatal("primary does not hold the lock after Bootstrap")
	}

	rt.ReleaseLock()
	if lockHeld() {
		t.Fatal("lock still held after ReleaseLock")
	}
	rt.ReleaseLock() // idempotent

	rt.Cleanup()
	rt.Cleanup() // idempotent: must not panic or double-close

	if err := rt.SQLDB.Ping(); err == nil {
		t.Fatal("DB still open after Cleanup")
	}
	// The bus was never started here (entrypoints start it), so Publish must
	// fail either as "not started" or as closed — never succeed.
	if err := rt.Bus.Publish("x", nil); err == nil {
		t.Fatal("Publish succeeded after Cleanup")
	}
}
