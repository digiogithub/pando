package conclusion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// writeFile creates a file inside dir and returns its base name.
func writeFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return name
}

func TestVerifyDowngradesOnMissingArtifact(t *testing.T) {
	dir := t.TempDir()
	present := writeFile(t, dir, "internal/parser.go")

	c := &models.Conclusion{
		Status:    StatusSuccess,
		Summary:   "wrote the parser",
		Artifacts: []string{present, "internal/ghost.go"},
	}

	res := Verify(c, VerifyOptions{BaseDir: dir})
	if !res.Downgraded {
		t.Fatal("a missing artifact must downgrade the conclusion")
	}
	if c.Status != StatusPartial {
		t.Errorf("Status = %q, want %q", c.Status, StatusPartial)
	}
	if len(res.MissingArtifacts) != 1 || res.MissingArtifacts[0] != "internal/ghost.go" {
		t.Errorf("MissingArtifacts = %v, want [internal/ghost.go]", res.MissingArtifacts)
	}
	if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "internal/ghost.go") {
		t.Errorf("warnings must name the missing artifact, got %v", c.Warnings)
	}
	// Nothing is ever discarded.
	if c.Summary != "wrote the parser" || len(c.Artifacts) != 2 {
		t.Error("Verify must never discard conclusion data")
	}
}

func TestVerifyKeepsSuccessWhenEvidenceExists(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.txt")
	b := writeFile(t, dir, "sub/b.txt")

	c := &models.Conclusion{Status: StatusSuccess, Artifacts: []string{a, b, filepath.Join(dir, a)}}
	if res := Verify(c, VerifyOptions{BaseDir: dir}); res.Downgraded {
		t.Fatalf("existing artifacts must not downgrade, missing=%v", res.MissingArtifacts)
	}
	if c.Status != StatusSuccess || len(c.Warnings) != 0 {
		t.Errorf("status/warnings must be untouched, got %q / %v", c.Status, c.Warnings)
	}
}

func TestVerifyIgnoresUnverifiableRefs(t *testing.T) {
	dir := t.TempDir()
	unverifiable := []string{
		"https://example.com/pr/1",
		"git@github.com:acme/repo.git",
		"internal/**/*.go",
		"the refactored parser package",
		"   ",
	}
	c := &models.Conclusion{Status: StatusSuccess, Artifacts: unverifiable}
	if res := Verify(c, VerifyOptions{BaseDir: dir}); res.Downgraded {
		t.Fatalf("unverifiable refs must be ignored, missing=%v", res.MissingArtifacts)
	}

	// A relative path with no base dir is unverifiable too.
	rel := &models.Conclusion{Status: StatusSuccess, Artifacts: []string{"nope.go"}}
	if res := Verify(rel, VerifyOptions{}); res.Downgraded {
		t.Error("a relative path without BaseDir must be ignored, not disproved")
	}
}

func TestVerifyOnlyInspectsSuccess(t *testing.T) {
	dir := t.TempDir()
	for _, status := range []string{StatusPartial, "failed", "blocked", ""} {
		c := &models.Conclusion{Status: status, Artifacts: []string{"ghost.go"}}
		if res := Verify(c, VerifyOptions{BaseDir: dir}); res.Downgraded {
			t.Errorf("status %q must not be inspected by the gate", status)
		}
	}
}

func TestVerifyDisabledAndNilAreNoOps(t *testing.T) {
	dir := t.TempDir()
	c := &models.Conclusion{Status: StatusSuccess, Artifacts: []string{"ghost.go"}}
	if res := Verify(c, VerifyOptions{Disabled: true, BaseDir: dir}); res.Downgraded {
		t.Error("a disabled gate must be a no-op")
	}
	if c.Status != StatusSuccess {
		t.Error("a disabled gate must not touch the status")
	}
	if res := Verify(nil, VerifyOptions{BaseDir: dir}); res.Downgraded {
		t.Error("a nil conclusion must be a no-op")
	}
}

func TestVerifyMemoryRefs(t *testing.T) {
	known := map[string]bool{"project.test_command": true}
	validator := func(ref string) bool { return known[ref] }

	c := &models.Conclusion{
		Status:     StatusSuccess,
		MemoryRefs: []string{"project.test_command", "pando/plans/imaginary.md"},
	}
	res := Verify(c, VerifyOptions{Memory: validator})
	if !res.Downgraded {
		t.Fatal("an unresolvable memory ref must downgrade the conclusion")
	}
	if len(res.MissingMemoryRefs) != 1 || res.MissingMemoryRefs[0] != "pando/plans/imaginary.md" {
		t.Errorf("MissingMemoryRefs = %v", res.MissingMemoryRefs)
	}
	if !strings.Contains(c.Warnings[0], "memory_refs") {
		t.Errorf("warning must mention memory_refs, got %q", c.Warnings[0])
	}

	// Without a validator memory refs are never checked.
	c2 := &models.Conclusion{Status: StatusSuccess, MemoryRefs: []string{"whatever"}}
	if res := Verify(c2, VerifyOptions{}); res.Downgraded {
		t.Error("a nil memory validator must skip memory refs entirely")
	}
}

func TestJoinCappedElidesOverflow(t *testing.T) {
	refs := []string{"a", "b", "c", "d"}
	if got := joinCapped(refs, 4); got != "a, b, c, d" {
		t.Errorf("no elision expected, got %q", got)
	}
	got := joinCapped(refs, 2)
	if !strings.Contains(got, "+2 more") {
		t.Errorf("expected elision note, got %q", got)
	}
}
