package tools

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/rag/code"
)

func TestTruncateField(t *testing.T) {
	short := "func Foo()"
	if got := truncateField(short); got != short {
		t.Errorf("short field changed: %q", got)
	}

	long := strings.Repeat("a", maxSearchFieldLen+50)
	got := truncateField(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated field missing ellipsis: %q", got)
	}
	if r := []rune(got); len(r) != maxSearchFieldLen+1 {
		t.Errorf("truncated field has %d runes, want %d", len(r), maxSearchFieldLen+1)
	}

	// Multi-byte runes must not be split.
	multibyte := strings.Repeat("é", maxSearchFieldLen+50)
	if !strings.HasSuffix(truncateField(multibyte), "…") {
		t.Errorf("multibyte truncation failed")
	}
}

func TestIsDocFile(t *testing.T) {
	docs := []string{"README.md", "docs/guide.MARKDOWN", "a.rst", "notes.txt", "x.mdx"}
	for _, p := range docs {
		if !isDocFile(p) {
			t.Errorf("isDocFile(%q) = false, want true", p)
		}
	}
	code := []string{"main.go", "app.ts", "x.py", "noext", "Makefile"}
	for _, p := range code {
		if isDocFile(p) {
			t.Errorf("isDocFile(%q) = true, want false", p)
		}
	}
}

func hres(path string, score float64) code.HybridSearchResult {
	return code.HybridSearchResult{
		Symbol: &code.CodeSymbol{FilePath: path, Name: "Sym", SymbolType: "function"},
		Score:  score,
	}
}

func TestRankAndFilterHybrid(t *testing.T) {
	// Doc files excluded by default; scores normalized to 0..1; tail trimmed.
	results := []code.HybridSearchResult{
		hres("a.go", 10),
		hres("README.md", 9), // doc, must be dropped by default
		hres("b.go", 5),
		hres("c.go", 0.5), // below 15% of top (10) -> trimmed
	}
	items := rankAndFilterHybrid(results, 0, false, false)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (%v)", len(items), items)
	}
	if items[0].Score != 1.0 {
		t.Errorf("top score = %v, want 1.0", items[0].Score)
	}
	if items[1].Score != 0.5 {
		t.Errorf("second score = %v, want 0.5", items[1].Score)
	}
	for _, it := range items {
		if it.Kind != "code" {
			t.Errorf("unexpected kind %q for %q", it.Kind, it.FilePath)
		}
	}

	// include_docs keeps the markdown result and tags it as doc.
	withDocs := rankAndFilterHybrid(results, 0, true, false)
	foundDoc := false
	for _, it := range withDocs {
		if it.FilePath == "README.md" {
			foundDoc = true
			if it.Kind != "doc" {
				t.Errorf("README.md kind = %q, want doc", it.Kind)
			}
		}
	}
	if !foundDoc {
		t.Error("include_docs did not keep README.md")
	}

	// Always keep at least the top result even if everything is below cutoff.
	one := rankAndFilterHybrid([]code.HybridSearchResult{hres("a.go", 10), hres("b.go", 0.1)}, 0.99, false, false)
	if len(one) != 1 {
		t.Fatalf("aggressive cutoff kept %d, want 1", len(one))
	}

	// Debug exposes raw score breakdown.
	dbg := rankAndFilterHybrid([]code.HybridSearchResult{hres("a.go", 10)}, 0, false, true)
	if dbg[0].RawScore != 10 {
		t.Errorf("debug raw_score = %v, want 10", dbg[0].RawScore)
	}
}

func TestPaginate(t *testing.T) {
	items := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	// First page: has_more true, next_offset set, accurate total.
	page, meta := paginate(items, 0, 3)
	if len(page) != 3 || page[0] != 0 || page[2] != 2 {
		t.Fatalf("page = %v", page)
	}
	if meta.Total != 10 || !meta.HasMore || meta.NextOffset != 3 {
		t.Errorf("meta = %+v, want total=10 has_more=true next_offset=3", meta)
	}

	// Last partial page: has_more false.
	page, meta = paginate(items, 9, 3)
	if len(page) != 1 || page[0] != 9 {
		t.Fatalf("last page = %v", page)
	}
	if meta.HasMore {
		t.Errorf("last page has_more = true, want false")
	}

	// Offset past the end yields an empty page.
	page, meta = paginate(items, 50, 3)
	if len(page) != 0 || meta.HasMore {
		t.Errorf("past-end page = %v meta = %+v", page, meta)
	}
}

func TestPaginateFetched(t *testing.T) {
	// Sentinel present (fetched offset+limit+1) -> has_more true, lower bound.
	withSentinel := []int{0, 1, 2, 3} // offset 0, limit 3, one extra
	page, meta := paginateFetched(withSentinel, 0, 3)
	if len(page) != 3 {
		t.Fatalf("page len = %d, want 3", len(page))
	}
	if !meta.HasMore || meta.NextOffset != 3 || !meta.TotalIsLowerBound {
		t.Errorf("meta = %+v, want has_more=true next_offset=3 lower_bound=true", meta)
	}

	// No sentinel (exactly the page) -> has_more false, exact total.
	exact := []int{0, 1, 2}
	page, meta = paginateFetched(exact, 0, 3)
	if len(page) != 3 || meta.HasMore || meta.TotalIsLowerBound {
		t.Errorf("exact page meta = %+v", meta)
	}
}

func TestCompactLine(t *testing.T) {
	line := compactLine(1, 0.873, "function", "internal/app/x.go", 42, "/App.Handler", "func (a *App) Handler()")
	want := "#1 0.873 function internal/app/x.go:42  /App.Handler  func (a *App) Handler()"
	if line != want {
		t.Errorf("compactLine =\n  %q\nwant\n  %q", line, want)
	}
	if strings.Contains(line, "\n") {
		t.Error("compact line must not contain a newline")
	}

	// Zero score is omitted (used by non-scored tools).
	noScore := compactLine(2, 0, "method", "a.go", 1, "/Foo", "")
	if strings.Contains(noScore, "0.000") {
		t.Errorf("zero score should be omitted: %q", noScore)
	}
}

func TestCompactPaginationFooter(t *testing.T) {
	more := compactPaginationFooter(paginationMeta{Total: 10, Offset: 0, Limit: 3, HasMore: true, NextOffset: 3}, 3)
	if !strings.Contains(more, "has_more=true") || !strings.Contains(more, "next_offset=3") || !strings.Contains(more, "of 10") {
		t.Errorf("footer = %q", more)
	}

	lower := compactPaginationFooter(paginationMeta{Total: 6, Offset: 3, Limit: 3, HasMore: true, NextOffset: 6, TotalIsLowerBound: true}, 3)
	if !strings.Contains(lower, "of 6+") {
		t.Errorf("lower-bound footer should show 'of 6+': %q", lower)
	}
}

func TestRenderCompactFlat(t *testing.T) {
	rows := []compactRow{
		{Rank: 1, Score: 0.9, Kind: "function", FilePath: "a.go", StartLine: 10, NamePath: "/A", Trailer: "func A()"},
		{Rank: 2, Score: 0.5, Kind: "method", FilePath: "a.go", StartLine: 20, NamePath: "/A/B", Trailer: "func B()"},
	}
	meta := paginationMeta{Total: 2, Offset: 0, Limit: 50}
	out := renderCompact(rows, meta, false)
	// Flat mode repeats the file path on each line and has no group header.
	if strings.Count(out, "a.go:") != 2 {
		t.Errorf("flat mode should print file:line per row:\n%s", out)
	}
	if strings.Contains(out, "a.go (2)") {
		t.Errorf("flat mode must not emit a group header:\n%s", out)
	}
	if !strings.Contains(out, "has_more=false") {
		t.Errorf("missing footer:\n%s", out)
	}
}

func TestRenderCompactGroupByFile(t *testing.T) {
	rows := []compactRow{
		{Rank: 1, Score: 0.9, Kind: "function", FilePath: "a.go", StartLine: 10, NamePath: "/A", Trailer: "func A()"},
		{Rank: 2, Score: 0.7, Kind: "function", FilePath: "b.go", StartLine: 5, NamePath: "/X", Trailer: "func X()"},
		{Rank: 3, Score: 0.5, Kind: "method", FilePath: "a.go", StartLine: 20, NamePath: "/A/B", Trailer: "func B()"},
	}
	meta := paginationMeta{Total: 3, Offset: 0, Limit: 50}
	out := renderCompact(rows, meta, true)

	// Each file appears once as a header with its hit count; a.go groups both
	// its rows together preserving first-appearance order (a.go before b.go).
	if !strings.Contains(out, "a.go (2)") {
		t.Errorf("missing a.go group header with count:\n%s", out)
	}
	if !strings.Contains(out, "b.go (1)") {
		t.Errorf("missing b.go group header with count:\n%s", out)
	}
	if strings.Index(out, "a.go (2)") > strings.Index(out, "b.go (1)") {
		t.Errorf("first-appearance order not preserved:\n%s", out)
	}
	// Grouped rows drop the repeated file path and use the L<line> form.
	if strings.Contains(out, "a.go:10") {
		t.Errorf("grouped rows must not repeat the file path:\n%s", out)
	}
	if !strings.Contains(out, "L10") || !strings.Contains(out, "L20") {
		t.Errorf("grouped rows should use L<line> form:\n%s", out)
	}
}

func TestCompactLineCoreNoFile(t *testing.T) {
	line := compactLineCore(1, 0, "function", "a.go", 42, "/A", "func A()", false)
	if strings.Contains(line, "a.go") {
		t.Errorf("withFile=false must omit the file path: %q", line)
	}
	if !strings.Contains(line, "L42") {
		t.Errorf("withFile=false should render L42: %q", line)
	}
}

func TestFirstSourceLine(t *testing.T) {
	sym := &code.CodeSymbol{SourceCode: "\n   \n  return doStuff()\nmore"}
	if got := firstSourceLine(sym); got != "return doStuff()" {
		t.Errorf("firstSourceLine = %q, want %q", got, "return doStuff()")
	}
	if got := firstSourceLine(&code.CodeSymbol{}); got != "" {
		t.Errorf("firstSourceLine empty = %q, want empty", got)
	}
	if got := firstSourceLine(nil); got != "" {
		t.Errorf("firstSourceLine(nil) = %q, want empty", got)
	}
}

func TestMatchSnippet(t *testing.T) {
	sym := &code.CodeSymbol{
		Signature: "func Handler()",
		SourceCode: "func Handler() {\n\tauthenticate(user)\n\treturn nil\n}",
	}

	// Case-insensitive match returns the matching line, trimmed.
	if got := matchSnippet(sym, "AUTHENTICATE", false); got != "authenticate(user)" {
		t.Errorf("matchSnippet match = %q, want %q", got, "authenticate(user)")
	}

	// No match falls back to the signature.
	if got := matchSnippet(sym, "nonexistent-token", false); got != "func Handler()" {
		t.Errorf("matchSnippet fallback = %q, want signature", got)
	}

	// No signature and no match falls back to first non-empty source line.
	noSig := &code.CodeSymbol{SourceCode: "\n  first line\n second line"}
	if got := matchSnippet(noSig, "zzz", false); got != "first line" {
		t.Errorf("matchSnippet first-line fallback = %q", got)
	}
}
