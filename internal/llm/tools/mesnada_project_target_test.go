package tools

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
)

// TestKnownProjectsHintEmpty verifies the hint for the "unknown project" error
// when no projects are registered.
func TestKnownProjectsHintEmpty(t *testing.T) {
	got := knownProjectsHint(nil)
	if !strings.Contains(got, "No projects are registered") {
		t.Fatalf("hint = %q, want a 'no projects registered' message", got)
	}
}

// TestKnownProjectsHintLists verifies that registered projects are listed as
// "name (id)" entries so the model can retry with a valid reference.
func TestKnownProjectsHintLists(t *testing.T) {
	refs := []orchestrator.ProjectRef{
		{ID: "p1", Name: "Alpha", Path: "/a"},
		{ID: "p2", Name: "", Path: "/beta"}, // no name => fall back to path
	}
	got := knownProjectsHint(refs)
	if !strings.Contains(got, "Alpha (p1)") {
		t.Fatalf("hint = %q, want to contain 'Alpha (p1)'", got)
	}
	if !strings.Contains(got, "/beta (p2)") {
		t.Fatalf("hint = %q, want to contain '/beta (p2)' (path fallback)", got)
	}
}

// TestKnownProjectsHintTruncates verifies the hint caps the listed projects and
// reports the remaining count.
func TestKnownProjectsHintTruncates(t *testing.T) {
	var refs []orchestrator.ProjectRef
	for i := 0; i < 15; i++ {
		refs = append(refs, orchestrator.ProjectRef{ID: "id", Name: "n", Path: "/p"})
	}
	got := knownProjectsHint(refs)
	if !strings.Contains(got, "and 5 more") {
		t.Fatalf("hint = %q, want to mention 'and 5 more'", got)
	}
}
