package rag

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/rag/events"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// constEmbedder returns the same vector for every text, so every stored item
// matches every query with a deterministic, equal vector score.
type constEmbedder struct{}

func (constEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}
func (constEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}
func (constEmbedder) Dimension() int { return 3 }

// openMigratedDB opens a temp SQLite file and applies the real migration chain,
// so the KB and events tables match production.
func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)
	goose.SetBaseFS(db.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return conn
}

// recordingFilter drops candidates whose ID is in drop and records what it saw.
type recordingFilter struct {
	drop map[string]bool
	seen []RelevanceCandidate
}

func (f *recordingFilter) Filter(_ context.Context, _ string, cands []RelevanceCandidate) ([]bool, FilterResult) {
	f.seen = append([]RelevanceCandidate(nil), cands...)
	keep := make([]bool, len(cands))
	for i, c := range cands {
		keep[i] = !f.drop[c.ID]
	}
	return keep, FilterResult{Applied: true}
}

func TestEnrichContextWithResultAgainstRealStores(t *testing.T) {
	conn := openMigratedDB(t)
	ctx := context.Background()
	kbStore := kb.NewKBStore(conn, constEmbedder{}, 0, 0)
	evStore := events.NewEventStore(conn, constEmbedder{})

	if err := kbStore.AddDocument(ctx, "docs/relevance.md", "relevance filter decision model keeps useful context", nil); err != nil {
		t.Fatalf("AddDocument: %v", err)
	}
	if err := kbStore.AddDocument(ctx, "docs/unrelated.md", "relevance filter cooking recipes for pasta", nil); err != nil {
		t.Fatalf("AddDocument: %v", err)
	}
	if _, err := evStore.SaveEvent(ctx, "work", "relevance filter benchmark run recorded", nil); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}

	svc := &RemembrancesService{KB: kbStore, Events: evStore}
	cfg := EnricherConfig{KBResults: 5, EventsResults: 5, MinScore: 0.001, EventsLastDays: 30}
	query := "relevance filter"

	// Filter off: baseline holds every source.
	base := NewContextEnricher(svc, cfg)
	out, res := base.EnrichContextWithResult(ctx, query)
	if res.Applied || res.Kept != 0 || res.Dropped != 0 {
		t.Fatalf("no filter must give a zero result: %+v", res)
	}
	for _, want := range []string{"docs/relevance.md", "docs/unrelated.md", "Past Session Events"} {
		if !strings.Contains(out, want) {
			t.Fatalf("baseline missing %q:\n%s", want, out)
		}
	}

	// Filter on: drop one KB doc and every event.
	e := NewContextEnricher(svc, cfg)
	f := &recordingFilter{drop: map[string]bool{"docs/unrelated.md": true}}
	e.SetRelevanceFilter(f)
	out, res = e.EnrichContextWithResult(ctx, query)
	if strings.Contains(out, "docs/unrelated.md") {
		t.Fatalf("dropped KB doc still injected:\n%s", out)
	}
	if !strings.Contains(out, "docs/relevance.md") || !strings.Contains(out, "Past Session Events") {
		t.Fatalf("kept candidates missing:\n%s", out)
	}
	if !res.Applied || res.Dropped != 1 || res.BySource[SourceKB] != [2]int{1, 1} || res.BySource[SourceEvents] != [2]int{1, 0} {
		t.Fatalf("result = %+v", res)
	}
	sources := map[string]bool{}
	for _, c := range f.seen {
		sources[c.Source] = true
	}
	if !sources[SourceKB] || !sources[SourceEvents] {
		t.Fatalf("filter must see kb and events candidates: %+v", f.seen)
	}

	// Dropping every candidate of a source removes its section.
	e.SetRelevanceFilter(&recordingFilter{drop: map[string]bool{"docs/unrelated.md": true, "docs/relevance.md": true}})
	out, _ = e.EnrichContextWithResult(ctx, query)
	if strings.Contains(out, "## Knowledge Base") {
		t.Fatalf("empty KB section must be omitted:\n%s", out)
	}

	// WithoutRelevanceFilter keeps everything even with a filter installed.
	out, res = e.EnrichContextWithResult(WithoutRelevanceFilter(ctx), query)
	if res.Applied || !strings.Contains(out, "docs/unrelated.md") {
		t.Fatalf("ineligible turn must not filter: res=%+v out=%s", res, out)
	}
}

func memHits(t *testing.T, conn *sql.DB, key string) int {
	t.Helper()
	var hits int
	if err := conn.QueryRow(`SELECT hits FROM kb_documents WHERE file_path = ?`, key).Scan(&hits); err != nil {
		t.Fatalf("hits(%s): %v", key, err)
	}
	return hits
}

func TestBuildMemoryBlockWithResultAgainstRealStore(t *testing.T) {
	conn := openMigratedDB(t)
	ctx := context.Background()
	store := kb.NewKBStore(conn, constEmbedder{}, 0, 0)

	mk := func(key, scope, content string) {
		t.Helper()
		if _, err := store.UpsertMemory(ctx, kb.MemoryUpsertOptions{
			FilePath: key, Scope: scope, Content: content, Tags: []string{"memory"},
		}); err != nil {
			t.Fatalf("UpsertMemory(%s): %v", key, err)
		}
		// The keyless path embeds the document but only records the scope in the
		// mirrored front matter; set the column the pinned-scope query reads.
		if _, err := conn.Exec(`UPDATE kb_documents SET memory_scope = ? WHERE file_path = ?`, scope, key); err != nil {
			t.Fatalf("set scope(%s): %v", key, err)
		}
	}
	mk("memory/project-vcs.md", "project/", "relevance memory: use jj instead of git stash")
	mk("memory/project-noise.md", "project/", "relevance memory: favourite pasta recipe")
	mk("memory/rules-pinned.md", "rules/", "relevance memory: always answer in English")

	cfg := config.RemembrancesConfig{
		MemoryEnabled:                  true,
		MemoryContextEnrichmentEnabled: true,
		MemoryContextMaxItems:          10,
		MemoryContextMaxChars:          0,
		MemoryPinnedScopes:             []string{"rules/"},
		MemoryDefaultTTLDays:           30,
	}

	// Filter off: byte-identical to the legacy entry point, nothing reported.
	legacy := BuildMemoryBlock(ctx, store, "relevance memory", cfg)
	offBlock, offRes := BuildMemoryBlockWithResult(ctx, store, "relevance memory", cfg, nil)
	if legacy != offBlock || offRes.Applied {
		t.Fatalf("nil filter must match BuildMemoryBlock: res=%+v", offRes)
	}
	for _, key := range []string{"jj instead", "pasta", "English"} {
		if !strings.Contains(offBlock, key) {
			t.Fatalf("baseline missing %s:\n%s", key, offBlock)
		}
	}

	waitHits := func(key string, want int) int {
		var got int
		for i := 0; i < 50; i++ {
			if got = memHits(t, conn, key); got == want {
				return got
			}
			time.Sleep(20 * time.Millisecond)
		}
		return got
	}
	// Two baseline builds (legacy + nil-filter) injected every memory twice.
	for _, key := range []string{"memory/project-vcs.md", "memory/project-noise.md", "memory/rules-pinned.md"} {
		if got := waitHits(key, 2); got != 2 {
			t.Fatalf("%s hits after baseline = %d, want 2", key, got)
		}
	}

	// Filter on: asks to drop the noise AND the pinned memory; pinned must stay.
	f := &recordingFilter{drop: map[string]bool{"memory/project-noise.md": true, "memory/rules-pinned.md": true}}
	block, res := BuildMemoryBlockWithResult(ctx, store, "relevance memory", cfg, f)
	if strings.Contains(block, "pasta") {
		t.Fatalf("dropped memory injected:\n%s", block)
	}
	if !strings.Contains(block, "jj instead") || !strings.Contains(block, "English") {
		t.Fatalf("kept or pinned memory missing:\n%s", block)
	}
	if !res.Applied || res.Dropped != 1 || res.Kept != 2 {
		t.Fatalf("result = %+v", res)
	}
	pinnedSeen := false
	for _, c := range f.seen {
		if c.ID == "memory/rules-pinned.md" {
			pinnedSeen = c.Pinned
		}
	}
	if !pinnedSeen {
		t.Fatalf("pinned memory must reach the filter flagged Pinned: %+v", f.seen)
	}

	// Hit counters move only for injected memories.
	if got := waitHits("memory/project-vcs.md", 3); got != 3 {
		t.Fatalf("injected memory hits = %d, want 3", got)
	}
	if got := waitHits("memory/rules-pinned.md", 3); got != 3 {
		t.Fatalf("pinned memory hits = %d, want 3", got)
	}
	time.Sleep(150 * time.Millisecond)
	if got := memHits(t, conn, "memory/project-noise.md"); got != 2 {
		t.Fatalf("dropped memory hits = %d, want 2 (unchanged)", got)
	}

	// Dropping everything droppable and keeping nothing yields an empty block for non-pinned only.
	f2 := &recordingFilter{drop: map[string]bool{"memory/project-vcs.md": true, "memory/project-noise.md": true}}
	block, _ = BuildMemoryBlockWithResult(ctx, store, "relevance memory", cfg, f2)
	if strings.Contains(block, "jj instead") || !strings.Contains(block, "English") {
		t.Fatalf("unexpected block:\n%s", block)
	}
}
