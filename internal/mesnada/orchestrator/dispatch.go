package orchestrator

import (
	"log"
	"sort"
	"time"

	"github.com/digiogithub/pando/internal/mesnada/events"
	"github.com/digiogithub/pando/internal/mesnada/store"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// Dispatch defaults. The claim lease only has to cover the window between
// reserving a task and the spawner marking it running, so a short TTL is enough;
// the tick reclaims anything stranded past it.
const (
	// DefaultClaimTTL bounds a dispatch reservation.
	DefaultClaimTTL = 2 * time.Minute
	// DefaultDispatchInterval is how often the dispatch tick reclaims expired
	// claims and drains the queue of tasks deferred by a concurrency cap.
	DefaultDispatchInterval = 10 * time.Second
)

// claimTTLOrDefault / dispatchIntervalOrDefault resolve the configured values,
// falling back to the package defaults when unset.
func (o *Orchestrator) claimTTLOrDefault() time.Duration {
	if o.claimTTL > 0 {
		return o.claimTTL
	}
	return DefaultClaimTTL
}

func (o *Orchestrator) dispatchIntervalOrDefault() time.Duration {
	if o.dispatchInterval > 0 {
		return o.dispatchInterval
	}
	return DefaultDispatchInterval
}

// dispatchLoop periodically reclaims expired claims and drains tasks that were
// deferred by a concurrency cap. Without it, a task that could not be admitted
// when its dependency completed would wait for the next unrelated completion to
// be reconsidered — the tick is what turns the cap into a queue instead of a
// dead end. It exits when the orchestrator context is cancelled.
func (o *Orchestrator) dispatchLoop() {
	defer o.wg.Done()

	ticker := time.NewTicker(o.dispatchIntervalOrDefault())
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			o.dispatchTick()
		}
	}
}

// dispatchTick runs one reclaim-then-drain cycle.
func (o *Orchestrator) dispatchTick() {
	o.reclaimExpiredClaims()
	o.drainDispatchable()
}

// reclaimExpiredClaims returns tasks stranded by a dispatcher that claimed them
// but never started them (a crash between the claim and the spawn) to the
// dispatchable pool. Only pending tasks are considered: once a task is running,
// its lifecycle is owned by the spawner, not by the claim.
func (o *Orchestrator) reclaimExpiredClaims() int {
	tasks, err := o.store.List(store.ListFilter{
		Status: []models.TaskStatus{models.TaskStatusPending},
	})
	if err != nil {
		return 0
	}

	now := time.Now()
	reclaimed := 0
	for _, task := range tasks {
		if task.ClaimLock == "" || task.ClaimActive(now) {
			continue
		}
		owner := task.ClaimLock
		if err := o.store.ReleaseClaim(task.ID); err != nil {
			continue
		}
		reclaimed++
		o.metrics.recordClaimReclaimed()
		o.appendEvent(events.Event{
			Kind:            events.KindReclaimed,
			TaskID:          task.ID,
			ParentSessionID: task.ParentSessionID,
			CorrelationID:   task.CorrelationID,
			Detail:          "claim lease expired, task returned to the dispatchable pool",
		})
		log.Printf("task_event=claim_reclaimed task_id=%s previous_owner=%s", task.ID, owner)
	}
	return reclaimed
}

// drainDispatchable starts every pending task that is ready and fits under the
// concurrency caps, highest priority first, and reports how many it dispatched.
// It is the single place that turns "ready" into "running" for automatic
// dispatch.
func (o *Orchestrator) drainDispatchable() int {
	tasks, err := o.store.List(store.ListFilter{
		Status: []models.TaskStatus{models.TaskStatusPending},
	})
	if err != nil {
		return 0
	}

	// Higher priority first, then oldest first — a fair queue rather than the
	// newest-first order List returns.
	sortByDispatchOrder(tasks)

	// Capacity is sampled once and then tracked locally as the pass hands tasks
	// out. Re-reading it per task would mean inspecting a task this same loop
	// just launched, while the goroutine running it is still writing to it.
	total, perEngine := o.inFlightCounts()

	dispatched := 0
	for _, task := range tasks {
		if task.ClaimActive(time.Now()) {
			continue // another dispatcher is already starting it
		}
		if !o.canStart(task) {
			continue
		}
		if reason, ok := o.fits(total, perEngine, task); !ok {
			// Not fatal to the drain: a lower-priority task may target a different
			// engine that still has capacity.
			o.metrics.recordDispatchDeferred()
			logTaskDeferred(task, reason)
			continue
		}
		if !o.claimForDispatch(task) {
			continue
		}
		total++
		perEngine[o.effectiveEngine(task)]++
		dispatched++
		go o.startTask(task)
	}
	return dispatched
}

// sortByDispatchOrder orders tasks priority-descending, then creation-ascending.
func sortByDispatchOrder(tasks []*models.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority > tasks[j].Priority
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
}

// tryDispatch applies admission control and, if the task is admitted and its
// claim is won, starts it in the background. It returns whether the task was
// dispatched. A refusal is never an error: the task stays pending and the next
// completion or dispatch tick reconsiders it.
func (o *Orchestrator) tryDispatch(task *models.Task) bool {
	if !o.admitAndClaim(task) {
		return false
	}
	go o.startTask(task)
	return true
}

// admitAndClaim is the whole dispatch decision — concurrency caps, then the
// claim compare-and-set — with no side effect beyond taking the claim. Keeping
// it separate from the goroutine launch is what makes the decision testable and
// keeps "may this run?" auditable independently of "run it".
func (o *Orchestrator) admitAndClaim(task *models.Task) bool {
	if task == nil {
		return false
	}
	if reason, ok := o.admit(task); !ok {
		o.metrics.recordDispatchDeferred()
		logTaskDeferred(task, reason)
		return false
	}
	return o.claimForDispatch(task)
}

// dispatchNow starts a task on an explicit operator/agent action (Spawn's
// foreground path, Resume, Relaunch, Retry). It still takes the claim — so a
// concurrent automatic dispatch cannot start the same task twice — but it
// deliberately bypasses the concurrency caps: someone asked for this task by
// name, and silently queueing it would be surprising.
func (o *Orchestrator) dispatchNow(task *models.Task, background bool) {
	if task == nil {
		return
	}
	if !o.claimForDispatch(task) {
		return
	}
	if background {
		go o.startTask(task)
		return
	}
	o.startTask(task)
}

// claimForDispatch reserves the task for this orchestrator instance. It returns
// true when the claim was won (or when the store predates claim support, so
// dispatch degrades to the previous unguarded behavior rather than stalling).
func (o *Orchestrator) claimForDispatch(task *models.Task) bool {
	won, err := o.store.ClaimForDispatch(task.ID, o.instanceID, time.Now().Add(o.claimTTLOrDefault()))
	if err != nil {
		log.Printf("Warning: failed to claim task %s for dispatch: %v", task.ID, err)
		return false
	}
	if !won {
		log.Printf("task_event=claim_rejected task_id=%s owner=%s", task.ID, o.instanceID)
		o.metrics.recordClaimRejected()
	}
	return won
}

// releaseClaim drops a task's dispatch claim once it is no longer pending.
func (o *Orchestrator) releaseClaim(taskID string) {
	if err := o.store.ReleaseClaim(taskID); err != nil {
		log.Printf("Warning: failed to release claim for task %s: %v", taskID, err)
	}
}

// admit reports whether a task may start now under the concurrency caps, and if
// not, which cap refused it. It samples the current occupancy; the drain uses
// fits directly against a running tally instead.
func (o *Orchestrator) admit(task *models.Task) (string, bool) {
	if o.maxParallel <= 0 && o.maxPerEngine <= 0 {
		return "", true
	}
	total, perEngine := o.inFlightCounts()
	return o.fits(total, perEngine, task)
}

// fits reports whether one more task can start given an occupancy tally, and if
// not, which cap refused it.
func (o *Orchestrator) fits(total int, perEngine map[models.Engine]int, task *models.Task) (string, bool) {
	if o.maxParallel > 0 && total >= o.maxParallel {
		return "max_parallel", false
	}
	if o.maxPerEngine > 0 && perEngine[o.effectiveEngine(task)] >= o.maxPerEngine {
		return "max_per_engine", false
	}
	return "", true
}

// inFlightCounts returns the number of tasks currently occupying a slot, in
// total and per engine. Both running tasks and tasks already claimed by a
// dispatcher but not yet running count, so a burst of simultaneous readiness
// cannot overshoot the limit.
func (o *Orchestrator) inFlightCounts() (int, map[models.Engine]int) {
	tasks, err := o.store.List(store.ListFilter{
		Status: []models.TaskStatus{models.TaskStatusRunning, models.TaskStatusPending},
	})
	if err != nil {
		return 0, nil
	}

	now := time.Now()
	total := 0
	perEngine := make(map[models.Engine]int)
	for _, t := range tasks {
		// A pending task occupies a slot only while a live claim is starting it.
		if t.Status == models.TaskStatusPending && !t.ClaimActive(now) {
			continue
		}
		total++
		perEngine[o.effectiveEngine(t)]++
	}
	return total, perEngine
}

// effectiveEngine resolves the engine a task will actually run on, so per-engine
// caps count tasks that inherited the default engine under that default.
func (o *Orchestrator) effectiveEngine(task *models.Task) models.Engine {
	if task != nil && task.Engine != "" {
		return task.Engine
	}
	return o.defaultEngine
}

// logTaskDeferred records a task held back by a concurrency cap.
func logTaskDeferred(task *models.Task, reason string) {
	log.Printf("task_event=dispatch_deferred task_id=%s engine=%s priority=%d cap=%s",
		task.ID, task.Engine, task.Priority, reason)
}
