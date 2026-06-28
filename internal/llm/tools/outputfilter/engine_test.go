package outputfilter

import (
	"os"
	"strings"
	"testing"
)

// trimTrailingNewlines normalises trailing newline differences, which are
// irrelevant for token reduction, so inline test expectations stay readable.
func trimTrailingNewlines(s string) string { return strings.TrimRight(s, "\n") }

// TestEmbeddedFiltersCompile ensures every built-in filter parses and compiles.
func TestEmbeddedFiltersCompile(t *testing.T) {
	e, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults returned error: %v", err)
	}
	if len(e.Filters()) == 0 {
		t.Fatal("expected at least one embedded filter")
	}
}

// TestEmbeddedInlineTests runs the [[filters.<name>.tests]] cases declared in
// defaults.toml against their owning filter's pipeline.
func TestEmbeddedInlineTests(t *testing.T) {
	e, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults returned error: %v", err)
	}
	ran := 0
	for _, f := range e.Filters() {
		for _, tc := range f.Tests {
			ran++
			t.Run(f.Name()+"/"+tc.Name, func(t *testing.T) {
				got, ok := f.run(tc.Input)
				if !ok {
					t.Fatalf("filter %q reported failure on input", f.Name())
				}
				if trimTrailingNewlines(got) != trimTrailingNewlines(tc.Expected) {
					t.Errorf("filter %q mismatch\n--- got ---\n%q\n--- want ---\n%q",
						f.Name(), got, tc.Expected)
				}
			})
		}
	}
	if ran == 0 {
		t.Fatal("no inline filter tests were executed")
	}
}

// TestApplyNoMatchReturnsRaw verifies a command with no matching filter is
// returned unchanged.
func TestApplyNoMatchReturnsRaw(t *testing.T) {
	e, _ := LoadDefaults()
	raw := "some arbitrary output\nwith lines"
	got, applied, name := e.Apply("ls -la /tmp", raw)
	if applied {
		t.Errorf("expected no filter to apply, got filter %q", name)
	}
	if got != raw {
		t.Errorf("expected raw output unchanged, got %q", got)
	}
}

// TestApplySelectsMatchingFilter verifies command routing reaches the filter.
func TestApplySelectsMatchingFilter(t *testing.T) {
	e, _ := LoadDefaults()
	out, applied, name := e.Apply("git status", "nothing to commit, working tree clean\n")
	if !applied {
		t.Fatal("expected git-status filter to apply")
	}
	if name != "git-status" {
		t.Errorf("expected filter git-status, got %q", name)
	}
	if !strings.Contains(out, "clean") {
		t.Errorf("expected clean summary, got %q", out)
	}
}

// TestPrecedenceUserOverridesBuiltin verifies filters loaded from a path match
// before the embedded defaults.
func TestPrecedenceUserOverridesBuiltin(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/filters.toml"
	custom := `[filters.git-status-override]
match_command = '(^|\s)git\s+status(\s|$)'
match_output = [ { pattern = '.*', message = "CUSTOM" } ]
`
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatalf("write custom filter: %v", err)
	}
	e, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out, applied, name := e.Apply("git status", "anything at all")
	if !applied {
		t.Fatal("expected a filter to apply")
	}
	if name != "git-status-override" {
		t.Errorf("expected user filter to win, got %q", name)
	}
	if out != "CUSTOM" {
		t.Errorf("expected CUSTOM, got %q", out)
	}
}

// TestBadFilterIsSkipped verifies a malformed filter does not abort loading.
func TestBadFilterIsSkipped(t *testing.T) {
	filters, err := parseFile([]byte(`[filters.broken]
match_command = '('
`), "test")
	if err == nil {
		t.Error("expected an error for invalid regex")
	}
	if len(filters) != 0 {
		t.Errorf("expected broken filter to be skipped, got %d", len(filters))
	}
}
