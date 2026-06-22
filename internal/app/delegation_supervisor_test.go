package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// fakeInjector is a minimal delegationAgent for testing the supervisor's decision
// core without spinning up a real agent.
type fakeInjector struct {
	mu          sync.Mutex
	busy        map[string]bool
	injected    []string // parentSessionIDs injected into (Case A)
	injectArgs  []string // content passed to InjectConclusion, in order
	resumed     []string // parentSessionIDs resurrected (Case B)
	resumeArgs  []string // content passed to Resume, in order
	injerr      error
	resumeErr   error
	resCount    int // value returned by ResurrectionCount (stub)
	resCountInc bool
}

func newFakeInjector() *fakeInjector {
	return &fakeInjector{busy: make(map[string]bool)}
}

func (f *fakeInjector) IsSessionBusy(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy[sessionID]
}

func (f *fakeInjector) InjectConclusion(sessionID string, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.injerr != nil {
		return f.injerr
	}
	f.injected = append(f.injected, sessionID)
	f.injectArgs = append(f.injectArgs, content)
	return nil
}

func (f *fakeInjector) Resume(ctx context.Context, sessionID string, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resumeErr != nil {
		return f.resumeErr
	}
	f.resumed = append(f.resumed, sessionID)
	f.resumeArgs = append(f.resumeArgs, content)
	if f.resCountInc {
		f.resCount++
	}
	return nil
}

func (f *fakeInjector) ResurrectionCount(sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resCount
}

func (f *fakeInjector) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.injected)
}

func (f *fakeInjector) resumeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.resumed)
}

func (f *fakeInjector) lastResumeContent() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.resumeArgs) == 0 {
		return ""
	}
	return f.resumeArgs[len(f.resumeArgs)-1]
}

// fakeLister stubs the parentLister interface so the supervisor's
// outstanding-sibling check can be driven from tests. It returns the configured
// task slice (or error) regardless of the queried session id.
type fakeLister struct {
	mu    sync.Mutex
	tasks []*models.Task
	err   error
}

func (l *fakeLister) ListByParentSession(sessionID string) ([]*models.Task, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	return l.tasks, nil
}

func (l *fakeLister) set(tasks []*models.Task) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tasks = tasks
}

func newSup(inj *fakeInjector, enabled, live bool) *delegationSupervisor {
	s := newDelegationSupervisor(nil, inj, delegationSupervisorOptions{
		Enabled:            enabled,
		InjectIntoLiveLoop: live,
	})
	return s
}

func completedTask(parent, corr string) *models.Task {
	return &models.Task{
		ID:              "task-1",
		Engine:          models.EnginePando,
		Status:          models.TaskStatusCompleted,
		ParentSessionID: parent,
		CorrelationID:   corr,
		Conclusion:      &models.Conclusion{Status: "success", Summary: "done"},
	}
}

func TestHandleCompletionInjectsWhenBusy(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	s := newSup(inj, true, true)

	if !s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected injection")
	}
	if inj.count() != 1 {
		t.Fatalf("expected 1 injection, got %d", inj.count())
	}
}

func TestHandleCompletionDedupesByCorrelationID(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	s := newSup(inj, true, true)

	s.handleCompletion(completedTask("sess-1", "corr-1"))
	// Second completion with same correlation id must be skipped.
	if s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected dedupe to skip second injection")
	}
	if inj.count() != 1 {
		t.Fatalf("expected exactly 1 injection after dedupe, got %d", inj.count())
	}
}

func TestHandleCompletionFallsBackToTaskIDWhenNoCorrelation(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	s := newSup(inj, true, true)

	task := completedTask("sess-1", "") // no correlation id
	if !s.handleCompletion(task) {
		t.Fatal("expected injection with task.ID fallback key")
	}
	if s.handleCompletion(task) {
		t.Fatal("expected dedupe by task.ID")
	}
	if inj.count() != 1 {
		t.Fatalf("expected 1 injection, got %d", inj.count())
	}
}

func TestHandleCompletionSkipsWhenNotBusy(t *testing.T) {
	inj := newFakeInjector() // sess-1 not busy
	s := newSup(inj, true, true)

	if s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected skip for idle session")
	}
	if inj.count() != 0 {
		t.Fatalf("expected 0 injections, got %d", inj.count())
	}
}

func TestHandleCompletionGatedOffWhenLiveDisabled(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	s := newSup(inj, true, false) // InjectIntoLiveLoop=false

	if s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected no-op when InjectIntoLiveLoop disabled")
	}
	if inj.count() != 0 {
		t.Fatalf("expected 0 injections, got %d", inj.count())
	}
}

func TestHandleCompletionGatedOffWhenDisabled(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	s := newSup(inj, false, true) // Enabled=false

	if s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected no-op when delegation disabled")
	}
}

func TestHandleCompletionSkipsWithoutParentOrConclusion(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	s := newSup(inj, true, true)

	// No parent session.
	noParent := completedTask("", "corr-1")
	if s.handleCompletion(noParent) {
		t.Fatal("expected skip when no parent session")
	}
	// No conclusion.
	noConc := completedTask("sess-1", "corr-2")
	noConc.Conclusion = nil
	if s.handleCompletion(noConc) {
		t.Fatal("expected skip when no conclusion")
	}
	if inj.count() != 0 {
		t.Fatalf("expected 0 injections, got %d", inj.count())
	}
}

func TestHandleCompletionRaceSessionEnded(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	inj.injerr = agent.ErrSessionNotBusy // race: ended between check and inject
	s := newSup(inj, true, true)

	if s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected no injection recorded on ErrSessionNotBusy race")
	}
	// Not marked as seen, so a later busy completion could retry.
	s.mu.Lock()
	_, seen := s.seen["corr-1"]
	s.mu.Unlock()
	if seen {
		t.Fatal("expected correlation NOT marked seen on failed injection")
	}
}

// newResurrectSup builds a supervisor wired for Case B resurrection with a stub
// lister so the outstanding-sibling check is controllable.
func newResurrectSup(inj *fakeInjector, lister parentLister, opts delegationSupervisorOptions) *delegationSupervisor {
	s := newDelegationSupervisor(nil, inj, opts)
	s.orch = lister
	s.ctx = context.Background()
	return s
}

func resurrectOpts() delegationSupervisorOptions {
	return delegationSupervisorOptions{
		Enabled:             true,
		ResurrectIdleLoop:   true,
		MaxResurrections:    4,
		MaxDepth:            3,
		ResurrectionTimeout: 50 * time.Millisecond,
	}
}

func TestResurrectIdleNoOutstandingFlushesOnce(t *testing.T) {
	inj := newFakeInjector() // sess-1 idle
	lister := &fakeLister{}  // no outstanding siblings
	s := newResurrectSup(inj, lister, resurrectOpts())

	if !s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected resurrection to be enqueued+flushed")
	}
	if inj.resumeCount() != 1 {
		t.Fatalf("expected exactly 1 resume, got %d", inj.resumeCount())
	}
	if content := inj.lastResumeContent(); content == "" {
		t.Fatal("expected combined resurrection content")
	}
}

func TestResurrectDepthCapBlocks(t *testing.T) {
	inj := newFakeInjector()
	lister := &fakeLister{}
	opts := resurrectOpts()
	opts.MaxDepth = 2
	s := newResurrectSup(inj, lister, opts)

	task := completedTask("sess-1", "corr-1")
	task.Depth = 2 // >= MaxDepth
	if s.handleCompletion(task) {
		t.Fatal("expected depth cap to block resurrection")
	}
	if inj.resumeCount() != 0 {
		t.Fatalf("expected 0 resumes, got %d", inj.resumeCount())
	}
}

func TestResurrectBudgetCapBlocks(t *testing.T) {
	inj := newFakeInjector()
	inj.resCount = 4 // already at the cap
	lister := &fakeLister{}
	s := newResurrectSup(inj, lister, resurrectOpts())

	if s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected budget cap to block resurrection")
	}
	if inj.resumeCount() != 0 {
		t.Fatalf("expected 0 resumes when budget exhausted, got %d", inj.resumeCount())
	}
}

func TestResurrectBatchesUntilSiblingTerminal(t *testing.T) {
	inj := newFakeInjector()
	// One sibling still running so the first completion is batched, not flushed.
	running := &models.Task{ID: "sib", Status: models.TaskStatusRunning, ParentSessionID: "sess-1"}
	lister := &fakeLister{tasks: []*models.Task{running}}
	opts := resurrectOpts()
	opts.ResurrectionTimeout = 2 * time.Second // long; we flush via the second completion
	s := newResurrectSup(inj, lister, opts)

	// First completion: sibling outstanding → batched (timer armed), no resume yet.
	if !s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected first completion to be batched")
	}
	if inj.resumeCount() != 0 {
		t.Fatalf("expected no resume while sibling outstanding, got %d", inj.resumeCount())
	}

	// Sibling becomes terminal; a second completion now finds none outstanding and
	// flushes the whole batch in ONE resume.
	lister.set(nil)
	second := completedTask("sess-1", "corr-2")
	second.ID = "task-2"
	if !s.handleCompletion(second) {
		t.Fatal("expected second completion to flush the batch")
	}
	if inj.resumeCount() != 1 {
		t.Fatalf("expected exactly 1 coalesced resume, got %d", inj.resumeCount())
	}
}

func TestResurrectResumeBusyFallsBackToInject(t *testing.T) {
	inj := newFakeInjector() // session idle at decision time
	inj.resumeErr = agent.ErrSessionBusy
	lister := &fakeLister{}
	s := newResurrectSup(inj, lister, resurrectOpts())

	if !s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected handleCompletion to attempt resurrection")
	}
	if inj.resumeCount() != 0 {
		t.Fatalf("expected resume to fail (busy), got %d successes", inj.resumeCount())
	}
	if inj.count() != 1 {
		t.Fatalf("expected 1 fallback injection after busy race, got %d", inj.count())
	}
}

func TestResurrectGatedOffWhenResurrectDisabled(t *testing.T) {
	inj := newFakeInjector()
	lister := &fakeLister{}
	opts := resurrectOpts()
	opts.ResurrectIdleLoop = false
	s := newResurrectSup(inj, lister, opts)

	if s.handleCompletion(completedTask("sess-1", "corr-1")) {
		t.Fatal("expected no-op when ResurrectIdleLoop disabled")
	}
	if inj.resumeCount() != 0 {
		t.Fatalf("expected 0 resumes, got %d", inj.resumeCount())
	}
}
