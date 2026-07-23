package app

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/mesnada/events"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// fakeEventSource stubs the durable event log so the supervisor's drain can be
// driven without an orchestrator. It models the real cursor semantics: Unseen
// returns everything past the acked watermark, and Ack only moves it forward.
type fakeEventSource struct {
	mu      sync.Mutex
	enabled bool
	log     []events.Event
	tasks   map[string]*models.Task
	cursor  int64
	acks    []int64
	ackErr  error
}

func newFakeEventSource() *fakeEventSource {
	return &fakeEventSource{enabled: true, tasks: make(map[string]*models.Task)}
}

func (f *fakeEventSource) EventLogEnabled() bool { return f.enabled }

func (f *fakeEventSource) UnseenEvents(subID string) []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []events.Event
	for _, e := range f.log {
		if e.Seq > f.cursor {
			out = append(out, e)
		}
	}
	return out
}

func (f *fakeEventSource) AckEvent(subID string, seq int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ackErr != nil {
		return f.ackErr
	}
	f.acks = append(f.acks, seq)
	if seq > f.cursor {
		f.cursor = seq
	}
	return nil
}

func (f *fakeEventSource) GetTask(taskID string) (*models.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	return task, nil
}

// add records a task plus the terminal event pointing at it.
func (f *fakeEventSource) add(task *models.Task, kind events.Kind, age time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID] = task
	f.log = append(f.log, events.Event{
		Seq:             int64(len(f.log) + 1),
		Kind:            kind,
		TaskID:          task.ID,
		ParentSessionID: task.ParentSessionID,
		CorrelationID:   task.CorrelationID,
		CreatedAt:       time.Now().Add(-age),
	})
}

func (f *fakeEventSource) ackedSeqs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.acks...)
}

// newEventSup builds a supervisor wired to a durable event source, with live
// injection on so a busy parent session records an observable action.
func newEventSup(inj *fakeInjector, src *fakeEventSource) *delegationSupervisor {
	s := newDelegationSupervisor(nil, inj, delegationSupervisorOptions{
		Enabled:            true,
		InjectIntoLiveLoop: true,
		ResurrectIdleLoop:  true,
	})
	s.events = src
	return s
}

func taskWithConclusion(id, parent string) *models.Task {
	return &models.Task{
		ID:              id,
		Engine:          models.EnginePando,
		Status:          models.TaskStatusCompleted,
		ParentSessionID: parent,
		CorrelationID:   "corr-" + id,
		Conclusion:      &models.Conclusion{Status: "success", Summary: "done"},
	}
}

func TestDrainEventsHandlesAndAcksInOrder(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	src := newFakeEventSource()
	src.add(taskWithConclusion("task-1", "sess-1"), events.KindCompleted, 0)
	src.add(taskWithConclusion("task-2", "sess-1"), events.KindCompleted, 0)
	s := newEventSup(inj, src)

	s.drainEvents()

	if got := inj.count(); got != 2 {
		t.Fatalf("injected %d conclusions, want 2", got)
	}
	acked := src.ackedSeqs()
	if len(acked) != 2 || acked[0] != 1 || acked[1] != 2 {
		t.Fatalf("acked = %v, want 1 then 2", acked)
	}
	if got := len(src.UnseenEvents(supervisorSubscriptionID)); got != 0 {
		t.Errorf("%d events still unseen after drain", got)
	}
}

func TestDrainEventsReplaysOnlyUnackedEvents(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	src := newFakeEventSource()
	src.add(taskWithConclusion("task-1", "sess-1"), events.KindCompleted, 0)
	src.add(taskWithConclusion("task-2", "sess-1"), events.KindCompleted, 0)
	// A previous run already handled and acked the first event.
	src.cursor = 1
	s := newEventSup(inj, src)

	s.drainEvents()

	if got := inj.count(); got != 1 {
		t.Fatalf("injected %d conclusions, want only the unacked one", got)
	}
	if inj.injected[0] != "sess-1" {
		t.Errorf("injected into %q", inj.injected[0])
	}
}

func TestDrainEventsSkipsNonTerminalKinds(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	src := newFakeEventSource()
	src.add(taskWithConclusion("task-1", "sess-1"), events.KindGateFailed, 0)
	src.add(taskWithConclusion("task-2", "sess-1"), events.KindBreakerTripped, 0)
	s := newEventSup(inj, src)

	s.drainEvents()

	if got := inj.count(); got != 0 {
		t.Fatalf("injected %d conclusions, want 0 (these signals are informational)", got)
	}
	// They are still acked, so they do not pile up and get replayed forever.
	if got := len(src.ackedSeqs()); got != 2 {
		t.Errorf("acked %d events, want both", got)
	}
}

func TestDrainEventsSkipsStaleReplay(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	src := newFakeEventSource()
	src.add(taskWithConclusion("task-1", "sess-1"), events.KindCompleted, eventReplayMaxAge+time.Minute)
	s := newEventSup(inj, src)

	s.drainEvents()

	if got := inj.count(); got != 0 {
		t.Fatalf("injected %d conclusions, want 0: re-entering a loop over a long-stale result is noise", got)
	}
	if got := len(src.ackedSeqs()); got != 1 {
		t.Errorf("a stale event must still be acked, acked %d", got)
	}
}

func TestDrainEventsSkipsUnknownTask(t *testing.T) {
	inj := newFakeInjector()
	src := newFakeEventSource()
	src.log = append(src.log, events.Event{
		Seq: 1, Kind: events.KindCompleted, TaskID: "deleted-task", CreatedAt: time.Now(),
	})
	s := newEventSup(inj, src)

	s.drainEvents()

	if got := inj.count(); got != 0 {
		t.Errorf("injected %d conclusions for a task that no longer exists", got)
	}
	if got := len(src.ackedSeqs()); got != 1 {
		t.Errorf("an unresolvable event must be acked, acked %d", got)
	}
}

func TestDrainEventsStopsOnAckFailure(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	src := newFakeEventSource()
	src.add(taskWithConclusion("task-1", "sess-1"), events.KindCompleted, 0)
	src.add(taskWithConclusion("task-2", "sess-1"), events.KindCompleted, 0)
	src.ackErr = fmt.Errorf("disk full")
	s := newEventSup(inj, src)

	s.drainEvents()

	// The first event is handled; the drain then stops rather than racing ahead
	// over a cursor it could not persist.
	if got := inj.count(); got != 1 {
		t.Fatalf("handled %d events, want 1 before bailing out", got)
	}
}

func TestDrainEventsDedupesAcrossRedelivery(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-1"] = true
	src := newFakeEventSource()
	src.add(taskWithConclusion("task-1", "sess-1"), events.KindCompleted, 0)
	s := newEventSup(inj, src)

	s.drainEvents()
	// Simulate a redelivery: the ack was lost, so the same event comes back.
	src.cursor = 0
	s.drainEvents()

	if got := inj.count(); got != 1 {
		t.Fatalf("injected %d times, want 1: at-least-once delivery needs consumer-side dedupe", got)
	}
}

func TestDurableEventsOffWhenLogDisabled(t *testing.T) {
	src := newFakeEventSource()
	src.enabled = false
	s := newEventSup(newFakeInjector(), src)

	if s.durableEvents() {
		t.Error("a disabled log must fall back to the in-memory completion bus")
	}

	s.events = nil
	if s.durableEvents() {
		t.Error("no event source means no durable path")
	}
	s.drainEvents() // must not panic
}
