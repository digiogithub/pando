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
// res controls optional enrichment of artifact paths and memory refs. When res is
// nil (or the individual resolver is nil), the corresponding field is rendered as a
// plain comma-joined list — byte-for-byte identical to the pre-resolver output. When
// a resolver is provided, each entry is rendered on its own line under the field
// header, resolved via the supplied function.
//
// It is nil-safe: a task without a Conclusion yields a minimal single line so the
// parent still learns the task finished.
func FormatForParent(task *models.Task, res *ResolveOptions) string {
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

	var artifactResolver ArtifactResolver
	var memoryResolver MemoryRefResolver
	if res != nil {
		artifactResolver = res.Artifacts
		memoryResolver = res.Memory
	}

	if artifactResolver != nil {
		writeResolvedList(&b, "artifacts", c.Artifacts, func(s string) string { return artifactResolver(s) })
	} else {
		if list := joinNonEmpty(c.Artifacts); list != "" {
			b.WriteString("\nartifacts: ")
			b.WriteString(list)
		}
	}

	if memoryResolver != nil {
		writeResolvedList(&b, "memory_refs", c.MemoryRefs, func(s string) string { return memoryResolver(s) })
	} else {
		if list := joinNonEmpty(c.MemoryRefs); list != "" {
			b.WriteString("\nmemory_refs: ")
			b.WriteString(list)
		}
	}

	// Surface soft-schema validation warnings (item D1) so the parent learns the
	// subagent's conclusion block was malformed/incomplete even though the data was
	// salvaged rather than discarded. Omitted when there are none.
	if list := joinNonEmpty(c.Warnings); list != "" {
		b.WriteString("\nwarnings: ")
		b.WriteString(list)
	}

	return b.String()
}

// writeResolvedList writes a per-line resolved list under a named field header.
// Blank/whitespace-only entries are dropped. If no entries remain, nothing is
// written (preserving the "omit empty" contract).
func writeResolvedList(b *strings.Builder, field string, items []string, resolve func(string) string) {
	var resolved []string
	for _, it := range items {
		if t := strings.TrimSpace(it); t != "" {
			resolved = append(resolved, resolve(t))
		}
	}
	if len(resolved) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(field)
	b.WriteString(":")
	for _, r := range resolved {
		b.WriteString("\n  - ")
		b.WriteString(r)
	}
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
