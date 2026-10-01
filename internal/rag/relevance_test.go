package rag

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// scriptFilter drops the candidates whose ID is in drop.
type scriptFilter struct {
	drop    map[string]bool
	applied bool
	calls   int
	mu      sync.Mutex
	seen    []RelevanceCandidate
	prompt  string
}

func (f *scriptFilter) Filter(_ context.Context, prompt string, cands []RelevanceCandidate) ([]bool, FilterResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.prompt = prompt
	f.seen = append([]RelevanceCandidate(nil), cands...)
	keep := make([]bool, len(cands))
	for i, c := range cands {
		keep[i] = !f.drop[c.ID]
	}
	return keep, FilterResult{Applied: true}
}

func testHits() (code, kbh, ev []enrichHit) {
	code = []enrichHit{
		{cand: RelevanceCandidate{Source: SourceCode, ID: "A"}, entry: "- **function** `A` — a.go:1 (0.90)\n  doc A\n"},
		{cand: RelevanceCandidate{Source: SourceCode, ID: "B"}, entry: "- **function** `B` — b.go:2 (0.80)\n"},
	}
	kbh = []enrichHit{
		{cand: RelevanceCandidate{Source: SourceKB, ID: "k1"}, entry: "- **docs/one.md** (0.70)\n  first excerpt\n"},
		{cand: RelevanceCandidate{Source: SourceKB, ID: "k2"}, entry: "- **docs/two.md** (0.60)\n  second excerpt\n"},
	}
	ev = []enrichHit{
		{cand: RelevanceCandidate{Source: SourceEvents, ID: "e1"}, entry: "- [2026-01-02] **general** (0.50): did a thing\n"},
	}
	return
}

func newTestEnricher(cfg EnricherConfig) *ContextEnricher {
	return NewContextEnricher(&RemembrancesService{}, cfg)
}

const goldenAll = "<context source=\"remembrances\">\n" +
	"## Code Index\n- **function** `A` — a.go:1 (0.90)\n  doc A\n- **function** `B` — b.go:2 (0.80)\n" +
	"\n\n## Knowledge Base\n- **docs/one.md** (0.70)\n  first excerpt\n- **docs/two.md** (0.60)\n  second excerpt\n" +
	"\n\n## Past Session Events\n- [2026-01-02] **general** (0.50): did a thing\n" +
	"\n</context>"

func TestAssembleFilterOffIsGolden(t *testing.T) {
	e := newTestEnricher(EnricherConfig{})
	c, k, ev := testHits()
	got, res := e.assemble(context.Background(), "q", c, k, ev)
	if got != goldenAll {
		t.Fatalf("filter-off output changed:\n%q\nwant\n%q", got, goldenAll)
	}
	if res.Applied || res.Kept != 0 || res.Dropped != 0 {
		t.Fatalf("no filter must yield a zero result, got %+v", res)
	}
}

func TestAssembleKeepAllFilterMatchesGolden(t *testing.T) {
	e := newTestEnricher(EnricherConfig{})
	f := &scriptFilter{}
	e.SetRelevanceFilter(f)
	c, k, ev := testHits()
	got, res := e.assemble(context.Background(), "my prompt", c, k, ev)
	if got != goldenAll {
		t.Fatalf("keep-all output differs from golden: %q", got)
	}
	if f.calls != 1 || f.prompt != "my prompt" || len(f.seen) != 5 {
		t.Fatalf("filter must run once over all 5 candidates, calls=%d seen=%d", f.calls, len(f.seen))
	}
	// Order handed to the filter: code, kb, events.
	if f.seen[0].ID != "A" || f.seen[2].ID != "k1" || f.seen[4].ID != "e1" {
		t.Fatalf("unexpected candidate order: %+v", f.seen)
	}
	if !res.Applied || res.Kept != 5 || res.Dropped != 0 || res.BySource[SourceKB] != [2]int{2, 0} {
		t.Fatalf("result = %+v", res)
	}
}

func TestAssembleFilterDropsCandidatesAndSections(t *testing.T) {
	e := newTestEnricher(EnricherConfig{})
	e.SetRelevanceFilter(&scriptFilter{drop: map[string]bool{"B": true, "k1": true, "e1": true}})
	c, k, ev := testHits()
	got, res := e.assemble(context.Background(), "q", c, k, ev)
	for _, gone := range []string{"`B`", "one.md", "Past Session Events", "did a thing"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%q must be dropped:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"`A`", "two.md", "## Code Index", "## Knowledge Base"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("%q must be kept:\n%s", kept, got)
		}
	}
	if res.Kept != 2 || res.Dropped != 3 || res.BySource[SourceCode] != [2]int{1, 1} || res.BySource[SourceEvents] != [2]int{0, 1} {
		t.Fatalf("result = %+v", res)
	}
}

func TestAssembleBudgetsApplyToFilteredSet(t *testing.T) {
	c, k, ev := testHits()
	cfg := EnricherConfig{KBMaxChars: 40}
	// Without a filter the KB budget cuts the section after the first entry.
	e := newTestEnricher(cfg)
	got, _ := e.assemble(context.Background(), "q", c, k, ev)
	if strings.Contains(got, "two.md") {
		t.Fatalf("budget should cut the second KB entry:\n%s", got)
	}
	// Dropping the first KB hit lets the second one use the budget.
	e.SetRelevanceFilter(&scriptFilter{drop: map[string]bool{"k1": true}})
	got, _ = e.assemble(context.Background(), "q", c, k, ev)
	if !strings.Contains(got, "two.md") || strings.Contains(got, "one.md") {
		t.Fatalf("budget must apply to the filtered set:\n%s", got)
	}

	// The total budget too: dropping the code section changes what gets truncated.
	e2 := newTestEnricher(EnricherConfig{TotalMaxChars: 120})
	e2.SetRelevanceFilter(&scriptFilter{drop: map[string]bool{"A": true, "B": true}})
	got, _ = e2.assemble(context.Background(), "q", c, k, ev)
	if !strings.Contains(got, "one.md") {
		t.Fatalf("total budget must count only kept sections:\n%s", got)
	}
}

type badMaskFilter struct{}

func (badMaskFilter) Filter(context.Context, string, []RelevanceCandidate) ([]bool, FilterResult) {
	return []bool{false}, FilterResult{Applied: true}
}

func TestAssembleMalformedMaskKeepsEverything(t *testing.T) {
	e := newTestEnricher(EnricherConfig{})
	e.SetRelevanceFilter(badMaskFilter{})
	c, k, ev := testHits()
	got, res := e.assemble(context.Background(), "q", c, k, ev)
	if got != goldenAll || res.Applied || res.Reason == "" {
		t.Fatalf("malformed mask must fail open, applied=%v reason=%q", res.Applied, res.Reason)
	}
}

func TestSetRelevanceFilterNilTurnsItOffAndIsRaceFree(t *testing.T) {
	e := newTestEnricher(EnricherConfig{})
	e.SetRelevanceFilter(&scriptFilter{drop: map[string]bool{"A": true}})
	c, k, ev := testHits()
	if got, _ := e.assemble(context.Background(), "q", c, k, ev); strings.Contains(got, "`A`") {
		t.Fatal("filter should drop A")
	}
	e.SetRelevanceFilter(nil)
	if got, res := e.assemble(context.Background(), "q", c, k, ev); got != goldenAll || res.Applied {
		t.Fatal("nil filter must restore the unfiltered output")
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				e.SetRelevanceFilter(&scriptFilter{})
				e.SetRelevanceFilter(nil)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				e.assemble(context.Background(), "q", c, k, ev)
			}
		}()
	}
	wg.Wait()
}

func TestFilterMemoriesPinnedNeverDropped(t *testing.T) {
	mk := func(id int64, key, scope string) kb.MemoryResult {
		return kb.MemoryResult{Document: kb.Document{ID: id, MemoryKey: key, MemoryScope: scope, Content: "content " + key}}
	}
	mems := []kb.MemoryResult{mk(1, "pinned", "global/"), mk(2, "other", "project/"), mk(3, "third", "")}
	cfg := config.RemembrancesConfig{MemoryPinnedScopes: []string{"global/"}}
	// The filter tries to drop everything.
	f := &scriptFilter{drop: map[string]bool{"pinned": true, "other": true, "third": true}}
	kept, res := filterMemories(context.Background(), mems, "q", cfg, f)
	if len(kept) != 1 || kept[0].Document.MemoryKey != "pinned" {
		t.Fatalf("only the pinned memory must survive, got %+v", kept)
	}
	if res.Kept != 1 || res.Dropped != 2 || res.BySource[SourceMemory] != [2]int{1, 2} {
		t.Fatalf("result = %+v", res)
	}
	if !f.seen[0].Pinned || f.seen[1].Pinned {
		t.Fatalf("pinned flag not propagated: %+v", f.seen)
	}

	// Off: untouched.
	all, res := filterMemories(context.Background(), mems, "q", cfg, nil)
	if len(all) != 3 || res.Applied {
		t.Fatal("nil filter must keep all memories")
	}
}

func TestApplyRelevanceFilterSkippedWhenContextDisablesIt(t *testing.T) {
	f := &scriptFilter{drop: map[string]bool{"A": true}}
	cands := []RelevanceCandidate{{Source: SourceCode, ID: "A"}, {Source: SourceKB, ID: "B"}}
	keep, res := applyRelevanceFilter(WithoutRelevanceFilter(context.Background()), f, "q", cands)
	if f.calls != 0 || len(keep) != 2 || !keep[0] || !keep[1] || res.Applied || res.Dropped != 0 {
		t.Fatalf("filter ran on an ineligible context: calls=%d keep=%v res=%+v", f.calls, keep, res)
	}
}
