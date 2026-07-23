package conclusion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// StatusSuccess and StatusPartial are the two conclusion statuses the gate moves
// between. They are plain strings on the wire (Conclusion.Status).
const (
	StatusSuccess = "success"
	StatusPartial = "partial"
)

// maxVerifyRefsListed bounds how many missing refs are named in a warning, so a
// pathological conclusion cannot bloat the task record.
const maxVerifyRefsListed = 10

// MemoryRefValidator reports whether a memory reference (a KB path or memory
// key) actually resolves. A nil validator means memory refs are not checked —
// the gate then only verifies filesystem artifacts.
type MemoryRefValidator func(ref string) bool

// VerifyOptions configures the anti-hallucination conclusion gate.
type VerifyOptions struct {
	// Disabled turns the gate off entirely: Verify becomes a no-op. Mirrors the
	// config opt-out so the gate can be switched off without unwiring it.
	Disabled bool
	// BaseDir resolves relative artifact paths (normally the task's work dir).
	// When empty, relative artifacts are considered unverifiable and ignored.
	BaseDir string
	// Memory validates memory_refs. When nil, memory refs are not checked.
	Memory MemoryRefValidator
}

// VerifyResult reports what the gate found.
type VerifyResult struct {
	// Downgraded is true when the conclusion's status was changed.
	Downgraded bool
	// MissingArtifacts are artifact refs that were checkable and absent.
	MissingArtifacts []string
	// MissingMemoryRefs are memory refs that were checkable and unresolvable.
	MissingMemoryRefs []string
}

// Verify is the anti-hallucination completion gate: it refuses to let a
// delegated task claim full success while citing evidence that does not exist.
//
// Only a conclusion with status "success" is inspected. Every artifact that can
// be checked (a local filesystem path) is stat'ed, and every memory ref is
// resolved through the validator when one is wired. If any checkable reference
// is missing, the status is downgraded to "partial" and a warning naming the
// missing refs is appended. Nothing is ever discarded — the conclusion keeps its
// summary, artifacts and refs, exactly as the "warn, don't discard" principle of
// the conclusion parser requires.
//
// Unverifiable references (URLs, git refs, glob patterns, opaque tokens, or any
// relative path when BaseDir is empty) are ignored rather than treated as
// missing: the gate is fail-open on what it cannot check and fail-closed only on
// what it positively disproves.
//
// Verify is nil-safe and returns a zero VerifyResult when it does nothing.
func Verify(c *models.Conclusion, opts VerifyOptions) VerifyResult {
	if c == nil || opts.Disabled || c.Status != StatusSuccess {
		return VerifyResult{}
	}

	var res VerifyResult

	for _, ref := range c.Artifacts {
		if !artifactCheckable(ref, opts.BaseDir) {
			continue
		}
		if !artifactExists(ref, opts.BaseDir) {
			res.MissingArtifacts = append(res.MissingArtifacts, strings.TrimSpace(ref))
		}
	}

	if opts.Memory != nil {
		for _, ref := range c.MemoryRefs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if !opts.Memory(ref) {
				res.MissingMemoryRefs = append(res.MissingMemoryRefs, ref)
			}
		}
	}

	if len(res.MissingArtifacts) == 0 && len(res.MissingMemoryRefs) == 0 {
		return res
	}

	c.Status = StatusPartial
	res.Downgraded = true
	c.Warnings = append(c.Warnings, verifyWarning(res))
	return res
}

// verifyWarning renders the human-readable downgrade warning.
func verifyWarning(res VerifyResult) string {
	var b strings.Builder
	b.WriteString("conclusion gate: status downgraded success -> partial because cited evidence could not be found")
	if len(res.MissingArtifacts) > 0 {
		fmt.Fprintf(&b, "; missing artifacts: %s", joinCapped(res.MissingArtifacts, maxVerifyRefsListed))
	}
	if len(res.MissingMemoryRefs) > 0 {
		fmt.Fprintf(&b, "; unresolvable memory_refs: %s", joinCapped(res.MissingMemoryRefs, maxVerifyRefsListed))
	}
	return b.String()
}

// joinCapped joins at most max entries, noting how many were elided.
func joinCapped(refs []string, max int) string {
	if len(refs) <= max {
		return strings.Join(refs, ", ")
	}
	return fmt.Sprintf("%s (+%d more)", strings.Join(refs[:max], ", "), len(refs)-max)
}

// artifactCheckable reports whether ref is a local filesystem path the gate is
// willing to disprove. Anything remote, patterned or ambiguous is excluded so
// the gate never downgrades on evidence it simply cannot inspect.
func artifactCheckable(ref, baseDir string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	// Remote or non-filesystem references.
	if strings.Contains(ref, "://") || strings.HasPrefix(ref, "git@") {
		return false
	}
	// Glob patterns: the ref names a set, not a file.
	if strings.ContainsAny(ref, "*?[") {
		return false
	}
	// Prose or descriptions that happen to land in the artifacts list.
	if strings.ContainsAny(ref, " \t") {
		return false
	}
	// Relative paths need a base dir to be resolvable at all.
	if !filepath.IsAbs(ref) && baseDir == "" {
		return false
	}
	return true
}

// artifactExists stats the artifact, resolving relative paths against baseDir.
// A stat error other than "not exists" (permissions, broken mount) counts as
// present: the gate must not punish a conclusion for an environment problem.
func artifactExists(ref, baseDir string) bool {
	ref = strings.TrimSpace(ref)
	path := ref
	if !filepath.IsAbs(ref) {
		path = filepath.Join(baseDir, ref)
	}
	if _, err := os.Stat(path); err != nil {
		return !os.IsNotExist(err)
	}
	return true
}
