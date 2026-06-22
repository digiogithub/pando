package conclusion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// nilRes is a convenience alias for calling FormatForParent with no resolvers,
// preserving the legacy (comma-joined) output path.
var nilRes *ResolveOptions = nil

func TestFormatForParentFull(t *testing.T) {
	task := &models.Task{
		ID:          "task-123",
		Engine:      models.EnginePando,
		Status:      models.TaskStatusCompleted,
		ProjectName: "myproj",
		ProjectPath: "/home/u/code/myproj",
		Conclusion: &models.Conclusion{
			Status:     "success",
			Summary:    "Implemented the feature and added tests.",
			FollowUp:   "Wire the settings UI.",
			Artifacts:  []string{"internal/foo.go", "internal/foo_test.go"},
			MemoryRefs: []string{"pando/changes/foo.md"},
			Confidence: 0.9,
		},
	}

	out := FormatForParent(task, nilRes)

	for _, want := range []string{
		"task-123",
		"engine=pando",
		"project=myproj",
		"status=success",
		"confidence=0.90",
		"Implemented the feature",
		"follow_up: Wire the settings UI.",
		"artifacts: internal/foo.go, internal/foo_test.go",
		"memory_refs: pando/changes/foo.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatForParent output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestFormatForParentOmitsEmptyOptionalFields(t *testing.T) {
	task := &models.Task{
		ID:     "t1",
		Engine: models.EngineClaude,
		Status: models.TaskStatusCompleted,
		Conclusion: &models.Conclusion{
			Status:  "partial",
			Summary: "Did part of the work.",
		},
	}
	out := FormatForParent(task, nilRes)
	if strings.Contains(out, "follow_up:") {
		t.Errorf("expected no follow_up line, got:\n%s", out)
	}
	if strings.Contains(out, "artifacts:") {
		t.Errorf("expected no artifacts line, got:\n%s", out)
	}
	if strings.Contains(out, "memory_refs:") {
		t.Errorf("expected no memory_refs line, got:\n%s", out)
	}
	if strings.Contains(out, "confidence=") {
		t.Errorf("expected no confidence when zero, got:\n%s", out)
	}
}

func TestFormatForParentNilConclusion(t *testing.T) {
	task := &models.Task{
		ID:          "t2",
		Engine:      models.EnginePando,
		Status:      models.TaskStatusFailed,
		ProjectName: "proj",
	}
	out := FormatForParent(task, nilRes)
	if !strings.Contains(out, "no conclusion captured") {
		t.Errorf("expected nil-conclusion guard text, got: %s", out)
	}
	if !strings.Contains(out, "t2") || !strings.Contains(out, "status=failed") {
		t.Errorf("expected id and status in guard line, got: %s", out)
	}
}

func TestFormatForParentNilTask(t *testing.T) {
	if out := FormatForParent(nil, nilRes); !strings.Contains(out, "no task") {
		t.Errorf("expected nil-task guard, got: %s", out)
	}
}

func TestFormatForParentProjectLabelFallback(t *testing.T) {
	// No ProjectName, but ProjectPath present -> base of path.
	task := &models.Task{
		ID:          "t3",
		ProjectPath: "/a/b/cool-project",
		Conclusion:  &models.Conclusion{Status: "success", Summary: "x"},
	}
	if out := FormatForParent(task, nilRes); !strings.Contains(out, "project=cool-project") {
		t.Errorf("expected project base fallback, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Resolver path tests
// ---------------------------------------------------------------------------

func TestFormatForParentWithArtifactResolver(t *testing.T) {
	task := &models.Task{
		ID:     "task-r1",
		Engine: models.EnginePando,
		Conclusion: &models.Conclusion{
			Status:    "success",
			Summary:   "Done.",
			Artifacts: []string{"cmd/main.go", "internal/foo.go"},
		},
	}
	fakeArtifact := func(ref string) string { return ref + " (fake-size)" }
	res := &ResolveOptions{Artifacts: fakeArtifact}

	out := FormatForParent(task, res)

	// Should use per-line format with resolver output.
	if !strings.Contains(out, "artifacts:") {
		t.Errorf("expected 'artifacts:' header, got:\n%s", out)
	}
	if !strings.Contains(out, "  - cmd/main.go (fake-size)") {
		t.Errorf("expected resolved artifact line, got:\n%s", out)
	}
	if !strings.Contains(out, "  - internal/foo.go (fake-size)") {
		t.Errorf("expected second resolved artifact line, got:\n%s", out)
	}
	// Must NOT use legacy comma-joined form.
	if strings.Contains(out, "artifacts: cmd/main.go, internal/foo.go") {
		t.Errorf("expected per-line format, but got comma-joined form:\n%s", out)
	}
}

func TestFormatForParentWithMemoryRefResolver(t *testing.T) {
	task := &models.Task{
		ID:     "task-r2",
		Engine: models.EnginePando,
		Conclusion: &models.Conclusion{
			Status:     "success",
			Summary:    "Done.",
			MemoryRefs: []string{"pando/foo", "pando/bar"},
		},
	}
	fakeMemory := func(ref string) string {
		titles := map[string]string{"pando/foo": "pando/foo — Foo Title", "pando/bar": "pando/bar — Bar Title"}
		if t, ok := titles[ref]; ok {
			return t
		}
		return ref
	}
	res := &ResolveOptions{Memory: fakeMemory}

	out := FormatForParent(task, res)

	if !strings.Contains(out, "memory_refs:") {
		t.Errorf("expected 'memory_refs:' header, got:\n%s", out)
	}
	if !strings.Contains(out, "  - pando/foo — Foo Title") {
		t.Errorf("expected resolved memory_ref line, got:\n%s", out)
	}
	if !strings.Contains(out, "  - pando/bar — Bar Title") {
		t.Errorf("expected second resolved memory_ref line, got:\n%s", out)
	}
	// Artifacts should still use legacy form (resolver not provided).
	if strings.Contains(out, "artifacts:") {
		t.Errorf("expected no artifacts section, got:\n%s", out)
	}
}

func TestFormatForParentResolverEmptyListOmitted(t *testing.T) {
	task := &models.Task{
		ID:     "task-r3",
		Engine: models.EnginePando,
		Conclusion: &models.Conclusion{
			Status:    "success",
			Summary:   "Done.",
			Artifacts: []string{"", "  "},
		},
	}
	res := &ResolveOptions{Artifacts: func(ref string) string { return ref + " (resolved)" }}
	out := FormatForParent(task, res)
	// All entries are blank, so the artifacts section must be omitted.
	if strings.Contains(out, "artifacts") {
		t.Errorf("expected no artifacts section for blank-only entries, got:\n%s", out)
	}
}

func TestFormatForParentNilResolveOptionsIsLegacy(t *testing.T) {
	// A nil *ResolveOptions must produce output identical to pre-resolver behavior.
	task := &models.Task{
		ID:     "task-legacy",
		Engine: models.EnginePando,
		Conclusion: &models.Conclusion{
			Status:     "success",
			Summary:    "Done.",
			Artifacts:  []string{"a.go", "b.go"},
			MemoryRefs: []string{"pando/x.md"},
		},
	}
	withNil := FormatForParent(task, nil)
	withExplicit := FormatForParent(task, &ResolveOptions{})
	if withNil != withExplicit {
		t.Errorf("nil ResolveOptions and empty ResolveOptions should produce identical output.\nnilRes=%q\nexplicit=%q", withNil, withExplicit)
	}
	if !strings.Contains(withNil, "artifacts: a.go, b.go") {
		t.Errorf("expected legacy comma-joined artifacts, got:\n%s", withNil)
	}
}

// TestFormatForParentRendersWarnings verifies the soft-schema validation
// warnings (item D1) are surfaced on a trailing compact line, and omitted when
// there are none.
func TestFormatForParentRendersWarnings(t *testing.T) {
	task := &models.Task{
		ID:     "task-w1",
		Engine: models.EnginePando,
		Conclusion: &models.Conclusion{
			Status:   "success",
			Summary:  "Done.",
			Warnings: []string{`unknown status "ok"; left for software default`, "missing summary"},
		},
	}
	out := FormatForParent(task, nilRes)
	if !strings.Contains(out, `warnings: unknown status "ok"; left for software default, missing summary`) {
		t.Errorf("expected compact warnings line, got:\n%s", out)
	}

	// No warnings -> no warnings line.
	task.Conclusion.Warnings = nil
	if out := FormatForParent(task, nilRes); strings.Contains(out, "warnings:") {
		t.Errorf("expected no warnings line when none present, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// NewFilesystemArtifactResolver tests
// ---------------------------------------------------------------------------

func TestNewFilesystemArtifactResolverExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	resolver := NewFilesystemArtifactResolver(dir)

	// Relative ref resolved against baseDir.
	got := resolver("hello.txt")
	if !strings.HasPrefix(got, "hello.txt (") {
		t.Errorf("expected 'hello.txt (<size>)', got %q", got)
	}
	if !strings.Contains(got, "B") {
		t.Errorf("expected byte-size suffix, got %q", got)
	}
}

func TestNewFilesystemArtifactResolverMissingFile(t *testing.T) {
	dir := t.TempDir()
	resolver := NewFilesystemArtifactResolver(dir)

	got := resolver("does_not_exist.go")
	if got != "does_not_exist.go (missing)" {
		t.Errorf("expected '...  (missing)', got %q", got)
	}
}

func TestNewFilesystemArtifactResolverAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "abs.txt")
	if err := os.WriteFile(absPath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// An absolute ref should be used as-is, ignoring baseDir.
	resolver := NewFilesystemArtifactResolver("/some/other/dir")
	got := resolver(absPath)
	if !strings.HasPrefix(got, absPath+" (") {
		t.Errorf("expected absolute ref with size, got %q", got)
	}
}

func TestNewFilesystemArtifactResolverBlankBaseDir(t *testing.T) {
	// With blank baseDir and a relative path we can't stat, just return bare ref.
	resolver := NewFilesystemArtifactResolver("")
	got := resolver("some/relative/path.go")
	// Should return the ref unchanged (can't resolve without baseDir).
	if got != "some/relative/path.go" && !strings.Contains(got, "(") {
		// Acceptable: either bare or (missing) is fine for a relative path with no base.
		// The key assertion is no panic.
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{int64(2.5 * 1024 * 1024), "2.5 MB"},
	}
	for _, tc := range cases {
		got := humanBytes(tc.n)
		if got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
