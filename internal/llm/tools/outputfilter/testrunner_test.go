package outputfilter

import "testing"

const sampleFilterFile = `
[filters.echo-demo]
description = "demo: drop blank lines"
match_command = '^echo\b'
strip_lines_matching = ['^\s*$']

[[filters.echo-demo.tests]]
name = "drops blank lines"
input = "hello\n\nworld\n"
expected = "hello\nworld"

[[filters.echo-demo.tests]]
name = "intentionally wrong"
input = "a\nb\n"
expected = "WRONG"
`

func TestLoadFileFiltersRunsInlineTests(t *testing.T) {
	eng, err := LoadFileFilters("sample.toml", []byte(sampleFilterFile))
	if err != nil {
		t.Fatalf("LoadFileFilters error: %v", err)
	}
	results := eng.RunInlineTests()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	byName := map[string]FilterTestResult{}
	for _, r := range results {
		if r.Filter != "echo-demo" {
			t.Errorf("unexpected filter %q", r.Filter)
		}
		byName[r.Test] = r
	}
	if !byName["drops blank lines"].Passed {
		t.Errorf("expected the first case to pass: %+v", byName["drops blank lines"])
	}
	if byName["intentionally wrong"].Passed {
		t.Errorf("expected the second case to fail")
	}
	if got := byName["intentionally wrong"].Got; got != "a\nb" {
		t.Errorf("unexpected got for failing case: %q", got)
	}
}

func TestLoadFileFiltersNoDefaults(t *testing.T) {
	// A file with no filters yields an engine with no inline tests, and the
	// built-in defaults must NOT leak in.
	eng, err := LoadFileFilters("empty.toml", []byte("# nothing here\n"))
	if err != nil {
		t.Fatalf("LoadFileFilters error: %v", err)
	}
	if got := eng.RunInlineTests(); len(got) != 0 {
		t.Fatalf("expected no inline tests, got %d", len(got))
	}
	if got := len(eng.Filters()); got != 0 {
		t.Fatalf("expected no filters (defaults must not leak), got %d", got)
	}
}
