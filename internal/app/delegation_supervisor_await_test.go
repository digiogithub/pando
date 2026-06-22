package app

import (
	"strings"
	"sync"
	"testing"
	"time"

	mesnadaOrch "github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// fakeAwaits stubs the awaitReader interface so the supervisor's await-aware path
// is controllable from tests. It holds a single intent keyed by session id.
type fakeAwaits struct {
	mu      sync.Mutex
	intents map[string]*mesnadaOrch.AwaitIntent
	cleared []string
}

func newFakeAwaits() *fakeAwaits {
	return &fakeAwaits{intents: make(map[string]*mesnadaOrch.AwaitIntent)}
}

func (f *fakeAwaits) AwaitIntentFor(sessionID string) (*mesnadaOrch.AwaitIntent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.intents[sessionID]
	return i, ok
}

func (f *fakeAwaits) ClearAwaitIntent(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.intents, sessionID)
	f.cleared = append(f.cleared, sessionID)
}

func (f *fakeAwaits) set(intent *mesnadaOrch.AwaitIntent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intents[intent.SessionID] = intent
}

func (f *fakeAwaits) clearedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cleared)
}

func newAwaitSup(inj *fakeInjector, lister parentLister, aw awaitReader, opts delegationSupervisorOptions) *delegationSupervisor {
	s := newResurrectSup(inj, lister, opts)
	s.awaits = aw
	return s
}

func taskWithStatus(id string, status models.TaskStatus) *models.Task {
	return &models.Task{
		ID:              id,
		Engine:          models.EnginePando,
		Status:          status,
		ParentSessionID: "sess-1",
		Conclusion:      &models.Conclusion{Status: "success", Summary: "done " + id},
	}
}

func completedWithID(id, parent, corr string) *models.Task {
	tk := completedTask(parent, corr)
	tk.ID = id
	return tk
}

// TestAwaitAllTwoTasksResurrectsOnSecond: policy=all over two awaited tasks. The
// first completion does NOT resurrect (one still outstanding); the second
// completion (condition met) → exactly one Resume with both conclusions + intent
// cleared.
func TestAwaitAllTwoTasksResurrectsOnSecond(t *testing.T) {
	inj := newFakeInjector() // sess-1 idle
	lister := &fakeLister{}
	aw := newFakeAwaits()
	aw.set(&mesnadaOrch.AwaitIntent{
		SessionID: "sess-1",
		TaskIDs:   []string{"t1", "t2"},
		Policy:    mesnadaOrch.AwaitPolicyAll,
		Deadline:  time.Now().Add(time.Hour),
	})
	opts := resurrectOpts()
	opts.ResurrectionTimeout = time.Hour // ensure no debounce path interferes
	s := newAwaitSup(inj, lister, aw, opts)

	// First completion: t1 terminal, t2 still running => not satisfied.
	lister.set([]*models.Task{
		taskWithStatus("t1", models.TaskStatusCompleted),
		taskWithStatus("t2", models.TaskStatusRunning),
	})
	if !s.handleCompletion(completedWithID("t1", "sess-1", "corr-1")) {
		t.Fatal("expected first completion handled (batched)")
	}
	if inj.resumeCount() != 0 {
		t.Fatalf("expected no resume after first of two, got %d", inj.resumeCount())
	}

	// Second completion: both terminal => satisfied => resurrect once.
	lister.set([]*models.Task{
		taskWithStatus("t1", models.TaskStatusCompleted),
		taskWithStatus("t2", models.TaskStatusCompleted),
	})
	if !s.handleCompletion(completedWithID("t2", "sess-1", "corr-2")) {
		t.Fatal("expected second completion to flush")
	}
	if inj.resumeCount() != 1 {
		t.Fatalf("expected exactly 1 resume, got %d", inj.resumeCount())
	}
	if aw.clearedCount() != 1 {
		t.Fatalf("expected intent cleared once, got %d", aw.clearedCount())
	}
	content := inj.lastResumeContent()
	if !strings.Contains(content, "t1") || !strings.Contains(content, "t2") {
		t.Fatalf("expected both conclusions in resume content, got: %s", content)
	}
	if !strings.Contains(content, "mesnada_await") {
		t.Fatalf("expected await framing in resume content, got: %s", content)
	}
}

// TestAwaitAnyResurrectsOnFirst: policy=any → the first completion resurrects.
func TestAwaitAnyResurrectsOnFirst(t *testing.T) {
	inj := newFakeInjector()
	lister := &fakeLister{}
	aw := newFakeAwaits()
	aw.set(&mesnadaOrch.AwaitIntent{
		SessionID: "sess-1",
		TaskIDs:   []string{"t1", "t2"},
		Policy:    mesnadaOrch.AwaitPolicyAny,
		Deadline:  time.Now().Add(time.Hour),
	})
	s := newAwaitSup(inj, lister, aw, resurrectOpts())

	lister.set([]*models.Task{
		taskWithStatus("t1", models.TaskStatusCompleted),
		taskWithStatus("t2", models.TaskStatusRunning),
	})
	if !s.handleCompletion(completedWithID("t1", "sess-1", "corr-1")) {
		t.Fatal("expected handled")
	}
	if inj.resumeCount() != 1 {
		t.Fatalf("expected 1 resume for policy=any on first completion, got %d", inj.resumeCount())
	}
	if aw.clearedCount() != 1 {
		t.Fatalf("expected intent cleared, got %d", aw.clearedCount())
	}
}

// TestAwaitDeadlinePassedFlushesEvenIfUnsatisfied: when the intent deadline is in
// the past, an eligible completion flushes even though the join condition is not
// satisfied.
func TestAwaitDeadlinePassedFlushesEvenIfUnsatisfied(t *testing.T) {
	inj := newFakeInjector()
	lister := &fakeLister{}
	aw := newFakeAwaits()
	aw.set(&mesnadaOrch.AwaitIntent{
		SessionID: "sess-1",
		TaskIDs:   []string{"t1", "t2"},
		Policy:    mesnadaOrch.AwaitPolicyAll,
		Deadline:  time.Now().Add(-time.Minute), // already passed
	})
	s := newAwaitSup(inj, lister, aw, resurrectOpts())

	// Only t1 terminal (all-policy not satisfied), but deadline passed => flush.
	lister.set([]*models.Task{
		taskWithStatus("t1", models.TaskStatusCompleted),
		taskWithStatus("t2", models.TaskStatusRunning),
	})
	if !s.handleCompletion(completedWithID("t1", "sess-1", "corr-1")) {
		t.Fatal("expected handled")
	}
	if inj.resumeCount() != 1 {
		t.Fatalf("expected 1 resume after deadline passed, got %d", inj.resumeCount())
	}
	if !strings.Contains(inj.lastResumeContent(), "timeout") {
		t.Fatalf("expected timeout framing, got: %s", inj.lastResumeContent())
	}
}

// TestNoAwaitIntentBehavesAsPhase3: with no await intent the Phase-3 path is used
// (no outstanding siblings → immediate flush).
func TestNoAwaitIntentBehavesAsPhase3(t *testing.T) {
	inj := newFakeInjector()
	lister := &fakeLister{} // no outstanding siblings
	aw := newFakeAwaits()   // no intent set
	s := newAwaitSup(inj, lister, aw, resurrectOpts())

	if !s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected Phase-3 resurrection")
	}
	if inj.resumeCount() != 1 {
		t.Fatalf("expected 1 resume via Phase-3 path, got %d", inj.resumeCount())
	}
	if aw.clearedCount() != 0 {
		t.Fatalf("expected no intent clear on Phase-3 path, got %d", aw.clearedCount())
	}
	// Phase-3 framing (not await framing).
	if strings.Contains(inj.lastResumeContent(), "mesnada_await") {
		t.Fatalf("expected non-await framing, got: %s", inj.lastResumeContent())
	}
}
