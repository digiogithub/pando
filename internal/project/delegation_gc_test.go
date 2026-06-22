package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/pubsub"
)

// newIdleTestInstance builds an in-memory Instance suitable for idle-GC teardown
// (no real process): a non-nil cmd with a nil Process (so SIGTERM/Kill guards are
// skipped), a closed errCh so the teardown drain returns immediately, and a
// cancel func that records invocation.
func newIdleTestInstance(id string, spawned bool, idleSince time.Time, inflight int, cancelled *bool) *Instance {
	inst := &Instance{
		Project:      Project{ID: id, Path: "/tmp"},
		cmd:          &exec.Cmd{},
		cancel:       func() { *cancelled = true },
		errCh:        make(chan error, 1),
		lastActiveAt: idleSince,
	}
	close(inst.errCh)
	inst.delegationSpawned = spawned
	inst.inflight = inflight
	return inst
}

// TestGCIdleStopsIdleSpawnedInstance verifies the janitor stops a
// delegation-spawned, idle, non-active instance with no in-flight sessions.
func TestGCIdleStopsIdleSpawnedInstance(t *testing.T) {
	const id = "proj-idle"
	cancelled := false
	inst := newIdleTestInstance(id, true, time.Now().Add(-time.Hour), 0, &cancelled)

	m := &Manager{
		instances: map[string]*Instance{id: inst},
		broker:    pubsub.NewBroker[ManagerEvent](),
	}
	t.Cleanup(m.broker.Shutdown)

	stopped := m.gcIdleInstances(time.Minute)
	if len(stopped) != 1 || stopped[0] != id {
		t.Fatalf("stopped = %v, want [%s]", stopped, id)
	}
	if !cancelled {
		t.Error("instance cancel func was not called")
	}
	if _, ok := m.instances[id]; ok {
		t.Error("instance still present after GC, want removed")
	}
}

// TestGCIdleSkipsProtectedInstances verifies the janitor never touches
// user-activated, active, busy (in-flight), or freshly-active instances.
func TestGCIdleSkipsProtectedInstances(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	c := false

	user := newIdleTestInstance("user", false /*spawned*/, old, 0, &c)
	active := newIdleTestInstance("active", true, old, 0, &c)
	busy := newIdleTestInstance("busy", true, old, 1 /*inflight*/, &c)
	fresh := newIdleTestInstance("fresh", true, time.Now(), 0, &c)
	idle := newIdleTestInstance("idle", true, old, 0, &c)

	m := &Manager{
		instances: map[string]*Instance{
			"user": user, "active": active, "busy": busy, "fresh": fresh, "idle": idle,
		},
		activeID: "active",
		broker:   pubsub.NewBroker[ManagerEvent](),
	}
	t.Cleanup(m.broker.Shutdown)

	stopped := m.gcIdleInstances(time.Minute)
	if len(stopped) != 1 || stopped[0] != "idle" {
		t.Fatalf("stopped = %v, want [idle]", stopped)
	}
	for _, id := range []string{"user", "active", "busy", "fresh"} {
		if _, ok := m.instances[id]; !ok {
			t.Errorf("protected instance %q was removed by GC", id)
		}
	}
}

// TestInstanceCloseBlocksSlotAcquire verifies the close/acquire race guard: an
// in-flight slot prevents tryBeginClose, and once an instance is closing no new
// slot can be acquired (so a concurrent delegation falls back to the cold path).
func TestInstanceCloseBlocksSlotAcquire(t *testing.T) {
	inst := &Instance{lastActiveAt: time.Now()}

	if !inst.acquireDelegationSlot(0) {
		t.Fatal("first acquire failed")
	}
	if inst.tryBeginClose() {
		t.Fatal("tryBeginClose succeeded while a delegation is in flight")
	}
	inst.releaseDelegationSlot()

	if !inst.tryBeginClose() {
		t.Fatal("tryBeginClose failed on an idle instance")
	}
	if inst.acquireDelegationSlot(0) {
		t.Fatal("acquired a slot on a closing instance, want refused")
	}
}

// --- C2: promote-on-focus -------------------------------------------------

// stubService is a minimal Service for exercising Activate's reuse branch.
type stubService struct {
	proj *Project
}

func (s *stubService) Create(context.Context, string, string) (*Project, error) { return s.proj, nil }
func (s *stubService) Get(context.Context, string) (*Project, error)            { return s.proj, nil }
func (s *stubService) GetByPath(context.Context, string) (*Project, error)      { return s.proj, nil }
func (s *stubService) List(context.Context) ([]Project, error)                  { return nil, nil }
func (s *stubService) UpdateStatus(context.Context, string, string, int, int) error {
	return nil
}
func (s *stubService) MarkInitialized(context.Context, string) error { return nil }
func (s *stubService) TouchLastOpened(context.Context, string) error { return nil }
func (s *stubService) Rename(context.Context, string, string) error  { return nil }
func (s *stubService) Delete(context.Context, string) error          { return nil }

// TestActivatePromotesDelegationSpawnedInstance verifies C2: a user-driven
// Activate of a project currently served by a delegation-spawned (warm) instance
// claims it as user-focused (clears delegationSpawned) and makes it active, so
// the idle auto-GC will no longer consider it a candidate.
func TestActivatePromotesDelegationSpawnedInstance(t *testing.T) {
	const id = "proj-promote"
	tmpDir := t.TempDir()
	// Activate's step 3 requires a Pando config file at the resolved path.
	if err := os.WriteFile(filepath.Join(tmpDir, ".pando.toml"), []byte("[Mesnada]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &Instance{
		Project: Project{ID: id, Path: tmpDir},
		ready:   make(chan struct{}),
		errCh:   make(chan error, 1),
	}
	close(inst.ready)
	inst.markDelegationSpawned(true)

	m := &Manager{
		service:   &stubService{proj: &Project{ID: id, Path: tmpDir}},
		instances: map[string]*Instance{id: inst},
		broker:    pubsub.NewBroker[ManagerEvent](),
	}
	t.Cleanup(m.broker.Shutdown)

	if err := m.Activate(context.Background(), id); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if inst.isDelegationSpawned() {
		t.Error("delegationSpawned still true after user Activate, want promoted to user-focused")
	}
	if m.ActiveID() != id {
		t.Errorf("ActiveID = %q, want %q", m.ActiveID(), id)
	}
}
