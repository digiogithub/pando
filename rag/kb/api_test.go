package kb

import (
	"context"
	"errors"
	"testing"
)

const specDoc = `---
title: Login
labels: [auth]
status: proposed
author: ana
---
# Login

### AUTH.R1 Throttle

The system shall throttle failed logins.
`

func TestPutGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	emb := &apiEmbedder{model: "fake-1"}
	s, db := openAPIStore(t, Options{Embedder: emb})

	res, err := s.Put(ctx, Doc{Path: "specs/login.md", Content: specDoc, GitBlob: "b1", Meta: map[string]interface{}{"project": "acme"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || !res.Embedded {
		t.Fatalf("first Put = %+v", res)
	}
	d, err := s.Get(ctx, "specs/login.md")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != KindDocument || d.Status != "proposed" || d.Author != "ana" || d.GitBlob != "b1" ||
		d.Rev != ContentRev(specDoc) || d.Meta["project"] != "acme" || d.Meta["title"] != "Login" ||
		len(d.Tags) != 1 || d.Tags[0] != "auth" || d.Content != specDoc {
		t.Fatalf("Get = %+v", d)
	}

	var start, end int
	var model string
	if err := db.QueryRow(`SELECT start_line, end_line, embedding_model FROM kb_chunks`).Scan(&start, &end, &model); err != nil {
		t.Fatal(err)
	}
	if start != 7 || end != 11 || model != "fake-1" {
		t.Fatalf("chunk lines %d-%d model %q", start, end, model)
	}

	// Same content: fields change, nothing is re-embedded.
	before := emb.embedded.Load()
	res, err = s.Put(ctx, Doc{Path: "specs/login.md", Content: specDoc, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || res.Embedded || emb.embedded.Load() != before {
		t.Fatalf("unchanged Put = %+v, embedded %d more", res, emb.embedded.Load()-before)
	}
	if d, _ := s.Get(ctx, "specs/login.md"); d.Status != "active" {
		t.Fatalf("status not updated: %q", d.Status)
	}

	// New content: re-embedded, chunks replaced.
	res, err = s.Put(ctx, Doc{Path: "specs/login.md", Content: specDoc + "\nMore text.\n"})
	if err != nil || !res.Embedded {
		t.Fatalf("changed Put = %+v, %v", res, err)
	}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM kb_chunks`).Scan(&n)
	if n != 1 {
		t.Fatalf("chunks after update = %d, want 1", n)
	}
}

func TestPutReembedsWhenModelChanges(t *testing.T) {
	ctx := context.Background()
	s, db := openAPIStore(t, Options{Embedder: &apiEmbedder{model: "old"}})
	if _, err := s.Put(ctx, Doc{Path: "a.md", Content: "alpha"}); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(db, Options{Embedder: &apiEmbedder{model: "new"}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s2.Put(ctx, Doc{Path: "a.md", Content: "alpha"})
	if err != nil || !res.Embedded {
		t.Fatalf("Put after model change = %+v, %v", res, err)
	}
}

func TestDeleteAndDeletePrefix(t *testing.T) {
	ctx := context.Background()
	s, db := openAPIStore(t, Options{Embedder: &apiEmbedder{}})
	for _, p := range []string{"kb/a.md", "kb/b.md", "kb_x/c.md", "other.md"} {
		if _, err := s.Put(ctx, Doc{Path: p, Content: "title: x\n\nbody of " + p}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete(ctx, "other.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "other.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get deleted = %v", err)
	}
	if err := s.Delete(ctx, "other.md"); err != nil {
		t.Fatalf("second Delete = %v", err)
	}
	// "kb/" must not match "kb_x/" ('_' is a LIKE wildcard unless escaped).
	n, err := s.DeletePrefix(ctx, "kb/")
	if err != nil || n != 2 {
		t.Fatalf("DeletePrefix = %d, %v", n, err)
	}
	var docs, chunks, fts int
	_ = db.QueryRow(`SELECT count(*) FROM kb_documents`).Scan(&docs)
	_ = db.QueryRow(`SELECT count(*) FROM kb_chunks`).Scan(&chunks)
	_ = db.QueryRow(`SELECT count(*) FROM kb_fts WHERE kb_fts MATCH 'body'`).Scan(&fts)
	if docs != 1 || chunks != 1 || fts != 1 {
		t.Fatalf("after DeletePrefix docs=%d chunks=%d fts=%d", docs, chunks, fts)
	}
	if _, err := s.DeletePrefix(ctx, ""); err == nil {
		t.Fatal("empty prefix must be refused")
	}
}

func TestPutWithoutEmbedder(t *testing.T) {
	ctx := context.Background()
	s, db := openAPIStore(t, Options{})
	if _, err := s.Put(ctx, Doc{Path: "a.md", Content: "plain text"}); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM kb_chunks WHERE embedding IS NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("chunks without embedding = %d", n)
	}
}
