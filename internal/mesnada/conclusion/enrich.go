package conclusion

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// synthesizedSummaryTailBytes is the amount of trailing output retained when
// synthesizing a summary from a completed task that emitted no conclusion block.
const synthesizedSummaryTailBytes = 1000

// DelegationOptions carries the conclusion-relevant knobs the enricher needs.
// It is defined here (rather than importing internal/config) so the conclusion
// package stays free of import cycles; the app/orchestrator translate config into
// this value.
type DelegationOptions struct {
	// SynthesizeFallback enables deriving a conclusion from output/error when the
	// subagent did not emit a sentinel block. When false, a missing block means no
	// conclusion is captured.
	SynthesizeFallback bool
}

// ProjectResolver maps a canonical project path to its registry id and display
// name. It returns empty strings when the path is not a registered project.
type ProjectResolver func(canonicalPath string) (id string, name string)

// Enrich builds the durable Conclusion for a terminal task. It parses the task's
// output for the sentinel block, synthesizes a fallback when configured, and
// always fills software-owned metadata (CapturedAt, project id/name). It returns
// nil when no conclusion can or should be captured.
//
// The function is pure with respect to globals: everything it needs is passed in.
// It mutates task.ProjectID / task.ProjectName as a convenience for the caller
// (the resolved project is software-owned launch metadata) but never panics on
// nil inputs.
func Enrich(task *models.Task, cfg DelegationOptions, resolver ProjectResolver) *models.Conclusion {
	if task == nil {
		return nil
	}

	c, found := Parse(task.Output)
	if !found {
		// OutputTail may carry the final chunk when Output was truncated.
		c, found = Parse(task.OutputTail)
	}

	if !found {
		if !cfg.SynthesizeFallback {
			return nil
		}
		c = synthesize(task)
	}
	if c == nil {
		return nil
	}

	// Software-owned metadata, filled regardless of source.
	c.CapturedAt = time.Now()

	// Default the status from the task's terminal state when the block omitted it.
	if c.Status == "" {
		c.Status = defaultStatus(task)
	}

	resolveProject(task, resolver)

	return c
}

// synthesize derives a Conclusion from a task that emitted no sentinel block,
// using deterministic truncation only.
//
// TODO(phase-later): optional cheap-model summarization (reuse EvaluatorConfig's
// cheap model) when enabled, instead of plain truncation.
func synthesize(task *models.Task) *models.Conclusion {
	c := &models.Conclusion{Synthesized: true}

	switch task.Status {
	case models.TaskStatusCompleted:
		c.Status = "success"
		if task.ExitCode != nil && *task.ExitCode != 0 {
			c.Status = "partial"
		}
		c.Summary = truncateTail(firstNonEmpty(task.OutputTail, task.Output), synthesizedSummaryTailBytes)
	case models.TaskStatusFailed:
		c.Status = "failed"
		c.Summary = failureSummary(task)
	case models.TaskStatusCancelled:
		c.Status = "blocked"
		c.Summary = failureSummary(task)
	default:
		c.Status = defaultStatus(task)
		c.Summary = truncateTail(firstNonEmpty(task.OutputTail, task.Output), synthesizedSummaryTailBytes)
	}

	return c
}

// failureSummary builds a deterministic summary for a failed/cancelled task from
// its error and exit code, falling back to the output tail.
func failureSummary(task *models.Task) string {
	var b strings.Builder
	if msg := strings.TrimSpace(task.Error); msg != "" {
		b.WriteString(msg)
	}
	if task.ExitCode != nil {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(fmt.Sprintf("(exit code %d)", *task.ExitCode))
	}
	if b.Len() == 0 {
		return truncateTail(firstNonEmpty(task.OutputTail, task.Output), synthesizedSummaryTailBytes)
	}
	return b.String()
}

// defaultStatus returns the status implied by a task's terminal state.
func defaultStatus(task *models.Task) string {
	switch task.Status {
	case models.TaskStatusFailed:
		return "failed"
	case models.TaskStatusCancelled:
		return "blocked"
	default:
		return "success"
	}
}

// resolveProject fills task.ProjectID / task.ProjectName from the resolver when
// available, otherwise derives the display name from the canonical path.
func resolveProject(task *models.Task, resolver ProjectResolver) {
	if task.ProjectPath == "" {
		return
	}
	if resolver != nil {
		id, name := resolver(task.ProjectPath)
		if id != "" {
			task.ProjectID = id
		}
		if name != "" {
			task.ProjectName = name
			return
		}
	}
	// Resolver missing or returned no name: derive from the path.
	if task.ProjectName == "" {
		task.ProjectName = filepath.Base(task.ProjectPath)
	}
}

// truncateTail returns the trimmed last n bytes of s.
func truncateTail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[len(s)-n:])
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
