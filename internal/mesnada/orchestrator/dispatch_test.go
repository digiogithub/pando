package orchestrator

import (
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/mesnada/events"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// saveTask stores a task and fails the test if the store rejects it.
func saveTask(t *testing.T, o *Orchestrator, task *models.Task) *models.Task {
	t.Helper()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	if err := o.store.Save(task); err != nil {
		t.Fatalf("save %s: %v", task.ID, err)
	}
	return task
}

// These tests exercise admitAndClaim rather than tryDispatch wherever the
// outcome is asserted: tryDispatch hands the task to a goroutine that mutates
// it, and the store hands out shared task pointers, so polling the task from
// the test would be reading a field another goroutine is writing. The dispatch
// decision is the part under test; startTask is covered by the spawn tests.

func TestClaimForDispatchIsExclusive(t *testing.T) {
	o := newTestOrchestrator(t)
	saveTask(t, o, &models.Task{ID: "t1", Status: models.TaskStatusPending})

	expires := time.Now().Add(time.Minute)
	won, err := o.store.ClaimForDispatch("t1", "dispatcher-a", expires)
	if err != nil || !won {
		t.Fatalf("first claim should win, got won=%v err=%v", won, err)
	}

	won, err = o.store.ClaimForDispatch("t1", "dispatcher-b", expires)
	if err != nil {
		t.Fatalf("second claim error = %v", err)
	}
	if won {
		t.Fatal("a live claim held by another dispatcher must not be stolen")
	}

	// The same owner is refused too: concurrent dispatchers inside one process
	// share an owner id, so an owner-scoped check would let both start the task.
	won, err = o.store.ClaimForDispatch("t1", "dispatcher-a", expires)
	if err != nil {
		t.Fatalf("re-claim error = %v", err)
	}
	if won {
		t.Fatal("a live claim must block its own owner as well")
	}

	stored, _ := o.store.Get("t1")
	if stored.ClaimLock != "dispatcher-a" {
		t.Errorf("ClaimLock = %q, want dispatcher-a", stored.ClaimLock)
	}
}

func TestClaimForDispatchStealsExpiredLease(t *testing.T) {
	o := newTestOrchestrator(t)
	saveTask(t, o, &models.Task{ID: "t1", Status: models.TaskStatusPending})

	// A dispatcher that claimed the task and then died leaves an expired lease.
	if _, err := o.store.ClaimForDispatch("t1", "dead-dispatcher", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("claim: %v", err)
	}

	won, err := o.store.ClaimForDispatch("t1", "live-dispatcher", time.Now().Add(time.Minute))
	if err != nil || !won {
		t.Fatalf("an expired lease must be stealable, got won=%v err=%v", won, err)
	}
}

func TestClaimForDispatchRefusesNonPendingTask(t *testing.T) {
	o := newTestOrchestrator(t)
	saveTask(t, o, &models.Task{ID: "t1", Status: models.TaskStatusRunning})

	won, err := o.store.ClaimForDispatch("t1", "dispatcher", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim error = %v", err)
	}
	if won {
		t.Fatal("only a pending task may be claimed for dispatch")
	}
}

func TestSecondDispatchOfSameTaskIsRefused(t *testing.T) {
	o := newTestOrchestrator(t)
	task := saveTask(t, o, &models.Task{ID: "t1", Status: models.TaskStatusPending})

	// Two readiness evaluations reaching the same task — two dependencies
	// completing at once used to start it twice. Exactly one wins the claim.
	if !o.admitAndClaim(task) {
		t.Fatal("first dispatch decision should win the claim")
	}
	if o.admitAndClaim(task) {
		t.Fatal("second dispatch decision must lose the claim")
	}
	if got := o.metrics.Snapshot().ClaimsRejected; got != 1 {
		t.Errorf("ClaimsRejected = %d, want 1", got)
	}
}

func TestTryDispatchDefersOverMaxParallel(t *testing.T) {
	o := newTestOrchestrator(t)
	o.maxParallel = 1
	saveTask(t, o, &models.Task{ID: "running", Status: models.TaskStatusRunning})
	ready := saveTask(t, o, &models.Task{ID: "ready", Status: models.TaskStatusPending})

	if o.admitAndClaim(ready) {
		t.Fatal("dispatch should be deferred: the single slot is taken")
	}
	stored := mustGet(t, o, "ready")
	if stored.Status != models.TaskStatusPending {
		t.Errorf("deferred task status = %s, want pending", stored.Status)
	}
	if stored.ClaimLock != "" {
		t.Errorf("a deferred task must not hold a claim, got %q", stored.ClaimLock)
	}
	if got := o.metrics.Snapshot().DispatchesDeferred; got != 1 {
		t.Errorf("DispatchesDeferred = %d, want 1", got)
	}
}

func TestTryDispatchDefersOverPerEngineCap(t *testing.T) {
	o := newTestOrchestrator(t)
	o.maxPerEngine = 1
	saveTask(t, o, &models.Task{ID: "busy", Status: models.TaskStatusRunning, Engine: models.EngineClaude})
	sameEngine := saveTask(t, o, &models.Task{ID: "same", Status: models.TaskStatusPending, Engine: models.EngineClaude})
	otherEngine := saveTask(t, o, &models.Task{ID: "other", Status: models.TaskStatusPending, Engine: models.EngineCopilot})

	if o.admitAndClaim(sameEngine) {
		t.Error("a task on a saturated engine must be deferred")
	}
	// The cap is per engine, so an idle engine still dispatches.
	if !o.admitAndClaim(otherEngine) {
		t.Fatal("a task on an idle engine must still dispatch")
	}
	if stored := mustGet(t, o, "other"); stored.ClaimLock == "" {
		t.Error("the dispatched task should hold a claim")
	}
}

func TestInFlightCountsIgnoreUnclaimedPending(t *testing.T) {
	o := newTestOrchestrator(t)
	saveTask(t, o, &models.Task{ID: "running", Status: models.TaskStatusRunning, Engine: models.EngineClaude})
	saveTask(t, o, &models.Task{ID: "queued", Status: models.TaskStatusPending, Engine: models.EngineClaude})
	claimed := saveTask(t, o, &models.Task{ID: "claimed", Status: models.TaskStatusPending, Engine: models.EngineCopilot})
	if _, err := o.store.ClaimForDispatch(claimed.ID, "orch-test", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("claim: %v", err)
	}

	total, perEngine := o.inFlightCounts()
	// running + claimed occupy slots; the unclaimed pending task is merely queued.
	if total != 2 {
		t.Errorf("in-flight total = %d, want 2", total)
	}
	if perEngine[models.EngineClaude] != 1 || perEngine[models.EngineCopilot] != 1 {
		t.Errorf("per-engine counts = %v, want one each", perEngine)
	}
}

func TestReclaimExpiredClaimsFreesStrandedTask(t *testing.T) {
	o := newTestOrchestrator(t)
	expired := time.Now().Add(-time.Minute)
	saveTask(t, o, &models.Task{
		ID: "stranded", Status: models.TaskStatusPending,
		ClaimLock: "dead-dispatcher", ClaimExpires: &expired,
	})
	live := time.Now().Add(time.Minute)
	saveTask(t, o, &models.Task{
		ID: "starting", Status: models.TaskStatusPending,
		ClaimLock: "live-dispatcher", ClaimExpires: &live,
	})

	if got := o.reclaimExpiredClaims(); got != 1 {
		t.Fatalf("reclaimed %d tasks, want 1", got)
	}
	if stored := mustGet(t, o, "stranded"); stored.ClaimLock != "" {
		t.Errorf("stranded task still holds claim %q", stored.ClaimLock)
	}
	if stored := mustGet(t, o, "starting"); stored.ClaimLock != "live-dispatcher" {
		t.Error("a live claim must not be reclaimed")
	}
	if got := o.metrics.Snapshot().ClaimsReclaimed; got != 1 {
		t.Errorf("ClaimsReclaimed = %d, want 1", got)
	}

	// The reclaim is recorded durably so the churn is visible after the fact.
	found := false
	for _, e := range o.RecentEvents(10) {
		if e.Kind == events.KindReclaimed && e.TaskID == "stranded" {
			found = true
		}
	}
	if !found {
		t.Error("reclaim did not append a durable event")
	}
}

func TestDrainDispatchableRespectsDependenciesAndPriority(t *testing.T) {
	o := newTestOrchestrator(t)
	o.maxParallel = 1

	saveTask(t, o, &models.Task{ID: "dep", Status: models.TaskStatusRunning})
	saveTask(t, o, &models.Task{
		ID: "blocked", Status: models.TaskStatusPending,
		Dependencies: []string{"dep"}, Priority: 100,
	})
	saveTask(t, o, &models.Task{ID: "ready", Status: models.TaskStatusPending, Priority: 1})

	// "blocked" outranks "ready" but is not startable, and the single slot is
	// held by "dep", so the drain starts nothing.
	if n := o.drainDispatchable(); n != 0 {
		t.Fatalf("drain dispatched %d tasks while the cap was full, want 0", n)
	}
	if stored := mustGet(t, o, "ready"); stored.ClaimLock != "" {
		t.Error("a task the drain skipped must not hold a claim")
	}

	// Free the slot: now the drain picks up the startable, highest-priority task
	// — and only that one, because the slot it took fills the cap again.
	completions, unsubscribe := o.SubscribeCompletions()
	defer unsubscribe()
	dep := mustGet(t, o, "dep")
	dep.Status = models.TaskStatusCompleted
	saveTask(t, o, dep)

	if n := o.drainDispatchable(); n != 1 {
		t.Fatalf("drain dispatched %d tasks, want 1", n)
	}
	// Wait for the dispatched task to finish before inspecting it: receiving the
	// completion is the happens-before edge with the goroutine that ran it.
	select {
	case done := <-completions:
		if done.ID != "blocked" {
			t.Fatalf("drain started %s, want the higher-priority blocked task", done.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatched task never completed")
	}
}

func TestSortByDispatchOrder(t *testing.T) {
	base := time.Now()
	tasks := []*models.Task{
		{ID: "old-low", Priority: 1, CreatedAt: base},
		{ID: "new-high", Priority: 5, CreatedAt: base.Add(time.Minute)},
		{ID: "new-low", Priority: 1, CreatedAt: base.Add(time.Minute)},
	}
	sortByDispatchOrder(tasks)

	want := []string{"new-high", "old-low", "new-low"}
	for i, id := range want {
		if tasks[i].ID != id {
			t.Fatalf("position %d = %s, want %s (priority desc, then oldest first)", i, tasks[i].ID, id)
		}
	}
}

func TestOnTaskCompleteAppendsTerminalEventAndReleasesClaim(t *testing.T) {
	o := newTestOrchestrator(t)
	expires := time.Now().Add(time.Minute)
	task := saveTask(t, o, &models.Task{
		ID: "t1", Status: models.TaskStatusCompleted,
		ParentSessionID: "session-1", CorrelationID: "corr-1",
		ClaimLock: "orch-test", ClaimExpires: &expires,
	})

	o.onTaskComplete(task)

	unseen := o.UnseenEvents("test-sub")
	if len(unseen) != 1 {
		t.Fatalf("got %d events, want 1", len(unseen))
	}
	e := unseen[0]
	if e.Kind != events.KindCompleted || e.TaskID != "t1" || e.ParentSessionID != "session-1" {
		t.Fatalf("unexpected event %+v", e)
	}
	if stored := mustGet(t, o, "t1"); stored.ClaimLock != "" {
		t.Error("a finished task must release its dispatch claim")
	}

	// Acking is what makes the signal stop being redelivered.
	if err := o.AckEvent("test-sub", e.Seq); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if got := len(o.UnseenEvents("test-sub")); got != 0 {
		t.Errorf("after ack %d events remain unseen, want 0", got)
	}
}

func TestTerminalEventCarriesConclusionSummary(t *testing.T) {
	o := newTestOrchestrator(t)
	task := saveTask(t, o, &models.Task{
		ID: "t1", Status: models.TaskStatusFailed, Error: "process exited 1",
		Conclusion: &models.Conclusion{Status: "failed", Summary: "could not build"},
	})

	o.recordTerminalEvent(task)

	unseen := o.UnseenEvents("test-sub")
	if len(unseen) != 1 {
		t.Fatalf("got %d events, want 1", len(unseen))
	}
	if unseen[0].Kind != events.KindFailed {
		t.Errorf("Kind = %s, want failed", unseen[0].Kind)
	}
	if unseen[0].Detail != "could not build" {
		t.Errorf("Detail = %q, want the conclusion summary", unseen[0].Detail)
	}
}

func TestNonTerminalStatusRecordsNoEvent(t *testing.T) {
	o := newTestOrchestrator(t)
	o.recordTerminalEvent(&models.Task{ID: "t1", Status: models.TaskStatusRunning})
	if got := len(o.UnseenEvents("test-sub")); got != 0 {
		t.Errorf("a running task recorded %d events, want 0", got)
	}
}

func TestDisabledEventLogIsSilentButHarmless(t *testing.T) {
	o := newTestOrchestrator(t)
	o.eventLog = nil // as if [Mesnada.Delegation] EventLogDisabled = true

	task := saveTask(t, o, &models.Task{ID: "t1", Status: models.TaskStatusCompleted})
	o.onTaskComplete(task)

	if o.EventLogEnabled() {
		t.Error("EventLogEnabled() should be false without a log")
	}
	if got := len(o.UnseenEvents("test-sub")); got != 0 {
		t.Errorf("disabled log returned %d events", got)
	}
}

func TestCancelRecordsDurableEvent(t *testing.T) {
	o := newTestOrchestrator(t)
	saveTask(t, o, &models.Task{ID: "t1", Status: models.TaskStatusPending})

	if err := o.Cancel("t1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	unseen := o.UnseenEvents("test-sub")
	if len(unseen) != 1 || unseen[0].Kind != events.KindCancelled {
		t.Fatalf("cancel should record one cancelled event, got %+v", unseen)
	}
}

func mustGet(t *testing.T, o *Orchestrator, id string) *models.Task {
	t.Helper()
	task, err := o.store.Get(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return task
}
