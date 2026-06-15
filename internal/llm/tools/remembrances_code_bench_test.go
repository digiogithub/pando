package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/rag/code"
)

// makeBenchResults builds n realistic hybrid search results, each carrying a
// full CodeSymbol with the heavy fields (source_code, doc_string, metadata, byte
// offsets) that the ORIGINAL tools dumped verbatim into the response. Scores
// decay so ranking/normalization/cutoff behave realistically.
func makeBenchResults(n int) []code.HybridSearchResult {
	results := make([]code.HybridSearchResult, n)
	body := strings.Repeat("\tresult := doWork(ctx, input)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n", 6)
	for i := 0; i < n; i++ {
		results[i] = code.HybridSearchResult{
			Score: float64(n-i) / float64(n),
			Symbol: &code.CodeSymbol{
				ID:         fmt.Sprintf("sym-%04d", i),
				ProjectID:  "pando",
				FilePath:   fmt.Sprintf("internal/app/service_%d.go", i%5),
				SymbolType: "function",
				Name:       fmt.Sprintf("HandleRequest%d", i),
				NamePath:   fmt.Sprintf("/Service/HandleRequest%d", i),
				StartLine:  10 + i,
				EndLine:    40 + i,
				StartByte:  1000 + i*200,
				EndByte:    2000 + i*200,
				Signature:  fmt.Sprintf("func (s *Service) HandleRequest%d(ctx context.Context, input Input) (*Output, error)", i),
				SourceCode: "func (s *Service) HandleRequest" + fmt.Sprint(i) + "() {\n" + body + "}",
				DocString:  "HandleRequest processes an incoming request and returns the computed output or an error.",
				Metadata: map[string]interface{}{
					"visibility": "public",
					"receiver":   "*Service",
					"complexity": i % 7,
				},
			},
		}
	}
	return results
}

// TestCompactFormatTokenReduction quantifies the Phase 1-4 token savings: it
// compares the byte size of the compact one-line-per-result page against the
// verbose baseline (raw CodeSymbol list serialized as JSON, as the original
// tools returned). It fails if the reduction regresses below a conservative
// threshold, and logs the measured ratio for visibility.
func TestCompactFormatTokenReduction(t *testing.T) {
	const total = 40
	const pageLimit = 20
	results := makeBenchResults(total)

	// Verbose baseline: what the old code_search_pattern dumped — the full raw
	// symbols (source_code, doc_string, metadata, byte offsets) for the page.
	baselineSymbols := make([]*code.CodeSymbol, 0, pageLimit)
	for i := 0; i < pageLimit; i++ {
		baselineSymbols = append(baselineSymbols, results[i].Symbol)
	}
	baselineJSON, err := json.Marshal(baselineSymbols)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	baselineBytes := len(baselineJSON)

	// New path: rank/filter the full candidate set, page it, render compact.
	ranked := rankAndFilterHybrid(results, 0, false, false)
	page, meta := paginate(ranked, 0, pageLimit)
	compact := renderCompact(toRows(page), meta, false)
	compactBytes := len(compact)

	if compactBytes == 0 {
		t.Fatal("compact output is empty")
	}
	reduction := 1 - float64(compactBytes)/float64(baselineBytes)
	t.Logf("token reduction: baseline=%dB compact=%dB (~%dvs%d est. tokens) reduction=%.1f%%",
		baselineBytes, compactBytes, baselineBytes/4, compactBytes/4, reduction*100)

	// Conservative floor: the compact format must be at least 80% smaller than
	// the raw-symbol dump. In practice it is far higher; this guards regressions.
	if reduction < 0.80 {
		t.Errorf("token reduction regressed: got %.1f%%, want >= 80%% (baseline=%dB compact=%dB)",
			reduction*100, baselineBytes, compactBytes)
	}

	// group_by_file should be no larger than the flat form (it removes repeated
	// file paths) for a result set with many hits sharing files.
	grouped := renderCompact(toRows(page), meta, true)
	if len(grouped) > compactBytes {
		t.Errorf("group_by_file output (%dB) larger than flat (%dB)", len(grouped), compactBytes)
	}
}

// toRows mirrors the projection each search tool performs before rendering.
func toRows(items []hybridResultItem) []compactRow {
	rows := make([]compactRow, len(items))
	for i, it := range items {
		rows[i] = compactRow{
			Rank:      it.Rank,
			Score:     it.Score,
			Kind:      it.Kind,
			FilePath:  it.FilePath,
			StartLine: it.StartLine,
			NamePath:  firstNonEmpty(it.NamePath, it.Name),
			Trailer:   firstNonEmpty(it.Signature, it.Snippet, it.Name),
		}
	}
	return rows
}

// TestHybridPipelinePagination exercises the full rank -> paginate -> render
// pipeline across pages, asserting ranks stay stable and pages are consistent.
func TestHybridPipelinePagination(t *testing.T) {
	results := makeBenchResults(25)
	ranked := rankAndFilterHybrid(results, 0, false, false)

	// Page 1.
	p1, m1 := paginate(ranked, 0, 10)
	if len(p1) != 10 || !m1.HasMore || m1.NextOffset != 10 {
		t.Fatalf("page1 meta = %+v len=%d", m1, len(p1))
	}
	if p1[0].Rank != 1 || p1[0].Score != 1.0 {
		t.Errorf("top result rank/score = %d/%v, want 1/1.0", p1[0].Rank, p1[0].Score)
	}

	// Page 2 continues the rank sequence without gaps or overlaps.
	p2, m2 := paginate(ranked, m1.NextOffset, 10)
	if p2[0].Rank != 11 {
		t.Errorf("page2 first rank = %d, want 11", p2[0].Rank)
	}
	if !m2.HasMore {
		t.Errorf("page2 should have more (25 results, offset 10, limit 10)")
	}

	// Rendered pages carry the footer and never contain raw source_code.
	body := renderCompact(toRows(p1), m1, false)
	if !strings.Contains(body, "has_more=true") || !strings.Contains(body, "next_offset=10") {
		t.Errorf("missing pagination footer:\n%s", body)
	}
	if strings.Contains(body, "doWork(ctx, input)") {
		t.Errorf("compact body leaked source_code:\n%s", body)
	}
	// One line per result + one footer line.
	if got := strings.Count(strings.TrimRight(body, "\n"), "\n"); got != len(p1) {
		t.Errorf("expected %d newlines (one per result, footer last), got %d", len(p1), got)
	}
}

func BenchmarkRenderCompactFlat(b *testing.B) {
	results := makeBenchResults(50)
	ranked := rankAndFilterHybrid(results, 0, false, false)
	page, meta := paginate(ranked, 0, 50)
	rows := toRows(page)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderCompact(rows, meta, false)
	}
}

func BenchmarkRenderCompactGrouped(b *testing.B) {
	results := makeBenchResults(50)
	ranked := rankAndFilterHybrid(results, 0, false, false)
	page, meta := paginate(ranked, 0, 50)
	rows := toRows(page)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderCompact(rows, meta, true)
	}
}

// BenchmarkVerboseBaseline measures the cost of the original verbose path
// (raw-symbol JSON), for comparison against the compact renderer above.
func BenchmarkVerboseBaseline(b *testing.B) {
	results := makeBenchResults(50)
	symbols := make([]*code.CodeSymbol, len(results))
	for i := range results {
		symbols[i] = results[i].Symbol
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(symbols)
	}
}
