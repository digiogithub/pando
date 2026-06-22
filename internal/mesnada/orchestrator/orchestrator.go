// Package orchestrator coordinates agent tasks and dependencies.
package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/mesnada/agent"
	"github.com/digiogithub/pando/internal/mesnada/conclusion"
	"github.com/digiogithub/pando/internal/mesnada/config"
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
	warmResolver         WarmTargetResolver
	projectRefResolver   ProjectRefResolver
	// awaitIntents records, per parent session id, the active non-blocking await
	// intent registered by the mesnada_await tool (see await.go). Guarded by
	// awaitMu; in-memory only (mirrors the supervisor's in-memory batch state).
	awaitIntents map[string]*AwaitIntent
	awaitMu      sync.Mutex
	// metrics records delegation routing/re-entry counters for the orchestrator's
	// lifetime (item E1). Always non-nil after New; safe for lock-free concurrent
	// updates from the warm path and the delegation supervisor.
	metrics *DelegationMetrics
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
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
}

// Config holds orchestrator configuration.
type Config struct {
	StorePath string
	LogDir    string
	// EnginesDir is the directory scanned at startup for *.template.yaml custom
	// engine files. When empty the manager derives it from LogDir.
	EnginesDir  string
	MaxParallel int
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
	// WarmTargetResolver routes delegated tasks to warm per-project ACP instances
	// (Phase 7.3). When nil (or Delegation.ReuseWarmInstances is false) every task
	// takes the cold subprocess path, preserving today's behavior.
	WarmTargetResolver WarmTargetResolver
	// ProjectRefResolver resolves a free-form project reference supplied to the
	// spawn tool (item B1) to a registered project id/path. When nil the spawn
	// tool's optional "project" argument is rejected as unsupported.
	ProjectRefResolver ProjectRefResolver
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

	o := &Orchestrator{
		store:                fileStore,
		personaManager:       personaManager,
		subscribers:          make(map[string][]chan *models.Task),
		maxParallel:          cfg.MaxParallel,
		defaultMCPConfig:     cfg.DefaultMCPConfig,
		defaultEngine:        defaultEngine,
		pandoMCPServers:      cfg.MCPServers,
		gatewayExposeEnabled: cfg.GatewayExposeEnabled,
		dynamicMCPDir:        dynamicMCPDir,
		delegation:           cfg.Delegation,
		projectResolver:      cfg.ProjectResolver,
		warmResolver:         cfg.WarmTargetResolver,
		projectRefResolver:   cfg.ProjectRefResolver,
		metrics:              &DelegationMetrics{},
		ctx:                  ctx,
		cancel:               cancel,
	}

	o.manager = agent.NewManager(cfg.AppConfig, cfg.LogDir, o.onTaskComplete, o.SetProgress, cfg.ModelResolver)

	// Recover any tasks that were left in running state from a previous run.
	o.recoverStaleTasks()

	return o, nil
}

// recoverStaleTasks marks any tasks that are still in "running" state as failed.
// This happens when pando is restarted after a crash or ungraceful shutdown.
func (o *Orchestrator) recoverStaleTasks() {
	tasks, err := o.store.List(store.ListFilter{
		Status: []models.TaskStatus{models.TaskStatusRunning},
	})
	if err != nil {
		log.Printf("Warning: failed to list running tasks for recovery: %v", err)
		return
	}

	for _, task := range tasks {
		log.Printf("task_event=recovering_stale task_id=%s - marking as failed (process interrupted)", task.ID)
		task.Status = models.TaskStatusFailed
		task.Error = "task was interrupted (process died or pando was restarted)"
		now := time.Now()
		task.CompletedAt = &now
		if err := o.store.Save(task); err != nil {
			log.Printf("Warning: failed to save recovered task %s: %v", task.ID, err)
		}
	}

	if len(tasks) > 0 {
		log.Printf("task_recovery: marked %d stale running task(s) as failed", len(tasks))
	}
}

func (o *Orchestrator) onTaskComplete(task *models.Task) {
	// Save final state
	o.store.Save(task)
	logTaskFinished(task)

	// Capture + enrich the delegated-task conclusion (default-off). Nothing
	// consumes it yet (Cases A/B are later phases); this only persists it on the
	// task. Kept side-effect-light and panic-safe so completion stays robust.
	o.captureConclusion(task)

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

	task.Conclusion = c
	if err := o.store.Save(task); err != nil {
		log.Printf("Warning: failed to persist conclusion for task %s: %v", task.ID, err)
	}
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
			go o.startTask(task)
		}
	}
}

func (o *Orchestrator) canStart(task *models.Task) bool {
	if len(task.Dependencies) == 0 {
		return true
	}

	for _, depID := range task.Dependencies {
		dep, err := o.store.Get(depID)
		if err != nil {
			return false
		}
		if dep.Status != models.TaskStatusCompleted {
			return false
		}
	}

	return true
}

func (o *Orchestrator) startTask(task *models.Task) {
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
		// When spawning fails, we still consider the task finished.
		logTaskFinished(task)
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
			go o.startTask(task)
		} else {
			o.startTask(task)
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
		if opts.Background {
			go o.startTask(task)
		} else {
			o.startTask(task)
		}
	}

	return task, nil
}

// RetryOptions controls how a failed or pending task is retried.
type RetryOptions struct {
	Background bool
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
	})
}

func (o *Orchestrator) replayPending(prev *models.Task) (*models.Task, error) {
	if o.canStart(prev) {
		logTaskStartable(prev, "replay_dependencies_satisfied")
		go o.startTask(prev)
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
	o.manager.Shutdown()
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
