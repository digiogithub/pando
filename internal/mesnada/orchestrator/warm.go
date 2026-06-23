package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// ErrNoWarmTarget signals that a delegated task cannot (or must not) be served by
// a warm per-project instance — e.g. the project is unregistered, has no config,
// is served by an external editor instance, auto-start is disabled with nothing
// running, or the per-instance concurrency cap is reached. The orchestrator falls
// back to the cold subprocess path when a resolver returns this. Any other error
// from RunWarm is treated as a genuine warm-run failure (terminal-failed task).
var ErrNoWarmTarget = errors.New("no warm target available; use cold path")

// ErrWarmCapReached is a more specific flavour of ErrNoWarmTarget: the project
// HAS a warm instance but it is at its concurrency cap (and the warm queue, if
// any, is full), so this delegation falls back to the cold path. It wraps
// ErrNoWarmTarget so existing `errors.Is(err, ErrNoWarmTarget)` cold-path checks
// keep working; the warm resolver adapter returns it (instead of plain
// ErrNoWarmTarget) only for cap-driven fallbacks so the orchestrator can count
// them separately for telemetry (item E1).
var ErrWarmCapReached = fmt.Errorf("warm instance concurrency cap reached: %w", ErrNoWarmTarget)

// WarmRunResult is the captured outcome of running a delegated prompt inside a
// warm per-project ACP instance. It mirrors internal/project.DelegateResult but
// lives here so the orchestrator stays free of an internal/project import cycle
// (the adapter that bridges the two lives in internal/app).
type WarmRunResult struct {
	// ChildSessionID is the ACP session id created in the warm child for this run.
	ChildSessionID string
	// Output is the agent's full streamed message text for the turn.
	Output string
	// StopReason is the ACP stop reason reported by the child (e.g. end_turn).
	StopReason string
	// External is true when the run was served by an external (editor-launched)
	// peer over the IPC bus (hot-peer delegation, B3) rather than a manager-spawned
	// warm child. Used only for metrics attribution (externalHits).
	External bool
}

// WarmTargetResolver routes a delegated task to a warm per-project ACP instance,
// capturing its conclusion over the wire instead of cold-spawning a CLI. It is
// implemented by an adapter over internal/project.Manager and injected into the
// orchestrator to avoid an import cycle (same pattern as ProjectResolver /
// ModelResolver).
type WarmTargetResolver interface {
	// RunWarm runs promptText inside a warm instance for the given project,
	// applying the reuse-then-autostart policy and the per-instance concurrency
	// cap. projectID may be empty, in which case projectPath is resolved to a
	// registered project. It returns ErrNoWarmTarget when the project cannot be
	// served by a warm instance so the caller takes the cold path.
	RunWarm(ctx context.Context, projectID, projectPath, promptText string) (*WarmRunResult, error)
}

// ProjectRef identifies a registered project, used by the spawn tool to target a
// specific project by id/name/path (item B1). Path is the project's working
// directory; Name its display name.
type ProjectRef struct {
	ID   string
	Name string
	Path string
}

// ProjectRefResolver resolves a free-form project reference (registry id, display
// name, or directory path) to a registered project, and lists registered projects
// for building helpful error messages. It is injected from internal/app over the
// project registry to avoid an import cycle (same pattern as WarmTargetResolver).
type ProjectRefResolver interface {
	// ResolveProjectRef returns the registered project matching ref, or ok=false
	// when none matches.
	ResolveProjectRef(ctx context.Context, ref string) (ProjectRef, bool)
	// ListProjectRefs returns all registered projects (for error messages).
	ListProjectRefs(ctx context.Context) []ProjectRef
}

// ResolveProjectRef resolves a free-form project reference (id/name/path) to a
// registered project via the injected resolver. ok is false when no resolver is
// wired or no project matches.
func (o *Orchestrator) ResolveProjectRef(ctx context.Context, ref string) (ProjectRef, bool) {
	if o.projectRefResolver == nil {
		return ProjectRef{}, false
	}
	return o.projectRefResolver.ResolveProjectRef(ctx, ref)
}

// ProjectRefsSupported reports whether project-targeting is available (a resolver
// is wired). When false the spawn tool rejects the "project" argument instead of
// silently ignoring it.
func (o *Orchestrator) ProjectRefsSupported() bool {
	return o.projectRefResolver != nil
}

// ListProjectRefs returns all registered projects (used to build a helpful error
// when a project reference does not resolve). Returns nil when no resolver wired.
func (o *Orchestrator) ListProjectRefs(ctx context.Context) []ProjectRef {
	if o.projectRefResolver == nil {
		return nil
	}
	return o.projectRefResolver.ListProjectRefs(ctx)
}

// tryStartWarm attempts to run a delegated task inside a warm per-project
// instance instead of cold-spawning. It returns true when the task was handled
// by the warm path (success or warm-run failure — completion is driven here via
// onTaskComplete); false means the caller must take the cold path, with the task
// left untouched.
//
// Gating: warm routing is only attempted when delegation is enabled, the master
// ReuseWarmInstances flag is on, a resolver is wired, and the task carries a
// project id or path. Otherwise it is a no-op returning false.
func (o *Orchestrator) tryStartWarm(task *models.Task) bool {
	if o.warmResolver == nil || !o.delegation.Enabled || !o.delegation.ReuseWarmInstances {
		return false
	}
	if task == nil || (task.ProjectID == "" && task.ProjectPath == "") {
		return false
	}

	o.metrics.recordWarmAttempt()
	start := time.Now()
	res, err := o.warmResolver.RunWarm(o.ctx, task.ProjectID, task.ProjectPath, task.Prompt)
	if errors.Is(err, ErrNoWarmTarget) {
		// No warm target — leave the task untouched for the cold path. Count the
		// fallback, attributing the cap-driven subset separately for telemetry.
		o.metrics.recordColdFallback()
		if errors.Is(err, ErrWarmCapReached) {
			o.metrics.recordCapRejection()
		}
		return false
	}

	// A warm instance WAS used (success or run failure): drive completion here so
	// the conclusion pipeline (captureConclusion → supervisor) runs exactly once.
	// Per the bookkeeping decision, a delegated run always reaches a terminal
	// state so the parent loop can never hang.
	task.Engine = models.EngineWarmACP
	task.StartedAt = &start
	now := time.Now()
	task.CompletedAt = &now

	if err != nil {
		o.metrics.recordWarmFailure()
		task.Status = models.TaskStatusFailed
		task.Error = err.Error()
		log.Printf("task_event=warm_failed task_id=%s project_id=%q project_path=%q error=%q",
			task.ID, task.ProjectID, task.ProjectPath, err.Error())
	} else {
		o.metrics.recordWarmHit()
		if res.External {
			o.metrics.recordExternalHit()
		}
		task.Status = models.TaskStatusCompleted
		task.Output = res.Output
		task.ACPSessionID = res.ChildSessionID
		exit := 0
		task.ExitCode = &exit
		log.Printf("task_event=warm_completed task_id=%s project_id=%q child_session=%q stop_reason=%q output_len=%d",
			task.ID, task.ProjectID, res.ChildSessionID, res.StopReason, len(res.Output))
	}

	o.onTaskComplete(task)
	return true
}
