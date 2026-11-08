package kb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

func TestApplyChangesIdempotentByWatermark(t *testing.T) {
	ctx := context.Background()
	emb := &apiEmbedder{}
	s, _ := openAPIStore(t, Options{Embedder: emb})
	fsys := fstest.MapFS{
		"kb/a.md":   {Data: []byte("# A\n\nalpha content")},
		"kb/b.md":   {Data: []byte("# B\n\nbeta content [[a]]")},
		"img/x.png": {Data: []byte{0x89}},
	}
	changes := []Change{
		{Op: OpAdd, Path: "kb/a.md", Blob: "ba1"},
		{Op: OpAdd, Path: "kb/b.md", Blob: "bb1"},
		{Op: OpAdd, Path: "img/x.png", Blob: "bx1"},
	}
	st, err := s.ApplyChanges(ctx, fsys, changes, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Added != 2 || st.Scanned != 3 || st.LinksIndexed != 1 {
		t.Fatalf("first apply = %+v", st)
	}
	if w, _ := s.Watermark(ctx); w != "c1" {
		t.Fatalf("watermark = %q", w)
	}
	embedded := emb.embedded.Load()

	st, err = s.ApplyChanges(ctx, fsys, changes, "c1")
	if err != nil || st != (SyncStats{}) || emb.embedded.Load() != embedded {
		t.Fatalf("re-apply = %+v, %v, embedded %d more", st, err, emb.embedded.Load()-embedded)
	}
	// Same blobs under a new watermark: nothing is read or embedded either.
	st, err = s.ApplyChanges(ctx, nil, changes[:2], "c2")
	if err != nil || st.Unchanged != 2 || emb.embedded.Load() != embedded {
		t.Fatalf("same blobs = %+v, %v", st, err)
	}
	if w, _ := s.Watermark(ctx); w != "c2" {
		t.Fatalf("watermark = %q", w)
	}
}

func TestApplyChangesRenameWithoutReembed(t *testing.T) {
	ctx := context.Background()
	emb := &apiEmbedder{}
	s, db := openAPIStore(t, Options{Embedder: emb})
	if _, err := s.ApplyChanges(ctx, nil, []Change{{Op: OpAdd, Path: "old/a.md", Blob: "b1", Content: []byte("rotation policy text [[x]]")}}, "c1"); err != nil {
		t.Fatal(err)
	}
	var chunkBefore int64
	_ = db.QueryRow(`SELECT id FROM kb_chunks`).Scan(&chunkBefore)
	embedded := emb.embedded.Load()

	st, err := s.ApplyChanges(ctx, nil, []Change{{Op: OpRename, OldPath: "old/a.md", Path: "new/a.md", Blob: "b1"}}, "c2")
	if err != nil || st.Renamed != 1 || emb.embedded.Load() != embedded {
		t.Fatalf("rename = %+v, %v, embedded %d more", st, err, emb.embedded.Load()-embedded)
	}
	if _, err := s.Get(ctx, "old/a.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old path still there: %v", err)
	}
	var chunkAfter int64
	var linkPath string
	_ = db.QueryRow(`SELECT id FROM kb_chunks`).Scan(&chunkAfter)
	_ = db.QueryRow(`SELECT source_path FROM kb_links`).Scan(&linkPath)
	if chunkAfter != chunkBefore || linkPath != "new/a.md" {
		t.Fatalf("chunk %d -> %d, link source %q", chunkBefore, chunkAfter, linkPath)
	}
	hits, _, _ := s.Search(ctx, "rotation", Query{})
	if len(hits) != 1 || hits[0].Path != "new/a.md" || hits[0].GitBlob != "b1" {
		t.Fatalf("hits = %+v", hits)
	}

	// Rename with a new blob: moved and re-embedded.
	st, err = s.ApplyChanges(ctx, nil, []Change{{Op: OpRename, OldPath: "new/a.md", Path: "b.md", Blob: "b2", Content: []byte("rotation policy v2")}}, "c3")
	if err != nil || st.Renamed != 1 || st.Updated != 1 || emb.embedded.Load() == embedded {
		t.Fatalf("rename+modify = %+v, %v", st, err)
	}
	if d, err := s.Get(ctx, "b.md"); err != nil || d.GitBlob != "b2" {
		t.Fatalf("Get b.md = %+v, %v", d, err)
	}
}

func TestApplyChangesModifyDeleteAndAtomicity(t *testing.T) {
	ctx := context.Background()
	emb := &apiEmbedder{}
	s, _ := openAPIStore(t, Options{Embedder: emb})
	if _, err := s.ApplyChanges(ctx, nil, []Change{
		{Op: OpAdd, Path: "a.md", Blob: "a1", Content: []byte("one")},
		{Op: OpAdd, Path: "b.md", Blob: "b1", Content: []byte("two")},
	}, "c1"); err != nil {
		t.Fatal(err)
	}

	// A batch with an unreadable change stores nothing, watermark included.
	_, err := s.ApplyChanges(ctx, fstest.MapFS{}, []Change{
		{Op: OpDelete, Path: "a.md"},
		{Op: OpModify, Path: "b.md", Blob: "b2"}, // not in fsys
	}, "c2")
	if err == nil {
		t.Fatal("missing content must fail the batch")
	}
	if w, _ := s.Watermark(ctx); w != "c1" {
		t.Fatalf("watermark moved to %q on a failed batch", w)
	}
	if _, err := s.Get(ctx, "a.md"); err != nil {
		t.Fatalf("a.md deleted by a failed batch: %v", err)
	}

	st, err := s.ApplyChanges(ctx, nil, []Change{
		{Op: OpDelete, Path: "a.md"},
		{Op: OpModify, Path: "b.md", Blob: "b2", Content: []byte("two, edited")},
		{Op: OpModify, Path: "b.md", Blob: "b2", Content: []byte("two, edited")}, // repeated in the batch
	}, "c2")
	if err != nil || st.Deleted != 1 || st.Updated != 1 || st.Unchanged != 1 {
		t.Fatalf("apply = %+v, %v", st, err)
	}
	if d, _ := s.Get(ctx, "b.md"); d.Content != "two, edited" {
		t.Fatalf("b.md = %q", d.Content)
	}
}

func TestSyncTree(t *testing.T) {
	ctx := context.Background()
	emb := &apiEmbedder{}
	s, _ := openAPIStore(t, Options{Embedder: emb})
	fsys := fstest.MapFS{
		"docs/a.md":         {Data: []byte("alpha")},
		"docs/sub/b.md":     {Data: []byte("beta")},
		"docs/.hidden/c.md": {Data: []byte("hidden")},
		"docs/drafts/d.md":  {Data: []byte("draft")},
		"docs/e.txt":        {Data: []byte("text")},
		"other/f.md":        {Data: []byte("outside")},
	}
	opts := SyncOptions{Prefix: "docs", Exclude: []string{"drafts"}, DeleteMissing: true}
	st, err := s.SyncTree(ctx, fsys, opts)
	if err != nil || st.Added != 2 || st.Scanned != 2 {
		t.Fatalf("first sync = %+v, %v", st, err)
	}
	embedded := emb.embedded.Load()
	st, err = s.SyncTree(ctx, fsys, opts)
	if err != nil || st.Unchanged != 2 || emb.embedded.Load() != embedded {
		t.Fatalf("second sync = %+v, %v", st, err)
	}

	// A document outside the prefix survives DeleteMissing.
	mustPut(t, s, Doc{Path: "other/f.md", Content: "outside"})
	delete(fsys, "docs/sub/b.md")
	st, err = s.SyncTree(ctx, fsys, opts)
	if err != nil || st.Deleted != 1 {
		t.Fatalf("third sync = %+v, %v", st, err)
	}
	if _, err := s.Get(ctx, "other/f.md"); err != nil {
		t.Fatalf("other/f.md removed: %v", err)
	}

	st, err = s.SyncTree(ctx, fsys, SyncOptions{Prefix: "docs", Include: []string{"*.txt"}})
	if err != nil || st.Added != 1 {
		t.Fatalf("include sync = %+v, %v", st, err)
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.SyncTree(cctx, fsys, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled sync = %v", err)
	}
}

// gintrackMemory is the memory file of gintrack's ADR-044 (front matter
// verbatim, including the map-valued "source" that Pando's typed FrontMatter
// cannot decode).
const gintrackMemory = `---
id: ACME-MEM-0042
type: memory
title: "Customer X is invoiced quarterly"
key: customer-x-invoicing
status: active
author: ana
created: 2026-11-08
updated: 2026-11-08
version: 17
content_hash: 9f2c
local_id: project/acme/customer-x-invoicing
valid_from: 2026-11-08T10:00:00Z
labels: [billing]
source: {kind: agent, ref: "memsync/inst-1"}
links:
  - {kind: supersedes, target: ACME-MEM-0017}
---
Customer X is invoiced at the end of each quarter, never monthly.

## Evidence

Contract signed 2026-10-01.
`

// Hub memories are plain files: their kind and status come from the front
// matter, so Search finds them without going through UpsertMemory.
func TestApplyChangesIndexesGintrackMemories(t *testing.T) {
	ctx := context.Background()
	s, _ := openAPIStore(t, Options{Embedder: &apiEmbedder{}})
	proposed := strings.Replace(strings.Replace(gintrackMemory, "status: active", "status: proposed", 1), "quarter", "quarter (draft)", 1)
	if _, err := s.ApplyChanges(ctx, nil, []Change{
		{Op: OpAdd, Path: "memory/ACME-MEM-0042.md", Blob: "m1", Content: []byte(gintrackMemory)},
		{Op: OpAdd, Path: "memory/ACME-MEM-0043.md", Blob: "m2", Content: []byte(proposed)},
		{Op: OpAdd, Path: "kb/invoicing.md", Blob: "k1", Content: []byte("# Invoicing\n\nCustomer X invoicing overview.")},
	}, "c1"); err != nil {
		t.Fatal(err)
	}

	d, err := s.Get(ctx, "memory/ACME-MEM-0042.md")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != KindMemory || d.Status != StatusActive || d.Author != "ana" || d.Meta["key"] != "customer-x-invoicing" ||
		len(d.Tags) != 1 || d.Tags[0] != "billing" {
		t.Fatalf("memory doc = %+v", d)
	}

	for _, mode := range []SearchMode{ModeHybrid, ModeFTS, ModeVector} {
		hits, _, err := s.Search(ctx, "customer invoiced quarter", Query{Kinds: []string{KindMemory}, Statuses: []string{StatusActive}, Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 || hits[0].Path != "memory/ACME-MEM-0042.md" || hits[0].Kind != KindMemory {
			t.Fatalf("mode %d: hits = %v", mode, paths(hits))
		}
		if strings.Contains(hits[0].Chunk, "local_id") || hits[0].StartLine != 19 {
			t.Fatalf("mode %d: chunk %d-%d %q", mode, hits[0].StartLine, hits[0].EndLine, hits[0].Chunk)
		}
	}
	// The title is searchable on its own (front-matter fields index).
	hits, _, _ := s.Search(ctx, "invoiced", Query{Kinds: []string{KindMemory}, Mode: ModeFTS})
	if len(hits) != 1 {
		t.Fatalf("title search = %v", paths(hits))
	}
	hits, _, _ = s.Search(ctx, "customer", Query{Kinds: []string{KindMemory}, Statuses: []string{"proposed"}})
	if len(hits) != 1 || hits[0].Path != "memory/ACME-MEM-0043.md" {
		t.Fatalf("proposed = %v", paths(hits))
	}
}
