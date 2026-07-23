// Package orchestrator coordinates agent tasks and dependencies.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/mesnada/agent"
	"github.com/digiogithub/pando/internal/mesnada/breaker"
	"github.com/digiogithub/pando/internal/mesnada/conclusion"
	"github.com/digiogithub/pando/internal/mesnada/config"
	"github.com/digiogithub/pando/internal/mesnada/events"
	"github.com/digiogithub/pando/internal/mesnada/persona"
	"github.com/digiogithub/pando/internal/mesnada/store"
	"github.com/digiogithub/pando/pkg/mesnada/models"
	"github.com/google/uuid"
)

// Orchestrator coordinates the execution of CLI agents.
type Orchestrator struct {
	store          store.Store
	manager        *agent.Manager
	personaManager *persona.Manager
	subscribers    map[string][]chan *models.Task
	subMu          sync.RWMutex
	// completionSubs are global "any task completed" subscribers (unlike
	// subscribers, which are keyed by task id and consumed by Wait). They are used
	// by the delegation supervisor to react to every completion. Guarded by
	// completionMu.
	completionSubs       []chan *models.Task
	completionMu         sync.Mutex
	maxParallel          int
	defaultMCPConfig     string
	defaultEngine        models.Engine
	pandoMCPServers      []agent.PandoMCPServerEntry
	gatewayExposeEnabled bool
	dynamicMCPDir        string // base dir for dynamic MCP config temp files
	delegation           DelegationConfig
	projectResolver      conclusion.ProjectResolver
	memoryRefValidator   conclusion.MemoryRefValidator
	warmResolver         WarmTargetResolver
	projectRefResolver   ProjectRefResolver
	// externalRecoverer recovers interrupted external delegations after a parent
	// restart (A2). When non-nil, warm attempts persist an in-flight breadcrumb and
	// recoverStaleTasks tries to recover external peers' results instead of bluntly
	// failing. nil (the default) preserves today's mark-failed behavior exactly.
	externalRecoverer ExternalDelegationRecoverer
	// awaitIntents records, per parent session id, the active non-blocking await
	// intent registered by the mesnada_await tool (see await.go). Guarded by
	// awaitMu; in-memory only (mirrors the supervisor's in-memory batch state).
	awaitIntents map[string]*AwaitIntent
	awaitMu      sync.Mutex
	// metrics records delegation routing/re-entry counters for the orchestrator's
	// lifetime (item E1). Always non-nil after New; safe for lock-free concurrent
	// updates from the warm path and the delegation supervisor.
	metrics *DelegationMetrics
	// blackboard is the sibling-coordination store (P1). Always non-nil after New.
	// Sibling delegated tasks post structured facts here (via the mesnada_note
	// tool) and read them back — the shared-state primitive Pando's DAG lacked.
	blackboard *Blackboard
	// eventLog is the durable delegation signal log (P5). nil when disabled or
	// when it could not be opened; every call site is nil-safe, so a missing log
	// degrades to the in-memory-only completion bus rather than failing.
	eventLog *events.Log
	// instanceID identifies this orchestrator as a dispatch claim owner.
	instanceID string
	// maxPerEngine caps concurrently in-flight tasks per engine (0 = unlimited),
	// so one wide fan-out cannot exhaust a single provider's quota while other
	// engines idle. maxParallel is the equivalent orchestrator-wide cap.
	maxPerEngine int
	// claimTTL bounds a dispatch claim; dispatchInterval paces the reclaim/drain
	// tick. Both fall back to the package defaults when unset.
	claimTTL         time.Duration
	dispatchInterval time.Duration
	wg               sync.WaitGroup
	ctx              context.Context
	cancel           context.CancelFunc
}

// DelegationConfig mirrors the conclusion-relevant subset of the application's
// delegation config. It is a plain struct (not an import of internal/config) so
// the orchestrator stays free of config import cycles, following the same
// pattern as ModelResolver.
type DelegationConfig struct {
	// Enabled gates the whole conclusion protocol. When false the orchestrator
	// preserves today's behavior byte-for-byte: no brief is appended and the
	// enricher is not run.
	Enabled bool
	// SynthesizeFallback enables deriving a conclusion from output/error when the
	// subagent did not emit a sentinel block.
	SynthesizeFallback bool
	// ReuseWarmInstances routes a delegated task whose project is known to a warm
	// per-project ACP instance (via WarmTargetResolver) instead of cold-spawning a
	// CLI. Master switch for warm reuse (default off); requires Enabled and a wired
	// resolver. The reuse-then-autostart policy and concurrency cap are applied by
	// the resolver adapter, not here.
	ReuseWarmInstances bool
	// ConclusionGateDisabled turns off the anti-hallucination conclusion gate,
	// which downgrades a "success" conclusion to "partial" when the artifacts or
	// memory refs it cites cannot be found. Default (false) = gate active
	// whenever Enabled is true.
	ConclusionGateDisabled bool
	// BreakerDisabled turns off the circuit breaker and respawn guard, which
	// refuse a Relaunch/Retry that has hit the consecutive-failure limit, is
	// auth-blocked, or is inside a rate-limit cooldown. Unlike the rest of this
	// struct the breaker does not require Enabled: it guards the plain task
	// respawn paths too. Default (false) = breaker active.
	BreakerDisabled bool
	// MaxTaskRetries is the consecutive-failure limit before the breaker trips.
	// 0 falls back to breaker.DefaultMaxRetries.
	MaxTaskRetries int
	// RateLimitCooldown defers a respawn after a quota wall. 0 falls back to
	// breaker.DefaultRateLimitCooldown.
	RateLimitCooldown time.Duration
	// RecentSuccessWindow suppresses a redundant re-run of a task that just
	// succeeded. 0 falls back to breaker.DefaultRecentSuccessWindow.
	RecentSuccessWindow time.Duration
	// EventLogDisabled turns off the durable delegation event log. When it is on
	// (the default), every terminal outcome is appended to disk before being
	// broadcast, so a signal dropped by the non-blocking in-memory bus — or lost
	// to a restart — is still delivered from the log. Unlike the rest of this
	// struct the log does not require Enabled: it records the lifecycle of every
	// task, not just delegated ones.
	EventLogDisabled bool
	// EventLogMaxEntries bounds the retained log; 0 falls back to
	// events.DefaultMaxEntries.
	EventLogMaxEntries int
	// BlackboardMaxEntriesPerSwarm bounds one swarm's blackboard append log; 0
	// falls back to DefaultBlackboardMaxEntriesPerSwarm. Winning (last-write-wins)
	// entries are always retained, so GC only sheds superseded history.
	BlackboardMaxEntriesPerSwarm int
	// BlackboardTTL purges a swarm's board once its newest entry is older than
	// this; 0 falls back to DefaultBlackboardTTL.
	BlackboardTTL time.Duration
}

// Config holds orchestrator configuration.
type Config struct {
	StorePath string
	LogDir    string
	// EnginesDir is the directory scanned at startup for *.template.yaml custom
	// engine files. When empty the manager derives it from LogDir.
	EnginesDir string
	// MaxParallel caps how many tasks the orchestrator keeps in flight at once
	// (0 = unlimited). Tasks over the cap stay pending and are dispatched by the
	// dispatch tick as slots free up.
	MaxParallel int
	// MaxPerEngine caps in-flight tasks per engine (0 = unlimited), so a wide
	// fan-out onto one provider cannot starve the others.
	MaxPerEngine int
	// ClaimTTL bounds a dispatch claim; DispatchInterval paces the reclaim/drain
	// tick. Both fall back to the package defaults when zero.
	ClaimTTL         time.Duration
	DispatchInterval time.Duration
	// DefaultMCPConfig is an optional explicit override for the MCP config file
	// passed to subagents. When empty (the default), pando builds a dynamic
	// config at spawn time that includes pando itself as an MCP server plus all
	// configured MCP servers.
	DefaultMCPConfig string
	DefaultEngine    string
	PersonaPath      string
	AppConfig        *mesnadaconfig.Config // Full app config for passing to managers
	// MCPServers lists the MCP servers configured in pando that should be
	// forwarded to subagents. Populated from the pando application config.
	MCPServers []agent.PandoMCPServerEntry
	// GatewayExposeEnabled indicates that MCPGateway re-exports all configured
	// MCP servers through pando's own MCP server. When true, the individual
	// MCPServers entries are not forwarded separately (they are already
	// accessible via the "pando" MCP server entry).
	GatewayExposeEnabled bool
	// ModelResolver converts a model ID (possibly empty or shorthand) into the
	// full "provider.model" string expected by the pando CLI's -m flag.
	// When nil, model IDs are forwarded as-is to the pando CLI spawner.
	ModelResolver func(string) string
	// Delegation carries the conclusion-protocol options. When Delegation.Enabled
	// is false (the default) the orchestrator behaves exactly as before.
	Delegation DelegationConfig
	// ProjectResolver maps a canonical project path to its registry id and display
	// name, used by the conclusion enricher. When nil the enricher derives the
	// display name from filepath.Base(projectPath).
	ProjectResolver conclusion.ProjectResolver
	// MemoryRefValidator resolves a conclusion's memory_refs for the
	// anti-hallucination gate. When nil (the default) the gate verifies only
	// filesystem artifacts and never downgrades on a memory ref.
	MemoryRefValidator conclusion.MemoryRefValidator
	// WarmTargetResolver routes delegated tasks to warm per-project ACP instances
	// (Phase 7.3). When nil (or Delegation.ReuseWarmInstances is false) every task
	// takes the cold subprocess path, preserving today's behavior.
	WarmTargetResolver WarmTargetResolver
	// ProjectRefResolver resolves a free-form project reference supplied to the
	// spawn tool (item B1) to a registered project id/path. When nil the spawn
	// tool's optional "project" argument is rejected as unsupported.
	ProjectRefResolver ProjectRefResolver
	// ExternalDelegationRecoverer recovers interrupted external delegations after a
	// parent restart (A2). Wired only when the caller opted into external warm
	// targets (AllowExternalWarmTargets); nil (the default) keeps recoverStaleTasks
	// marking interrupted tasks failed exactly as before.
	ExternalDelegationRecoverer ExternalDelegationRecoverer
}

// New creates a new Orchestrator.
func New(cfg Config) (*Orchestrator, error) {
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 5
	}

	fileStore, err := store.NewFileStore(cfg.StorePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Parse default engine
	defaultEngine := models.Engine(cfg.DefaultEngine)
	if !models.ValidEngine(defaultEngine) {
		defaultEngine = models.DefaultEngine()
	}

	// Initialize persona manager
	personaManager, err := persona.NewManager(cfg.PersonaPath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create persona manager: %w", err)
	}

	// Resolve the base directory for dynamic MCP temp files.
	dynamicMCPDir := cfg.LogDir
	if dynamicMCPDir == "" {
		home, _ := os.UserHomeDir()
		dynamicMCPDir = home + "/.mesnada/logs"
	}

	// Blackboard lives beside the task store so it shares the store's lifecycle
	// and directory. A load failure is non-fatal: coordination degrades to none
	// rather than blocking the orchestrator from starting.
	blackboardDir := cfg.StorePath
	if info, statErr := os.Stat(cfg.StorePath); statErr != nil || !info.IsDir() {
		blackboardDir = filepath.Dir(cfg.StorePath)
	}
	blackboardLimits := WithBlackboardLimits(cfg.Delegation.BlackboardMaxEntriesPerSwarm, cfg.Delegation.BlackboardTTL)
	blackboard, err := NewBlackboard(filepath.Join(blackboardDir, "blackboard.json"), blackboardLimits)
	if err != nil {
		log.Printf("Warning: failed to open swarm blackboard: %v", err)
		blackboard, _ = NewBlackboard("", blackboardLimits) // in-memory fallback
	}

	// Durable delegation event log (P5), alongside the store and blackboard. A
	// failure to open leaves eventLog nil, which every call site tolerates: the
	// completion bus keeps working, it just loses its crash-safe backing.
	var eventLog *events.Log
	if !cfg.Delegation.EventLogDisabled {
		eventLog, err = events.Open(filepath.Join(blackboardDir, "events.jsonl"), cfg.Delegation.EventLogMaxEntries)
		if err != nil {
			log.Printf("Warning: failed to open delegation event log: %v", err)
			eventLog = nil
		}
	}

	o := &Orchestrator{
		store:                fileStore,
		personaManager:       personaManager,
		blackboard:           blackboard,
		eventLog:             eventLog,
		instanceID:           fmt.Sprintf("orch-%s", uuid.New().String()[:8]),
		maxPerEngine:         cfg.MaxPerEngine,
		claimTTL:             cfg.ClaimTTL,
		dispatchInterval:     cfg.DispatchInterval,
		subscribers:          make(map[string][]chan *models.Task),
		maxParallel:          cfg.MaxParallel,
		defaultMCPConfig:     cfg.DefaultMCPConfig,
		defaultEngine:        defaultEngine,
		pandoMCPServers:      cfg.MCPServers,
		gatewayExposeEnabled: cfg.GatewayExposeEnabled,
		dynamicMCPDir:        dynamicMCPDir,
		delegation:           cfg.Delegation,
		projectResolver:      cfg.ProjectResolver,
		memoryRefValidator:   cfg.MemoryRefValidator,
		warmResolver:         cfg.WarmTargetResolver,
		projectRefResolver:   cfg.ProjectRefResolver,
		externalRecoverer:    cfg.ExternalDelegationRecoverer,
		metrics:              &DelegationMetrics{},
		ctx:                  ctx,
		cancel:               cancel,
	}

	o.manager = agent.NewManager(cfg.AppConfig, cfg.LogDir, o.onTaskComplete, o.SetProgress, cfg.ModelResolver)

	// Recover any tasks that were left in running state from a previous run.
	o.recoverStaleTasks()

	// Dispatch tick: reclaims stranded claims and drains tasks deferred by a
	// concurrency cap. Started last so it never observes a half-built instance.
	o.wg.Add(1)
	go o.dispatchLoop()

	return o, nil
}

// appendEvent records a delegation signal in the durable log. It is nil-safe and
// never fails the caller: an event log that cannot be written is a telemetry
// problem, not a reason to derail task handling.
func (o *Orchestrator) appendEvent(e events.Event) {
	if o == nil || o.eventLog == nil {
		return
	}
	if _, err := o.eventLog.Append(e); err != nil {
		log.Printf("Warning: failed to append %s event for task %s: %v", e.Kind, e.TaskID, err)
	}
}

// UnseenEvents returns the durable events a subscription has not acked yet.
// Consumers (the delegation supervisor) drain this in order and Ack each event
// once handled, which is what makes signal delivery survive a restart.
func (o *Orchestrator) UnseenEvents(subID string) []events.Event {
	if o == nil {
		return nil
	}
	return o.eventLog.Unseen(subID)
}

// AckEvent advances a subscription's durable cursor past seq.
func (o *Orchestrator) AckEvent(subID string, seq int64) error {
	if o == nil {
		return nil
	}
	return o.eventLog.Ack(subID, seq)
}

// RecentEvents returns up to limit of the most recent durable events, oldest
// first. For inspection surfaces only.
func (o *Orchestrator) RecentEvents(limit int) []events.Event {
	if o == nil {
		return nil
	}
	return o.eventLog.Recent(limit)
}

// EventLogEnabled reports whether a durable event log is backing this
// orchestrator, so consumers can choose the log-driven path over the
// best-effort in-memory bus.
func (o *Orchestrator) EventLogEnabled() bool {
	return o != nil && o.eventLog != nil
}

// terminalEventKind maps a terminal task status to its event kind, reporting
// false for a non-terminal status.
func terminalEventKind(status models.TaskStatus) (events.Kind, bool) {
	switch status {
	case models.TaskStatusCompleted:
		return events.KindCompleted, true
	case models.TaskStatusFailed:
		return events.KindFailed, true
	case models.TaskStatusCancelled:
		return events.KindCancelled, true
	}
	return "", false
}

// recordTerminalEvent appends the durable signal for a task that just reached a
// terminal state. It runs after the conclusion is captured so the event carries
// the final summary.
func (o *Orchestrator) recordTerminalEvent(task *models.Task) {
	if task == nil {
		return
	}
	kind, ok := terminalEventKind(task.Status)
	if !ok {
		return
	}
	detail := task.Error
	if task.Conclusion != nil && task.Conclusion.Summary != "" {
		detail = task.Conclusion.Summary
	}
	o.appendEvent(events.Event{
		Kind:            kind,
		TaskID:          task.ID,
		ParentSessionID: task.ParentSessionID,
		CorrelationID:   task.CorrelationID,
		Detail:          truncateForLog(detail, 500),
	})
}

// Bounds for polling an external peer whose recovered delegation is still running
// when recovery starts (A2). Kept local (the orchestrator's DelegationConfig does
// not carry the supervisor's ResurrectionTimeout) and short, since recovery runs
// right after a restart.
const (
	externalReattachPollTimeout  = 2 * time.Minute
	externalReattachPollInterval = 5 * time.Second
)

// recoverStaleTasks reconciles tasks left in "running" state by a crash or
// ungraceful shutdown. By default each is marked failed (the process that was
// running it is gone). When external-delegation recovery is enabled (A2) and a
// stale task looks like an interrupted external delegation, recovery is attempted
// in the background against the surviving peer instead — the task is only failed
// if the peer cannot produce a result.
func (o *Orchestrator) recoverStaleTasks() {
	tasks, err := o.store.List(store.ListFilter{
		Status: []models.TaskStatus{models.TaskStatusRunning},
	})
	if err != nil {
		log.Printf("Warning: failed to list running tasks for recovery: %v", err)
		return
	}

	var failed, reattaching int
	for _, task := range tasks {
		if o.externalRecoverer != nil && isRecoverableExternalDelegation(task) {
			reattaching++
			o.wg.Add(1)
			go func(t *models.Task) {
				defer o.wg.Done()
				o.recoverExternalDelegation(t)
			}(task)
			continue
		}
		o.markStaleTaskFailed(task)
		failed++
	}

	if failed > 0 {
		log.Printf("task_recovery: marked %d stale running task(s) as failed", failed)
	}
	if reattaching > 0 {
		log.Printf("task_recovery: attempting external re-attach for %d interrupted delegation(s)", reattaching)
	}
}

// isRecoverableExternalDelegation reports whether a stale running task is an
// interrupted warm/external delegation that recovery should try against a peer:
// it carries the warm-acp engine breadcrumb plus the correlation id and project
// reference needed to locate and query the peer. Cold tasks (their own engine)
// never match, so they keep the mark-failed path.
func isRecoverableExternalDelegation(task *models.Task) bool {
	return task != nil &&
		task.Engine == models.EngineWarmACP &&
		task.CorrelationID != "" &&
		(task.ProjectPath != "" || task.ProjectID != "")
}

// markStaleTaskFailed marks one interrupted task failed (the pre-A2 behaviour).
func (o *Orchestrator) markStaleTaskFailed(task *models.Task) {
	log.Printf("task_event=recovering_stale task_id=%s - marking as failed (process interrupted)", task.ID)
	task.Status = models.TaskStatusFailed
	task.Error = "task was interrupted (process died or pando was restarted)"
	now := time.Now()
	task.CompletedAt = &now
	if err := o.store.Save(task); err != nil {
		log.Printf("Warning: failed to save recovered task %s: %v", task.ID, err)
	}
}

// recoverExternalDelegation tries to recover one interrupted external delegation
// from its surviving peer (A2). On success it drives the normal completion path so
// the conclusion pipeline / supervisor re-enters the parent loop exactly as for an
// in-process run; otherwise it falls back to marking the task failed.
func (o *Orchestrator) recoverExternalDelegation(task *models.Task) {
	res, state, err := o.externalRecoverer.RecoverExternalDelegation(o.ctx, task.ProjectID, task.ProjectPath, task.CorrelationID)

	if err == nil && state == RecoveryRunning {
		// The peer is still running the delegation: poll briefly for completion.
		if r, ok := o.pollExternalRecovery(task); ok {
			res, state, err = r, RecoveryCompleted, nil
		}
	}

	if err == nil && state == RecoveryCompleted && res != nil {
		o.completeRecoveredDelegation(task, res)
		return
	}

	// Unreachable / unknown / declined / poll-exhausted: mark failed as before.
	o.markStaleTaskFailed(task)
	o.metrics.recordExternalReattachFailed()
	log.Printf("task_event=external_reattach_failed task_id=%s project_path=%q", task.ID, task.ProjectPath)
}

// pollExternalRecovery re-queries the peer up to externalReattachPollTimeout for a
// delegation that was still running when recovery began. It returns the recovered
// result and ok=true once the peer reports completion, or ok=false on timeout /
// ctx cancellation / the peer becoming unreachable.
func (o *Orchestrator) pollExternalRecovery(task *models.Task) (*WarmRunResult, bool) {
	deadline := time.Now().Add(externalReattachPollTimeout)
	ticker := time.NewTicker(externalReattachPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-o.ctx.Done():
			return nil, false
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, false
			}
			res, state, err := o.externalRecoverer.RecoverExternalDelegation(o.ctx, task.ProjectID, task.ProjectPath, task.CorrelationID)
			if err != nil {
				return nil, false
			}
			switch state {
			case RecoveryCompleted:
				if res != nil {
					return res, true
				}
				return nil, false
			case RecoveryRunning:
				continue
			default: // RecoveryUnknown
				return nil, false
			}
		}
	}
}

// completeRecoveredDelegation writes the recovered result onto the task and drives
// the standard completion path (captureConclusion + broadcast via onTaskComplete),
// so a recovered external delegation re-enters the parent loop identically to an
// in-process warm completion.
func (o *Orchestrator) completeRecoveredDelegation(task *models.Task, res *WarmRunResult) {
	now := time.Now()
	task.Engine = models.EngineWarmACP
	task.Status = models.TaskStatusCompleted
	task.Output = res.Output
	task.ACPSessionID = res.ChildSessionID
	task.CompletedAt = &now
	exit := 0
	task.ExitCode = &exit
	o.metrics.recordExternalReattachRecovered()
	log.Printf("task_event=external_reattach_recovered task_id=%s child_session=%q output_len=%d",
		task.ID, res.ChildSessionID, len(res.Output))
	o.onTaskComplete(task)
}

func (o *Orchestrator) onTaskComplete(task *models.Task) {
	// Save final state
	o.store.Save(task)
	logTaskFinished(task)

	// Capture + enrich the delegated-task conclusion (default-off). Nothing
	// consumes it yet (Cases A/B are later phases); this only persists it on the
	// task. Kept side-effect-light and panic-safe so completion stays robust.
	o.captureConclusion(task)

	// Update the circuit-breaker state from this terminal outcome (a success
	// resets the counter, a failure increments and classifies it). Runs after the
	// conclusion is captured so the classifier can read it, and before the
	// broadcast so subscribers observe the up-to-date counters.
	o.recordBreakerOutcome(task)

	// The task no longer occupies a dispatch slot: drop its claim so the caps see
	// the freed capacity and the next drain can use it.
	o.releaseClaim(task.ID)

	// Durable signal (P5) BEFORE the in-memory broadcast: the log is the record
	// of what happened, the broadcast is only a best-effort wakeup. Appending
	// first means a subscriber that misses the (non-blocking, droppable) send
	// still finds the event waiting for it.
	o.recordTerminalEvent(task)

	// Broadcast to global completion subscribers (the delegation supervisor). Done
	// AFTER captureConclusion so the delivered *Task already carries its enriched
	// Conclusion. Non-blocking send: a slow/full subscriber drops the event rather
	// than stalling completion handling (mirrors the per-task notify below).
	o.broadcastCompletion(task)

	// Clean up dynamic MCP config temp dir for this task (if one was generated).
	if o.dynamicMCPDir != "" {
		dynDir := o.dynamicMCPDir + "/mcp-dynamic/" + task.ID
		os.RemoveAll(dynDir)
	}

	// Notify subscribers
	o.subMu.RLock()
	subs := o.subscribers[task.ID]
	o.subMu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- task:
		default:
		}
	}

	// Clean up subscribers
	o.subMu.Lock()
	delete(o.subscribers, task.ID)
	o.subMu.Unlock()

	// Check for dependent tasks
	o.processDependentTasks(task)
}

// SubscribeCompletions registers a global subscriber that receives every task
// that reaches a terminal state via onTaskComplete (after its conclusion has been
// captured). It returns a receive-only channel and an unsubscribe function that
// removes the channel; the unsubscribe function is idempotent. The channel is
// buffered; deliveries are non-blocking so a slow consumer drops events rather
// than stalling the orchestrator. This is the hook the delegation supervisor uses
// for Case A (inject into a live parent loop) and, later, Case B (resurrection).
func (o *Orchestrator) SubscribeCompletions() (<-chan *models.Task, func()) {
	ch := make(chan *models.Task, 16)

	o.completionMu.Lock()
	o.completionSubs = append(o.completionSubs, ch)
	o.completionMu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			o.completionMu.Lock()
			for i, c := range o.completionSubs {
				if c == ch {
					o.completionSubs = append(o.completionSubs[:i], o.completionSubs[i+1:]...)
					break
				}
			}
			o.completionMu.Unlock()
			close(ch)
		})
	}
	return ch, unsubscribe
}

// broadcastCompletion delivers a completed task to all global completion
// subscribers with a non-blocking send (drop if the buffer is full).
func (o *Orchestrator) broadcastCompletion(task *models.Task) {
	o.completionMu.Lock()
	subs := make([]chan *models.Task, len(o.completionSubs))
	copy(subs, o.completionSubs)
	o.completionMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- task:
		default:
		}
	}
}

// captureConclusion runs the conclusion enricher for a completed task when
// delegation is enabled, stores the result on the task, and persists it again.
// It never panics: completion handling must stay robust regardless of parser or
// store failures.
func (o *Orchestrator) captureConclusion(task *models.Task) {
	if !o.delegation.Enabled || task == nil {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Warning: conclusion capture panicked for task %s: %v", task.ID, r)
		}
	}()

	c := conclusion.Enrich(task, conclusion.DelegationOptions{
		SynthesizeFallback: o.delegation.SynthesizeFallback,
	}, o.projectResolver)
	if c == nil {
		return
	}

	// Anti-hallucination gate: a subagent may not claim full success while citing
	// artifacts or memory refs that do not exist. Verify downgrades the status to
	// "partial" and records a warning; it never discards data. This also protects
	// the swarm verifier gate, which passes only on status "success".
	if res := conclusion.Verify(c, conclusion.VerifyOptions{
		Disabled: o.delegation.ConclusionGateDisabled,
		BaseDir:  task.WorkDir,
		Memory:   o.memoryRefValidator,
	}); res.Downgraded {
		o.metrics.recordConclusionGateDowngrade()
		logConclusionGateDowngraded(task, res)
	}

	task.Conclusion = c
	if err := o.store.Save(task); err != nil {
		log.Printf("Warning: failed to persist conclusion for task %s: %v", task.ID, err)
	}
}

// breakerOptions builds the guard options from the delegation config, applying
// the package defaults for unset values.
func (o *Orchestrator) breakerOptions() breaker.Options {
	opts := breaker.Options{
		Disabled:            o.delegation.BreakerDisabled,
		MaxRetries:          o.delegation.MaxTaskRetries,
		RateLimitCooldown:   o.delegation.RateLimitCooldown,
		RecentSuccessWindow: o.delegation.RecentSuccessWindow,
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = breaker.DefaultMaxRetries
	}
	if opts.RateLimitCooldown == 0 {
		opts.RateLimitCooldown = breaker.DefaultRateLimitCooldown
	}
	if opts.RecentSuccessWindow == 0 {
		opts.RecentSuccessWindow = breaker.DefaultRecentSuccessWindow
	}
	return opts
}

// recordBreakerOutcome folds a terminal execution into the task's breaker state
// and persists it. Panic-safe like captureConclusion: completion handling must
// never be derailed by bookkeeping.
func (o *Orchestrator) recordBreakerOutcome(task *models.Task) {
	if task == nil || o.delegation.BreakerDisabled {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Warning: breaker bookkeeping panicked for task %s: %v", task.ID, r)
		}
	}()

	opts := o.breakerOptions()
	tripped := breaker.RecordOutcome(task, opts, time.Now())
	if tripped {
		o.metrics.recordBreakerTripped()
		logBreakerTripped(task, opts.MaxRetries)
		o.appendEvent(events.Event{
			Kind:            events.KindBreakerTripped,
			TaskID:          task.ID,
			ParentSessionID: task.ParentSessionID,
			CorrelationID:   task.CorrelationID,
			Detail: fmt.Sprintf("consecutive_failures=%d limit=%d kind=%s",
				task.ConsecutiveFailures, opts.MaxRetries, task.LastFailureKind),
		})
	}
	if err := o.store.Save(task); err != nil {
		log.Printf("Warning: failed to persist breaker state for task %s: %v", task.ID, err)
	}
}

// RespawnDecision reports whether a task may currently be re-executed, without
// re-executing it. Surfaces the circuit breaker and respawn guard to callers
// (tools, UI) so they can explain a refusal before attempting a Relaunch/Retry.
func (o *Orchestrator) RespawnDecision(taskID string) (breaker.Decision, error) {
	task, err := o.store.Get(taskID)
	if err != nil {
		return breaker.Decision{}, err
	}
	return breaker.Guard(task, o.breakerOptions(), time.Now()), nil
}

// guardRespawn applies the circuit breaker / respawn guard to a re-execution
// attempt, returning an error when it is refused. force bypasses the guard (an
// explicit human/agent override), which is always logged.
func (o *Orchestrator) guardRespawn(task *models.Task, op string, force bool) error {
	decision := breaker.Guard(task, o.breakerOptions(), time.Now())
	if decision.Allow {
		return nil
	}
	if force {
		logRespawnForced(task, op, decision)
		return nil
	}
	o.metrics.recordRespawnRefused()
	logRespawnRefused(task, op, decision)
	o.appendEvent(events.Event{
		Kind:            events.KindRespawnRefused,
		TaskID:          task.ID,
		ParentSessionID: task.ParentSessionID,
		CorrelationID:   task.CorrelationID,
		Detail:          fmt.Sprintf("%s refused: %s", op, decision.Reason),
	})
	return fmt.Errorf("%s refused for task %s: %s", op, task.ID, decision.Error())
}

func (o *Orchestrator) processDependentTasks(completed *models.Task) {
	if completed.Status != models.TaskStatusCompleted {
		return
	}

	// Find tasks waiting on this one
	tasks, _ := o.store.List(store.ListFilter{
		Status: []models.TaskStatus{models.TaskStatusPending},
	})

	for _, task := range tasks {
		if o.canStart(task) {
			logTaskStartable(task, fmt.Sprintf("dependency_completed=%s", completed.ID))
			// Admission control + claim: two dependencies completing at the same
			// instant both reach this point for the same dependent, and only the
			// dispatcher that wins the claim starts it.
			o.tryDispatch(task)
			continue
		}
		// A dependent held pending purely by a failed gate (the verifier completed
		// without passing): leave it pending and emit a gate_failed signal. The
		// verifier's own conclusion (status=blocked/failed) propagates up the
		// completion bus so the parent agent can adjust and Relaunch the verifier;
		// its next completion re-runs this evaluation and re-opens the gate.
		if gateBlocks(task, completed) {
			summary := ""
			if completed.Conclusion != nil {
				summary = completed.Conclusion.Summary
			}
			logTaskGateFailed(task, completed.ID, summary)
			o.appendEvent(events.Event{
				Kind:            events.KindGateFailed,
				TaskID:          task.ID,
				ParentSessionID: task.ParentSessionID,
				CorrelationID:   task.CorrelationID,
				Detail:          fmt.Sprintf("gate dependency %s did not pass: %s", completed.ID, truncateForLog(summary, 300)),
			})
		}
	}
}

func (o *Orchestrator) canStart(task *models.Task) bool {
	if len(task.Dependencies) == 0 {
		return true
	}

	gate := gateSet(task.GateDeps)
	for _, depID := range task.Dependencies {
		dep, err := o.store.Get(depID)
		if err != nil {
			return false
		}
		if dep.Status != models.TaskStatusCompleted {
			return false
		}
		// A gate dependency must not only be completed but also PASS its
		// conclusion gate. A completed-but-not-passing gate dep keeps this task
		// pending (fail-closed) — gate-fail is surfaced separately via a signal.
		if _, isGate := gate[depID]; isGate && !conclusionPasses(dep) {
			return false
		}
	}

	return true
}

// gateSet builds a lookup set of gated dependency ids.
func gateSet(gateDeps []string) map[string]struct{} {
	if len(gateDeps) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(gateDeps))
	for _, id := range gateDeps {
		m[id] = struct{}{}
	}
	return m
}

// conclusionPasses reports whether a completed dependency passed its verifier
// gate. Fail-closed: a missing conclusion never passes. Only status "success"
// passes; "partial"/"failed"/"blocked" do not (verifiers signal a pass by closing
// with status=success, matching the swarm verifier brief).
func conclusionPasses(dep *models.Task) bool {
	return dep != nil && dep.Conclusion != nil && dep.Conclusion.Status == "success"
}

// gateBlocks reports whether completed is a gate dependency of task that did NOT
// pass — i.e. task is being held pending purely by a failed gate.
func gateBlocks(task *models.Task, completed *models.Task) bool {
	for _, id := range task.GateDeps {
		if id == completed.ID {
			return !conclusionPasses(completed)
		}
	}
	return false
}

func (o *Orchestrator) startTask(task *models.Task) {
	// Defensive: an orchestrator without a manager can never spawn. Mark the task
	// failed rather than dereferencing a nil manager. This never happens in
	// production (New always wires a manager) but keeps the goroutine safe when a
	// test constructs the orchestrator directly.
	if o.manager == nil {
		task.Status = models.TaskStatusFailed
		task.Error = "orchestrator has no spawn manager"
		now := time.Now()
		task.CompletedAt = &now
		// Terminal like any other failure, so it signals like any other failure —
		// otherwise the task dies silently, still holding its dispatch claim.
		o.store.Save(task)
		o.onTaskComplete(task)
		return
	}

	// Inject swarm coordination context (P1 blackboard pointer + shared facts, P2
	// dependency conclusions) right before spawning — deps are guaranteed complete
	// at start time, unlike Spawn. Gated on delegation (Conclusions are only
	// captured then) and guarded by a marker so retries never double-append.
	if o.delegation.Enabled && !strings.Contains(task.Prompt, swarmContextMarker) {
		if block := o.buildSwarmContext(task); block != "" {
			task.Prompt = task.Prompt + "\n\n" + block
			o.store.Save(task)
		}
	}

	// Warm-target routing (Phase 7.3): when enabled and the task targets a known
	// project, try running it inside an already-running ("warm") per-project ACP
	// instance instead of cold-spawning a CLI. tryStartWarm drives completion
	// itself when it handles the task; it returns false (task untouched) to fall
	// back to the cold path.
	if o.tryStartWarm(task) {
		return
	}

	if err := o.manager.Spawn(o.ctx, task); err != nil {
		task.Status = models.TaskStatusFailed
		task.Error = err.Error()
		now := time.Now()
		task.CompletedAt = &now
		// A spawn failure is a terminal outcome like any other, so it goes through
		// the normal completion path: without it the task would end failed while
		// emitting no signal at all — no durable event, no broadcast — and a parent
		// waiting on it would never learn that it is never coming back.
		o.store.Save(task)
		o.onTaskComplete(task)
		return
	}
	o.store.Save(task)
}

// getDependencyLogs retrieves the last N lines from the log files of dependency tasks.
func (o *Orchestrator) getDependencyLogs(dependencies []string, numLines int) (string, error) {
	if len(dependencies) == 0 {
		return "", nil
	}

	var logsBuilder strings.Builder
	logsBuilder.WriteString("===LAST TASK RESULTS===\n\n")

	for _, depID := range dependencies {
		dep, err := o.store.Get(depID)
		if err != nil {
			log.Printf("Warning: failed to get dependency task %s: %v", depID, err)
			continue
		}

		if dep.LogFile == "" {
			log.Printf("Warning: dependency task %s has no log file", depID)
			continue
		}

		// Read the log file
		content, err := os.ReadFile(dep.LogFile)
		if err != nil {
			log.Printf("Warning: failed to read log file %s: %v", dep.LogFile, err)
			continue
		}

		// Split into lines and get the last N lines
		lines := strings.Split(string(content), "\n")
		startIdx := 0
		if len(lines) > numLines {
			startIdx = len(lines) - numLines
		}

		logsBuilder.WriteString(fmt.Sprintf("--- Task: %s ---\n", depID))
		logsBuilder.WriteString(strings.Join(lines[startIdx:], "\n"))
		logsBuilder.WriteString("\n\n")
	}

	return logsBuilder.String(), nil
}

// swarmContextMarker delimits the injected swarm-coordination block so startTask
// never double-appends it (retry / relaunch reuse the same *Task).
const swarmContextMarker = "===SWARM CONTEXT==="

// swarmKeyForTask resolves the shared coordination id for a task's swarm. Sibling
// tasks spawned by the same parent agent session share ParentSessionID, so that is
// the primary key; ParentTaskID (nested delegations) and CorrelationID are
// fallbacks. Returns "" when the task has no correlation and therefore no swarm.
func (o *Orchestrator) swarmKeyForTask(task *models.Task) string {
	switch {
	case task.ParentSessionID != "":
		return task.ParentSessionID
	case task.ParentTaskID != "":
		return task.ParentTaskID
	case task.CorrelationID != "":
		return task.CorrelationID
	default:
		return ""
	}
}

// PostNote records a structured fact on a swarm's shared blackboard (P1). Thin
// pass-through used by the mesnada_note tool.
func (o *Orchestrator) PostNote(swarmID, key string, value json.RawMessage, author, taskID string) error {
	if o.blackboard == nil {
		return fmt.Errorf("blackboard unavailable")
	}
	return o.blackboard.Post(swarmID, BlackboardEntry{
		Key:    key,
		Value:  value,
		Author: author,
		TaskID: taskID,
	})
}

// ListNotes returns the merged (last-write-wins) blackboard for a swarm (P1).
func (o *Orchestrator) ListNotes(swarmID string) []BlackboardEntry {
	if o.blackboard == nil {
		return nil
	}
	return o.blackboard.Latest(swarmID)
}

// getDependencyConclusions renders the structured Conclusion of each completed
// dependency (P2). Unlike getDependencyLogs (raw log tail captured at spawn), this
// forwards the enriched, model-emitted result — summary, artifacts, follow-up —
// which Pando already captures but never fed to dependents.
func (o *Orchestrator) getDependencyConclusions(dependencies []string) string {
	if len(dependencies) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, depID := range dependencies {
		dep, err := o.store.Get(depID)
		if err != nil {
			continue
		}
		c := dep.Conclusion
		if c == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("--- Dependency %s (status: %s) ---\n", depID, c.Status))
		if c.Summary != "" {
			sb.WriteString(c.Summary + "\n")
		}
		if len(c.Artifacts) > 0 {
			sb.WriteString("Artifacts: " + strings.Join(c.Artifacts, ", ") + "\n")
		}
		if len(c.MemoryRefs) > 0 {
			sb.WriteString("Memory refs: " + strings.Join(c.MemoryRefs, ", ") + "\n")
		}
		if c.FollowUp != "" {
			sb.WriteString("Follow-up: " + c.FollowUp + "\n")
		}
		sb.WriteString("\n")
	}
	if sb.Len() == 0 {
		return ""
	}
	return "===DEPENDENCY CONCLUSIONS===\n\n" + sb.String()
}

// buildSwarmContext assembles the coordination block injected into a task's prompt
// at start time (deps are guaranteed complete then, unlike Spawn): P2 dependency
// conclusions plus a P1 blackboard pointer + current shared facts. Returns "" when
// there is nothing to inject.
func (o *Orchestrator) buildSwarmContext(task *models.Task) string {
	var sb strings.Builder

	if concl := o.getDependencyConclusions(task.Dependencies); concl != "" {
		sb.WriteString(concl)
	}

	swarmID := o.swarmKeyForTask(task)
	if swarmID != "" {
		sb.WriteString("===SWARM BLACKBOARD===\n")
		sb.WriteString(fmt.Sprintf("Swarm id: %s\n", swarmID))
		sb.WriteString("Coordinate with sibling agents through the shared blackboard.\n")
		sb.WriteString(fmt.Sprintf("- Post a fact:  mesnada_note(action=\"post\", swarm_id=%q, key=..., value=...)\n", swarmID))
		sb.WriteString(fmt.Sprintf("- Read facts:   mesnada_note(action=\"list\", swarm_id=%q)\n", swarmID))
		if latest := o.blackboard.Latest(swarmID); len(latest) > 0 {
			sb.WriteString("\n" + renderLatest(swarmID, latest))
		}
		sb.WriteString("\n")
	}

	if sb.Len() == 0 {
		return ""
	}
	return swarmContextMarker + "\n\n" + sb.String()
}

// Spawn creates and optionally starts a new agent task.
func (o *Orchestrator) Spawn(ctx context.Context, req models.SpawnRequest) (*models.Task, error) {
	// Validate work directory
	workDir := req.WorkDir
	if workDir == "" {
		workDir = "."
	}

	// Parse timeout
	var timeout models.Duration
	if req.Timeout != "" {
		dur, err := time.ParseDuration(req.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout: %w", err)
		}
		timeout = models.Duration(dur)
	}

	// Apply orchestrator MCP config when not explicitly provided.
	// Priority: explicit request > persona-specific config > global override > dynamic.
	mcpConfig := req.MCPConfig
	if mcpConfig == "" {
		if req.Persona != "" {
			if personaMCPConfig := o.personaManager.GetPersonaMCPConfig(req.Persona); personaMCPConfig != "" {
				mcpConfig = personaMCPConfig
			}
		}
		if mcpConfig == "" && o.defaultMCPConfig != "" {
			// Explicit override still supported for advanced users.
			mcpConfig = o.defaultMCPConfig
		}
	}

	// Apply orchestrator default engine when not explicitly provided.
	engine := req.Engine
	if engine == "" {
		engine = o.defaultEngine
	}

	// Apply persona to prompt if specified
	prompt := req.Prompt
	if req.Persona != "" {
		prompt = o.personaManager.ApplyPersona(req.Persona, prompt)
	}

	// When delegation is enabled, append the conclusion brief as the trailing
	// instruction (after persona) so the subagent closes its run with a
	// <pando:conclusion> block. Default-off: nothing is appended otherwise.
	if o.delegation.Enabled {
		prompt = prompt + conclusion.BriefInstruction()
	}

	// Prepare the prompt with dependency logs if requested
	if req.IncludeDependencyLogs && len(req.Dependencies) > 0 {
		logLines := req.DependencyLogLines
		if logLines <= 0 {
			logLines = 100
		}

		dependencyLogs, err := o.getDependencyLogs(req.Dependencies, logLines)
		if err != nil {
			log.Printf("Warning: failed to get dependency logs: %v", err)
		} else if dependencyLogs != "" {
			prompt = prompt + "\n\n" + dependencyLogs
		}
	}

	// For ACP engines, ACPAgent is the agent identifier stored in Model.
	// If both req.Model and req.ACPAgent are set, req.Model takes precedence.
	// EnginePando runs Pando itself as a CLI subprocess (non-ACP path).
	model := req.Model
	acpAgent := req.ACPAgent
	if acpAgent != "" && model == "" {
		model = acpAgent
	}

	taskID := generateID()

	// For non-ACP engines with no explicit MCP config, generate a dynamic
	// config that always includes pando as an MCP server plus any additional
	// MCP servers configured in pando (unless already proxied via gateway).
	if mcpConfig == "" && !isACPEngine(engine) {
		dynCfg := agent.BuildSubagentMCPConfig("", o.pandoMCPServers, o.gatewayExposeEnabled)
		dynDir := o.dynamicMCPDir + "/mcp-dynamic/" + taskID
		if written, err := agent.WriteCanonicalConfigToFile(dynCfg, dynDir, "mcp-config.json"); err != nil {
			log.Printf("Warning: failed to write dynamic MCP config for task %s: %v", taskID, err)
		} else {
			mcpConfig = written
		}
	}

	task := &models.Task{
		ID:           taskID,
		Prompt:       prompt,
		WorkDir:      workDir,
		Status:       models.TaskStatusPending,
		Engine:       engine,
		Model:        model,
		Dependencies: req.Dependencies,
		GateDeps:     req.GateDeps,
		Tags:         req.Tags,
		Priority:     req.Priority,
		Timeout:      timeout,
		MCPConfig:    mcpConfig,
		ExtraArgs:    req.ExtraArgs,
		Persona:      req.Persona,
		CreatedAt:    time.Now(),
		// Delegation correlation: persisted so the task's origin (parent
		// session/task, project, depth) is durable. Populated by the spawn tool;
		// no consumer reads these yet (default-off feature).
		ParentSessionID: req.ParentSessionID,
		ParentTaskID:    req.ParentTaskID,
		CorrelationID:   req.CorrelationID,
		ProjectID:       req.ProjectID,
		ProjectPath:     req.ProjectPath,
		Depth:           req.Depth,
		// Circuit-breaker state. ConsecutiveFailures is normally zero; a Retry
		// seeds it from the previous task so the breaker accumulates across the
		// whole retry chain instead of resetting on every new replica.
		MaxRetries:          req.MaxRetries,
		ConsecutiveFailures: req.ConsecutiveFailures,
	}

	// Inherit log file from previous task (retry scenario).
	if req.LogFile != "" {
		task.LogFile = req.LogFile
	}

	logTaskReceived(task)

	// Save task
	if err := o.store.Save(task); err != nil {
		return nil, fmt.Errorf("failed to save task: %w", err)
	}

	// Check if can start immediately
	if o.canStart(task) {
		reason := "dependencies_satisfied"
		if len(task.Dependencies) == 0 {
			reason = "no_dependencies"
		}
		logTaskStartable(task, reason)
		if req.Background {
			// Background spawns are the automatic fan-out path, so they go through
			// admission control: over the cap the task stays pending and the
			// dispatch tick starts it when a slot frees.
			o.tryDispatch(task)
		} else {
			o.dispatchNow(task, false)
		}
	}

	return task, nil
}

// GetTask retrieves a task by ID.
func (o *Orchestrator) GetTask(taskID string) (*models.Task, error) {
	return o.store.Get(taskID)
}

// ListByParentSession returns all tasks correlated to the given parent agent
// session id. The delegation supervisor uses it to decide whether any sibling
// delegated tasks are still outstanding before resurrecting an idle parent loop
// (Case B). Thin pass-through to the underlying store.
func (o *Orchestrator) ListByParentSession(sessionID string) ([]*models.Task, error) {
	return o.store.ListByParentSession(sessionID)
}

// ListTasks lists tasks matching the filter.
func (o *Orchestrator) ListTasks(req models.ListRequest) ([]*models.Task, error) {
	return o.store.List(store.ListFilter{
		Status: req.Status,
		Tags:   req.Tags,
		Limit:  req.Limit,
		Offset: req.Offset,
	})
}

// Wait waits for a task to complete.
func (o *Orchestrator) Wait(ctx context.Context, taskID string, timeout time.Duration) (*models.Task, error) {
	// Check if already complete
	task, err := o.store.Get(taskID)
	if err != nil {
		return nil, err
	}

	if task.IsTerminal() {
		return task, nil
	}

	// Set up timeout context
	waitCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Subscribe to completion
	ch := make(chan *models.Task, 1)
	o.subMu.Lock()
	o.subscribers[taskID] = append(o.subscribers[taskID], ch)
	o.subMu.Unlock()

	defer func() {
		o.subMu.Lock()
		subs := o.subscribers[taskID]
		for i, sub := range subs {
			if sub == ch {
				o.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		o.subMu.Unlock()
	}()

	// Also wait on spawner in case task completes between check and subscribe
	go func() {
		o.manager.Wait(waitCtx, taskID)
		task, _ := o.store.Get(taskID)
		if task != nil && task.IsTerminal() {
			select {
			case ch <- task:
			default:
			}
		}
	}()

	select {
	case <-waitCtx.Done():
		// Return current state even on timeout
		task, _ = o.store.Get(taskID)
		return task, fmt.Errorf("timeout waiting for task %s: %w", taskID, waitCtx.Err())
	case task := <-ch:
		return task, nil
	}
}

// WaitMultiple waits for multiple tasks.
func (o *Orchestrator) WaitMultiple(ctx context.Context, taskIDs []string, waitAll bool, timeout time.Duration) (map[string]*models.Task, error) {
	results := make(map[string]*models.Task)
	var mu sync.Mutex
	var wg sync.WaitGroup

	waitCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	done := make(chan struct{})

	for _, id := range taskIDs {
		wg.Add(1)
		go func(taskID string) {
			defer wg.Done()

			task, err := o.Wait(waitCtx, taskID, 0)
			if task != nil {
				mu.Lock()
				results[taskID] = task
				mu.Unlock()
			}

			if !waitAll && err == nil && task != nil && task.IsTerminal() {
				select {
				case done <- struct{}{}:
				default:
				}
			}
		}(id)
	}

	if waitAll {
		wg.Wait()
	} else {
		select {
		case <-waitCtx.Done():
		case <-done:
		}
	}

	return results, nil
}

// Cancel cancels a running task.
func (o *Orchestrator) Cancel(taskID string) error {
	task, err := o.store.Get(taskID)
	if err != nil {
		return err
	}

	if task.IsTerminal() {
		return fmt.Errorf("task %s is already in terminal state: %s", taskID, task.Status)
	}

	if task.Status == models.TaskStatusRunning {
		if err := o.manager.Cancel(taskID); err != nil {
			return err
		}
	}

	task.Status = models.TaskStatusCancelled
	now := time.Now()
	task.CompletedAt = &now

	if err := o.store.Save(task); err != nil {
		return err
	}
	logTaskFinished(task)
	o.releaseClaim(task.ID)
	// Record the cancellation durably. It is deliberately not broadcast: a
	// cancelled task carries no conclusion, so there is nothing for a parent loop
	// to act on — the event exists so the history of the task is complete.
	o.recordTerminalEvent(task)
	return nil
}

// Pause pauses a running or pending task.
// Pausing stops the underlying Copilot process (if any) and marks the task as paused.
func (o *Orchestrator) Pause(taskID string) (*models.Task, error) {
	task, err := o.store.Get(taskID)
	if err != nil {
		return nil, err
	}

	if task.Status == models.TaskStatusPaused {
		return task, nil
	}

	if task.IsTerminal() {
		return nil, fmt.Errorf("task %s is already in terminal state: %s", taskID, task.Status)
	}

	if task.Status == models.TaskStatusRunning {
		if err := o.manager.Pause(taskID); err != nil {
			return nil, err
		}
	}

	task.Status = models.TaskStatusPaused
	now := time.Now()
	task.CompletedAt = &now

	if err := o.store.Save(task); err != nil {
		return nil, err
	}
	logTaskFinished(task)
	return task, nil
}

// ResumeOptions controls how a paused task is resumed.
type ResumeOptions struct {
	Prompt     string
	Model      string
	Background bool
	Timeout    string
	Tags       *[]string
}

// Resume creates a new task to continue work from a previously paused task.
func (o *Orchestrator) Resume(ctx context.Context, taskID string, opts ResumeOptions) (*models.Task, error) {
	prev, err := o.store.Get(taskID)
	if err != nil {
		return nil, err
	}
	if prev.Status != models.TaskStatusPaused {
		return nil, fmt.Errorf("task %s is not paused (status=%s)", taskID, prev.Status)
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	model := opts.Model
	if model == "" {
		model = prev.Model
	}

	timeout := opts.Timeout
	if timeout == "" && prev.Timeout > 0 {
		timeout = time.Duration(prev.Timeout).String()
	}

	tags := prev.Tags
	if opts.Tags != nil {
		tags = *opts.Tags
	}

	resumePrompt := fmt.Sprintf(
		"Resume work from previous task_id: %s\nPrevious task log file path: %s\n\nAdditional resume instructions:\n%s\n",
		prev.ID,
		prev.LogFile,
		strings.TrimSpace(opts.Prompt),
	)

	// Keep workdir/deps/config consistent with the paused task by default.
	return o.Spawn(ctx, models.SpawnRequest{
		Prompt:       resumePrompt,
		WorkDir:      prev.WorkDir,
		Model:        model,
		Dependencies: prev.Dependencies,
		Tags:         tags,
		Priority:     prev.Priority,
		Timeout:      timeout,
		MCPConfig:    prev.MCPConfig,
		ExtraArgs:    prev.ExtraArgs,
		Background:   opts.Background,
		// Preserve the original task's delegation correlation across resume.
		ParentSessionID: prev.ParentSessionID,
		ParentTaskID:    prev.ParentTaskID,
		CorrelationID:   prev.CorrelationID,
		ProjectID:       prev.ProjectID,
		ProjectPath:     prev.ProjectPath,
		Depth:           prev.Depth,
	})
}

// RelaunchOptions controls how a task is relaunched in-place.
type RelaunchOptions struct {
	// Prompt overrides the original prompt when non-empty.
	Prompt string
	// Engine overrides the engine when non-zero.
	Engine models.Engine
	// Model overrides the model when non-empty.
	Model string
	// Timeout overrides the timeout when non-empty.
	Timeout string
	// Background controls whether the relaunch runs in the background.
	Background bool
	// Force bypasses the circuit breaker / respawn guard. Use it when the caller
	// has actually changed something (a new prompt, engine or model) or when a
	// human explicitly insists on re-running a guarded task.
	Force bool
}

// Relaunch resets an existing task (of any status) back to pending and re-runs it.
// Unlike Retry (which creates a new task), Relaunch reuses the same task ID so that
// any dependent tasks automatically benefit from the new execution without needing
// their dependency arrays updated.
//
// Optional fields in opts override the stored task configuration; zero values keep
// the original values.
func (o *Orchestrator) Relaunch(ctx context.Context, taskID string, opts RelaunchOptions) (*models.Task, error) {
	task, err := o.store.Get(taskID)
	if err != nil {
		return nil, err
	}

	// Anti-thrash: refuse a re-run that the circuit breaker or respawn guard has
	// ruled out (limit reached, auth blocker, rate-limit cooldown, just
	// succeeded). Changing the prompt/engine/model does NOT implicitly override
	// it — pass Force so the bypass is always deliberate and logged.
	if err := o.guardRespawn(task, "relaunch", opts.Force); err != nil {
		return nil, err
	}

	// Best-effort: stop the process if it is still tracked as running.
	if task.Status == models.TaskStatusRunning {
		if err := o.manager.Cancel(taskID); err != nil {
			log.Printf("Warning: failed to cancel running task %s before relaunch: %v", taskID, err)
		}
	}

	// Apply overrides.
	if opts.Prompt != "" {
		task.Prompt = opts.Prompt
	}
	if opts.Engine != "" {
		task.Engine = opts.Engine
	}
	if opts.Model != "" {
		task.Model = opts.Model
	}
	if opts.Timeout != "" {
		dur, parseErr := time.ParseDuration(opts.Timeout)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid timeout: %w", parseErr)
		}
		task.Timeout = models.Duration(dur)
	}

	// Apply orchestrator default engine if none is set.
	if task.Engine == "" {
		task.Engine = o.defaultEngine
	}

	// Regenerate the dynamic MCP config for non-ACP engines, because the
	// previous config directory may have been cleaned up when the task finished
	// (or was never created if the task was interrupted before starting).
	if !isACPEngine(task.Engine) {
		dynPrefix := o.dynamicMCPDir + "/mcp-dynamic/" + taskID
		if task.MCPConfig == "" || strings.HasPrefix(task.MCPConfig, dynPrefix) {
			dynCfg := agent.BuildSubagentMCPConfig("", o.pandoMCPServers, o.gatewayExposeEnabled)
			if written, writeErr := agent.WriteCanonicalConfigToFile(dynCfg, dynPrefix, "mcp-config.json"); writeErr != nil {
				log.Printf("Warning: failed to regenerate dynamic MCP config for task %s: %v", taskID, writeErr)
			} else {
				task.MCPConfig = written
			}
		}
	}

	// Reset execution state while preserving identity and configuration.
	task.Status = models.TaskStatusPending
	task.PID = 0
	task.Error = ""
	task.ExitCode = nil
	task.Output = ""
	task.OutputTail = ""
	task.Progress = nil
	task.StartedAt = nil
	task.CompletedAt = nil
	task.CurrentTool = ""
	task.ToolCalls = nil

	logTaskReceived(task)

	if err := o.store.Save(task); err != nil {
		return nil, fmt.Errorf("failed to save task: %w", err)
	}

	if o.canStart(task) {
		reason := "relaunch_dependencies_satisfied"
		if len(task.Dependencies) == 0 {
			reason = "relaunch_no_dependencies"
		}
		logTaskStartable(task, reason)
		// A relaunch is an explicit action: it takes the claim (so no automatic
		// dispatch races it) but is not held back by the concurrency caps.
		o.dispatchNow(task, opts.Background)
	}

	return task, nil
}

// RetryOptions controls how a failed or pending task is retried.
type RetryOptions struct {
	Background bool
	// Force bypasses the circuit breaker / respawn guard (see RelaunchOptions).
	Force bool
}

// Retry relaunches a failed task or reactivates a pending task.
// For failed tasks, it creates a new task reusing the same log file (append mode)
// with the original prompt plus a retry notice.
// For pending tasks, it checks dependencies and starts the task if ready.
func (o *Orchestrator) Retry(ctx context.Context, taskID string, opts RetryOptions) (*models.Task, error) {
	prev, err := o.store.Get(taskID)
	if err != nil {
		return nil, err
	}

	switch prev.Status {
	case models.TaskStatusFailed:
		// Anti-thrash: the retry chain accumulates failures, so a task that keeps
		// failing trips the breaker instead of spawning replica after replica.
		if err := o.guardRespawn(prev, "retry", opts.Force); err != nil {
			return nil, err
		}
		return o.retryFailed(ctx, prev, opts)
	case models.TaskStatusPending:
		return o.replayPending(prev)
	default:
		return nil, fmt.Errorf("task %s cannot be retried (status=%s)", taskID, prev.Status)
	}
}

func (o *Orchestrator) retryFailed(ctx context.Context, prev *models.Task, opts RetryOptions) (*models.Task, error) {
	retryPrompt := fmt.Sprintf(
		"%s\n\n[RETRY] This task has been retried from a previous failed execution (task_id: %s). Check the previous log at: %s and verify the current state before continuing.",
		prev.Prompt,
		prev.ID,
		prev.LogFile,
	)

	model := prev.Model
	timeout := ""
	if prev.Timeout > 0 {
		timeout = time.Duration(prev.Timeout).String()
	}

	return o.Spawn(ctx, models.SpawnRequest{
		Prompt:       retryPrompt,
		WorkDir:      prev.WorkDir,
		Engine:       prev.Engine,
		Model:        model,
		Dependencies: prev.Dependencies,
		Tags:         prev.Tags,
		Priority:     prev.Priority,
		Timeout:      timeout,
		MCPConfig:    prev.MCPConfig,
		ExtraArgs:    prev.ExtraArgs,
		Persona:      prev.Persona,
		Background:   opts.Background,
		LogFile:      prev.LogFile,
		// Preserve the original task's delegation correlation so a retry stays
		// attributable to the same parent session/task and project.
		ParentSessionID: prev.ParentSessionID,
		ParentTaskID:    prev.ParentTaskID,
		CorrelationID:   prev.CorrelationID,
		ProjectID:       prev.ProjectID,
		ProjectPath:     prev.ProjectPath,
		Depth:           prev.Depth,
		// Carry the breaker state so the retry chain keeps counting: N failed
		// replicas of the same task must trip the breaker exactly like N failed
		// relaunches of a single task.
		MaxRetries:          prev.MaxRetries,
		ConsecutiveFailures: prev.ConsecutiveFailures,
	})
}

func (o *Orchestrator) replayPending(prev *models.Task) (*models.Task, error) {
	if o.canStart(prev) {
		logTaskStartable(prev, "replay_dependencies_satisfied")
		// Explicit retry of a pending task: claim it, but do not queue it behind
		// the concurrency caps.
		o.dispatchNow(prev, true)
	}
	return prev, nil
}

// Delete removes a task from the store.
// If the task is running, it will attempt to cancel it first.
// If the process is already dead or doesn't exist, the task will be deleted anyway.
func (o *Orchestrator) Delete(taskID string) error {
	task, err := o.store.Get(taskID)
	if err != nil {
		return err
	}

	if task.Status == models.TaskStatusRunning {
		// Try to cancel the task first through the manager
		if err := o.manager.Cancel(taskID); err != nil {
			// If cancel fails (e.g., process already dead), log it but continue
			log.Printf("Warning: failed to cancel task %s before deletion (process may be dead): %v", taskID, err)
		}

		// Mark task as cancelled and save state
		task.Status = models.TaskStatusCancelled
		now := time.Now()
		task.CompletedAt = &now
		if err := o.store.Save(task); err != nil {
			log.Printf("Warning: failed to save cancelled state for task %s: %v", taskID, err)
		}

		// Wait a bit for cleanup
		time.Sleep(100 * time.Millisecond)
	}

	return o.store.Delete(taskID)
}

// Purge stops a running task (if needed), deletes its log file (if any), and removes it from the store.
// This operation is intentionally idempotent: purging a missing task returns nil.
func (o *Orchestrator) Purge(taskID string) error {
	task, err := o.store.Get(taskID)
	if err != nil {
		if strings.Contains(err.Error(), "task not found") {
			return nil
		}
		return err
	}

	// Best-effort: stop the process if it is running.
	if task.Status == models.TaskStatusRunning {
		if err := o.manager.Cancel(taskID); err != nil {
			// If cancel fails (e.g., process already dead), log it but continue with purge
			log.Printf("Warning: failed to cancel task %s during purge (process may be dead): %v", taskID, err)
		}

		// Mark task as cancelled and save state
		task.Status = models.TaskStatusCancelled
		now := time.Now()
		task.CompletedAt = &now
		if err := o.store.Save(task); err != nil {
			log.Printf("Warning: failed to save cancelled state for task %s during purge: %v", taskID, err)
		}

		// Wait a bit for cleanup
		time.Sleep(100 * time.Millisecond)
	}

	// Best-effort: remove log file.
	if task.LogFile != "" {
		_ = os.Remove(task.LogFile)
	}

	if err := o.store.Delete(taskID); err != nil {
		if strings.Contains(err.Error(), "task not found") {
			return nil
		}
		return err
	}

	return nil
}

// SetProgress updates the progress of a running task.
func (o *Orchestrator) SetProgress(taskID string, percentage int, description string) error {
	task, err := o.store.Get(taskID)
	if err != nil {
		return err
	}

	// Sanitize percentage to be between 0 and 100
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}

	task.Progress = &models.TaskProgress{
		Percentage:  percentage,
		Description: description,
		UpdatedAt:   time.Now(),
	}

	return o.store.Save(task)
}

// ACPSessionControl sends a control command to an active ACP session.
// This delegates to the agent manager's ACP session control.
func (o *Orchestrator) ACPSessionControl(taskID, action, message, mode string) (interface{}, error) {
	return o.manager.ACPSessionControl(taskID, action, message, mode)
}

// GetStats returns orchestrator statistics.
func (o *Orchestrator) GetStats() Stats {
	tasks, _ := o.store.List(store.ListFilter{})

	stats := Stats{
		Running:         o.manager.RunningCount(),
		RunningProgress: make(map[string]TaskProgressInfo),
	}

	for _, task := range tasks {
		stats.Total++
		switch task.Status {
		case models.TaskStatusPending:
			stats.Pending++
		case models.TaskStatusRunning:
			// Add progress information for running tasks
			if task.Progress != nil {
				stats.RunningProgress[task.ID] = TaskProgressInfo{
					TaskID:      task.ID,
					Percentage:  task.Progress.Percentage,
					Description: task.Progress.Description,
					UpdatedAt:   task.Progress.UpdatedAt,
				}
			}
		case models.TaskStatusPaused:
			stats.Paused++
		case models.TaskStatusCompleted:
			stats.Completed++
		case models.TaskStatusFailed:
			stats.Failed++
		case models.TaskStatusCancelled:
			stats.Cancelled++
		}
	}

	return stats
}

// DelegationMetrics returns a point-in-time snapshot of the delegation routing /
// re-entry counters (item E1). Safe to call concurrently; never blocks the
// delegation path. The returned snapshot includes the derived warm-reuse hit rate.
func (o *Orchestrator) DelegationMetrics() DelegationMetricsSnapshot {
	return o.metrics.Snapshot()
}

// RecordResurrection is called by the delegation supervisor when an idle parent
// loop was successfully resurrected (Case B). It is exported because the
// supervisor lives in internal/app and only holds the concrete orchestrator.
func (o *Orchestrator) RecordResurrection() {
	if o.metrics != nil {
		o.metrics.recordResurrection()
	}
}

// RecordLiveInjection is called by the delegation supervisor when a conclusion
// was successfully injected into a still-running parent loop (Case A), including
// the resume-race fallback injection.
func (o *Orchestrator) RecordLiveInjection() {
	if o.metrics != nil {
		o.metrics.recordLiveInjection()
	}
}

// TaskProgressInfo holds progress information for a task.
type TaskProgressInfo struct {
	TaskID      string    `json:"task_id"`
	Percentage  int       `json:"percentage"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Stats holds orchestrator statistics.
type Stats struct {
	Total           int                         `json:"total"`
	Pending         int                         `json:"pending"`
	Running         int                         `json:"running"`
	Paused          int                         `json:"paused"`
	Completed       int                         `json:"completed"`
	Failed          int                         `json:"failed"`
	Cancelled       int                         `json:"cancelled"`
	RunningProgress map[string]TaskProgressInfo `json:"running_progress,omitempty"`
}

// Shutdown gracefully shuts down the orchestrator.
func (o *Orchestrator) Shutdown() error {
	o.cancel()
	// Wait for the dispatch loop before closing the store: a tick that outlived
	// Close would write to a store that is already flushed and shut down.
	o.wg.Wait()
	if o.manager != nil {
		o.manager.Shutdown()
	}
	return o.store.Close()
}

func generateID() string {
	return fmt.Sprintf("task-%s", uuid.New().String()[:8])
}

// ListPersonas returns a list of available persona names.
func (o *Orchestrator) ListPersonas() []string {
	return o.personaManager.ListPersonas()
}

// ListCustomEngines returns the names of all custom engine templates loaded at startup.
func (o *Orchestrator) ListCustomEngines() []string {
	return o.manager.CustomEngineNames()
}

func logTaskReceived(task *models.Task) {
	log.Printf(
		"task_event=received task_id=%s status=%s work_dir=%q engine=%q model=%q dependencies=%v tags=%v priority=%d timeout=%q mcp_config=%q extra_args=%v prompt_len=%d prompt_preview=%q",
		task.ID,
		task.Status,
		task.WorkDir,
		task.Engine,
		task.Model,
		task.Dependencies,
		task.Tags,
		task.Priority,
		time.Duration(task.Timeout).String(),
		task.MCPConfig,
		task.ExtraArgs,
		len(task.Prompt),
		truncateForLog(task.Prompt, 160),
	)
}

func logTaskStartable(task *models.Task, reason string) {
	log.Printf(
		"task_event=startable task_id=%s status=%s reason=%q dependencies=%v",
		task.ID,
		task.Status,
		reason,
		task.Dependencies,
	)
}

func logTaskGateFailed(task *models.Task, gateID, summary string) {
	log.Printf(
		"task_event=gate_failed task_id=%s gate_task_id=%s status=%s summary=%q",
		task.ID,
		gateID,
		task.Status,
		summary,
	)
}

// logConclusionGateDowngraded reports an anti-hallucination downgrade with the
// evidence that could not be found.
func logConclusionGateDowngraded(task *models.Task, res conclusion.VerifyResult) {
	log.Printf(
		"task_event=conclusion_gate_downgraded task_id=%s missing_artifacts=%v missing_memory_refs=%v",
		task.ID,
		res.MissingArtifacts,
		res.MissingMemoryRefs,
	)
}

// logBreakerTripped reports a task whose failure reached the retry limit.
func logBreakerTripped(task *models.Task, limit int) {
	log.Printf(
		"task_event=breaker_tripped task_id=%s consecutive_failures=%d limit=%d last_failure_kind=%q error=%q",
		task.ID,
		task.ConsecutiveFailures,
		limit,
		task.LastFailureKind,
		truncateForLog(strings.TrimSpace(task.Error), 160),
	)
}

// logRespawnRefused reports a re-execution denied by the respawn guard.
func logRespawnRefused(task *models.Task, op string, d breaker.Decision) {
	log.Printf(
		"task_event=respawn_refused task_id=%s op=%s reason=%s retry_after=%q detail=%q",
		task.ID,
		op,
		d.Reason,
		d.RetryAfter.Round(time.Second).String(),
		d.Detail,
	)
}

// logRespawnForced reports a guard denial that was explicitly overridden.
func logRespawnForced(task *models.Task, op string, d breaker.Decision) {
	log.Printf(
		"task_event=respawn_forced task_id=%s op=%s overridden_reason=%s",
		task.ID,
		op,
		d.Reason,
	)
}

func logTaskFinished(task *models.Task) {
	duration := ""
	if task.StartedAt != nil && task.CompletedAt != nil {
		duration = task.CompletedAt.Sub(*task.StartedAt).String()
	}

	exitCode := ""
	if task.ExitCode != nil {
		exitCode = fmt.Sprintf("%d", *task.ExitCode)
	}

	log.Printf(
		"task_event=finished task_id=%s status=%s exit_code=%s error=%q duration=%q log_file=%q",
		task.ID,
		task.Status,
		exitCode,
		strings.TrimSpace(task.Error),
		duration,
		task.LogFile,
	)
}

func truncateForLog(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// isACPEngine reports whether e is any ACP-based engine variant.
// ACP agents manage their own MCP server connections, so the dynamic
// MCP config generated by the orchestrator does not apply to them.
// EnginePando is a CLI engine (not ACP) and does get dynamic MCP config.
func isACPEngine(e models.Engine) bool {
	switch e {
	case models.EngineACP, models.EngineACPClaudeCode, models.EngineACPCodex,
		models.EngineACPCustom, models.EngineACPServer:
		return true
	}
	if strings.HasPrefix(string(e), "acp-") {
		return true
	}
	return false
}
