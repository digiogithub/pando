package conclusion

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// FormatForParent renders a compact, pointers-not-dumps message describing a
// delegated task's result, suitable for injection into the parent agent loop. The
// header line carries only software-owned launch metadata (task id, engine,
// project, status, confidence) — never values the model could have hallucinated.
// The body carries the model-provided summary plus optional follow-up, artifacts
// and memory refs (each omitted when empty) so the parent can lazily fetch detail.
//
// It is nil-safe: a task without a Conclusion yields a minimal single line so the
// parent still learns the task finished.
func FormatForParent(task *models.Task) string {
	if task == nil {
		return "[delegated-result] (no task)"
	}

	id := task.ID
	engine := string(task.Engine)
	project := projectLabel(task)

	if task.Conclusion == nil {
		return fmt.Sprintf("[delegated-result task=%s engine=%s project=%s status=%s] (no conclusion captured)",
			id, engine, project, string(task.Status))
	}

	c := task.Conclusion
	var b strings.Builder

	b.WriteString("[delegated-result")
	b.WriteString(" task=")
	b.WriteString(id)
	b.WriteString(" engine=")
	b.WriteString(engine)
	b.WriteString(" project=")
	b.WriteString(project)
	b.WriteString(" status=")
	b.WriteString(c.Status)
	if c.Confidence > 0 {
		b.WriteString(fmt.Sprintf(" confidence=%.2f", c.Confidence))
	}
	b.WriteString("]")

	if s := strings.TrimSpace(c.Summary); s != "" {
		b.WriteString("\nsummary: ")
		b.WriteString(s)
	}
	if f := strings.TrimSpace(c.FollowUp); f != "" {
		b.WriteString("\nfollow_up: ")
		b.WriteString(f)
	}
	if list := joinNonEmpty(c.Artifacts); list != "" {
		b.WriteString("\nartifacts: ")
		b.WriteString(list)
	}
	if list := joinNonEmpty(c.MemoryRefs); list != "" {
		b.WriteString("\nmemory_refs: ")
		b.WriteString(list)
	}

	return b.String()
}

// projectLabel returns the best available display name for the task's project:
// the resolved project name, else the base of the canonical project path, else
// the work dir base, else "unknown".
func projectLabel(task *models.Task) string {
	if n := strings.TrimSpace(task.ProjectName); n != "" {
		return n
	}
	if p := strings.TrimSpace(task.ProjectPath); p != "" {
		return filepath.Base(p)
	}
	if w := strings.TrimSpace(task.WorkDir); w != "" {
		return filepath.Base(w)
	}
	return "unknown"
}

// joinNonEmpty joins the trimmed, non-empty elements of items with ", ".
func joinNonEmpty(items []string) string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if t := strings.TrimSpace(it); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, ", ")
}
