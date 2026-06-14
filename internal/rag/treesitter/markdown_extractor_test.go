package treesitter

import (
	"context"
	"strings"
	"testing"
)

// TestMarkdownExtractorNamesAreHeadingsOnly ensures markdown section symbols
// carry only their heading text as Name/NamePath, never the section body. A
// regression here reintroduces multi-thousand-character names that bloat every
// code search result.
func TestMarkdownExtractorNamesAreHeadingsOnly(t *testing.T) {
	src := []byte(`# Top Heading

This is a long body paragraph that must never leak into the symbol name. It
contains multiple sentences and spans several lines so that any accidental
fallback to whole-section content would be obvious in the assertions below.

## Nested Heading

Another body paragraph with plenty of words to detect leakage.
`)

	parser := NewParser()
	tree, err := parser.Parse(context.Background(), src, LanguageMarkdown)
	if err != nil {
		t.Fatalf("parse markdown: %v", err)
	}
	defer tree.Close()

	ex := NewMarkdownExtractor(DefaultWalkerConfig())
	symbols, err := ex.ExtractSymbols(tree, src, "doc.md", "test")
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected at least one markdown symbol")
	}

	foundTop, foundNested := false, false
	for _, s := range symbols {
		if strings.Contains(s.Name, "\n") {
			t.Errorf("symbol name contains newline (body leaked): %q", s.Name)
		}
		if strings.Contains(s.Name, "body paragraph") {
			t.Errorf("symbol name contains body text: %q", s.Name)
		}
		if len(s.Name) > 100 {
			t.Errorf("symbol name unexpectedly long (%d chars): %q", len(s.Name), s.Name)
		}
		switch s.Name {
		case "Top Heading":
			foundTop = true
		case "Nested Heading":
			foundNested = true
		}
	}
	if !foundTop {
		t.Errorf("expected a symbol named %q, got %v", "Top Heading", symbolNames(symbols))
	}
	if !foundNested {
		t.Errorf("expected a symbol named %q, got %v", "Nested Heading", symbolNames(symbols))
	}
}

func TestHeadingTextOnly(t *testing.T) {
	cases := map[string]string{
		"# Title":                       "Title",
		"### Deep Title  ":              "Deep Title",
		"Title\n\nbody text here":       "Title",
		"## Title with # inside":        "Title with # inside",
		"   \n# Late Title":             "Late Title",
		"":                             "",
		"Plain heading\nignored body":   "Plain heading",
	}
	for in, want := range cases {
		if got := headingTextOnly(in); got != want {
			t.Errorf("headingTextOnly(%q) = %q, want %q", in, got, want)
		}
	}
}

func symbolNames(symbols []*CodeSymbol) []string {
	names := make([]string, len(symbols))
	for i, s := range symbols {
		names[i] = s.Name
	}
	return names
}
