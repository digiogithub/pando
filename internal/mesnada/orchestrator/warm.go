package orchestrator

import (
	"context"
	"errors"
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

	start := time.Now()
	res, err := o.warmResolver.RunWarm(o.ctx, task.ProjectID, task.ProjectPath, task.Prompt)
	if errors.Is(err, ErrNoWarmTarget) {
		// No warm target — leave the task untouched for the cold path.
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
		task.Status = models.TaskStatusFailed
		task.Error = err.Error()
		log.Printf("task_event=warm_failed task_id=%s project_id=%q project_path=%q error=%q",
			task.ID, task.ProjectID, task.ProjectPath, err.Error())
	} else {
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
