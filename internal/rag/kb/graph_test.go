package kb

import (
	"context"
	"math"
	"reflect"
	"testing"
)

// addDoc stores a document through the normal write path, so its links are
// indexed exactly as they would be in production.
func addDoc(t *testing.T, store *KBStore, filePath, content string, meta map[string]interface{}) {
	t.Helper()
	if err := store.AddDocument(context.Background(), filePath, content, meta); err != nil {
		t.Fatalf("AddDocument(%q) error = %v", filePath, err)
	}
}

func relatedPaths(rels []RelatedDocument) []string {
	out := make([]string, len(rels))
	for i, r := range rels {
		out[i] = r.FilePath
	}
	return out
}

func TestOutgoingLinksResolvesTargets(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	addDoc(t, store, "pando/plans/alpha.md",
		"Alpha builds on [[pando/features/beta.md]], on [[gamma]] and on [[nobody-wrote-this]].", nil)
	addDoc(t, store, "pando/features/beta.md", "Beta.", nil)
	addDoc(t, store, "pando/features/gamma.md", "Gamma.", nil)

	targets, err := store.OutgoingLinks(ctx, "pando/plans/alpha.md")
	if err != nil {
		t.Fatalf("OutgoingLinks() error = %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("len(targets) = %d, want 3", len(targets))
	}

	// Full path resolves.
	if !targets[0].Resolved || targets[0].FilePath != "pando/features/beta.md" {
		t.Errorf("target[0] = %+v, want resolved to pando/features/beta.md", targets[0])
	}
	// Bare basename resolves too.
	if !targets[1].Resolved || targets[1].FilePath != "pando/features/gamma.md" {
		t.Errorf("target[1] = %+v, want resolved to pando/features/gamma.md", targets[1])
	}
	// An undefined concept stays unresolved: that is a feature, not an error.
	if targets[2].Resolved || targets[2].Slug != "nobody-wrote-this" {
		t.Errorf("target[2] = %+v, want unresolved nobody-wrote-this", targets[2])
	}
}

func TestOutgoingLinksResolvesAliases(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	addDoc(t, store, "pando/plans/alpha.md", "See [[Code Property Graph]].", nil)
	addDoc(t, store, "pando/features/code_graph.md", "The graph.", map[string]interface{}{
		"aliases": []string{"Code Property Graph", "CPG"},
	})

	targets, err := store.OutgoingLinks(ctx, "pando/plans/alpha.md")
	if err != nil {
		t.Fatalf("OutgoingLinks() error = %v", err)
	}
	if len(targets) != 1 || !targets[0].Resolved || targets[0].FilePath != "pando/features/code_graph.md" {
		t.Fatalf("targets = %+v, want the alias resolved to pando/features/code_graph.md", targets)
	}
}

// TestResolutionPrecedence pins the path > basename order: when two documents
// share a basename, the bare form belongs to exactly one of them (the first in
// path order), and the other is only reachable by its full path.
func TestResolutionPrecedence(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	addDoc(t, store, "a/notes.md", "First notes.", nil)
	addDoc(t, store, "b/notes.md", "Second notes.", nil)
	addDoc(t, store, "index.md", "Pointing at [[notes]] and at [[b/notes.md]].", nil)

	targets, err := store.OutgoingLinks(ctx, "index.md")
	if err != nil {
		t.Fatalf("OutgoingLinks() error = %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(targets))
	}
	if targets[0].FilePath != "a/notes.md" {
		t.Errorf("[[notes]] resolved to %q, want a/notes.md", targets[0].FilePath)
	}
	if targets[1].FilePath != "b/notes.md" {
		t.Errorf("[[b/notes.md]] resolved to %q, want b/notes.md", targets[1].FilePath)
	}

	// The ambiguous bare slug must not become a backlink of the document that
	// did not win it.
	backs, err := store.Backlinks(ctx, "b/notes.md")
	if err != nil {
		t.Fatalf("Backlinks() error = %v", err)
	}
	if len(backs) != 1 || backs[0].Slug != "b/notes" {
		t.Fatalf("Backlinks(b/notes.md) = %+v, want only the full-path link", backs)
	}
}

func TestBacklinks(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	addDoc(t, store, "pando/features/beta.md", "Beta.", map[string]interface{}{
		"aliases": []string{"The Beta Feature"},
	})
	addDoc(t, store, "pando/plans/alpha.md", "Uses [[beta]].", nil)
	addDoc(t, store, "pando/changes/delta.md", "Ships [[The Beta Feature|beta at last]].", nil)
	addDoc(t, store, "pando/plans/unrelated.md", "Nothing here.", nil)

	backs, err := store.Backlinks(ctx, "pando/features/beta.md")
	if err != nil {
		t.Fatalf("Backlinks() error = %v", err)
	}
	if len(backs) != 2 {
		t.Fatalf("len(backlinks) = %d, want 2: %+v", len(backs), backs)
	}
	// Ordered by source path: changes/ before plans/.
	if backs[0].FilePath != "pando/changes/delta.md" || backs[0].Label != "beta at last" {
		t.Errorf("backlink[0] = %+v, want the labelled alias link from delta", backs[0])
	}
	if backs[1].FilePath != "pando/plans/alpha.md" {
		t.Errorf("backlink[1] = %+v, want the link from alpha", backs[1])
	}
}

func TestRelatedDocumentsRanksByConnection(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	tags := map[string]interface{}{"tags": []string{"kb", "wiki"}}
	addDoc(t, store, "hub.md", "Hub points at [[target]].", tags)
	addDoc(t, store, "target.md", "The target.", nil)
	addDoc(t, store, "citer.md", "Citer points at [[hub]].", nil)
	addDoc(t, store, "topical.md", "Same subject, no links.", tags)
	addDoc(t, store, "stranger.md", "Unconnected.", nil)

	rels, err := store.RelatedDocuments(ctx, "hub.md", 0)
	if err != nil {
		t.Fatalf("RelatedDocuments() error = %v", err)
	}

	want := []string{"target.md", "citer.md", "topical.md"}
	if got := relatedPaths(rels); !reflect.DeepEqual(got, want) {
		t.Fatalf("related = %v, want %v (outgoing > backlink > shared tags, stranger excluded)", got, want)
	}
	if rels[0].Score != weightOutgoing || rels[1].Score != weightBacklink || rels[2].Score != weightSharedTags {
		t.Errorf("scores = %v/%v/%v, want %v/%v/%v",
			rels[0].Score, rels[1].Score, rels[2].Score,
			weightOutgoing, weightBacklink, weightSharedTags)
	}
}

// TestRelatedDocumentsAccumulatesSignals covers a document that is connected in
// several ways at once: the weights add up, and each connection is counted once
// even when two different slugs point at the same document.
func TestRelatedDocumentsAccumulatesSignals(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	tags := map[string]interface{}{"tags": []string{"kb"}}
	addDoc(t, store, "a.md", "A points at [[b]] and again at [[b.md]].", tags)
	addDoc(t, store, "b.md", "B points back at [[a]].", tags)

	rels, err := store.RelatedDocuments(ctx, "a.md", 0)
	if err != nil {
		t.Fatalf("RelatedDocuments() error = %v", err)
	}
	if len(rels) != 1 {
		t.Fatalf("len(related) = %d, want 1: %+v", len(rels), rels)
	}
	want := weightOutgoing + weightBacklink + weightSharedTags
	if math.Abs(rels[0].Score-want) > 1e-9 {
		t.Errorf("score = %v, want %v (each signal counted once)", rels[0].Score, want)
	}
	if len(rels[0].Reasons) != 3 {
		t.Errorf("reasons = %v, want one per signal", rels[0].Reasons)
	}
}

func TestWantedConcepts(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	addDoc(t, store, "a.md", "Needs [[Token Ledger]] and [[graph view]].", nil)
	addDoc(t, store, "b.md", "Also needs [[token-ledger]], plus the existing [[c]].", nil)
	addDoc(t, store, "c.md", "Exists.", nil)

	wanted, err := store.WantedConcepts(ctx, 0)
	if err != nil {
		t.Fatalf("WantedConcepts() error = %v", err)
	}
	if len(wanted) != 2 {
		t.Fatalf("len(wanted) = %d, want 2 (c.md exists): %+v", len(wanted), wanted)
	}

	// Most wanted first, and the raw spelling of the first author is kept because
	// it reads better than the slug when suggesting a title.
	if wanted[0].Slug != "token-ledger" || wanted[0].Count != 2 || wanted[0].Raw != "Token Ledger" {
		t.Errorf("wanted[0] = %+v, want token-ledger x2 raw %q", wanted[0], "Token Ledger")
	}
	if !reflect.DeepEqual(wanted[0].Sources, []string{"a.md", "b.md"}) {
		t.Errorf("wanted[0].Sources = %v, want [a.md b.md]", wanted[0].Sources)
	}
	if wanted[1].Slug != "graph-view" || wanted[1].Count != 1 {
		t.Errorf("wanted[1] = %+v, want graph-view x1", wanted[1])
	}
}

// TestGraphOnLinklessKB is the backward-compatibility guarantee: a knowledge base
// written by an older Pando, with no wiki links anywhere, answers every graph
// query with an empty result and never an error.
func TestGraphOnLinklessKB(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	addDoc(t, store, "legacy/one.md", "Plain prose.", nil)
	addDoc(t, store, "legacy/two.md", "More prose.", nil)

	targets, err := store.OutgoingLinks(ctx, "legacy/one.md")
	if err != nil || len(targets) != 0 {
		t.Errorf("OutgoingLinks() = %v, %v; want empty, nil", targets, err)
	}
	backs, err := store.Backlinks(ctx, "legacy/one.md")
	if err != nil || len(backs) != 0 {
		t.Errorf("Backlinks() = %v, %v; want empty, nil", backs, err)
	}
	rels, err := store.RelatedDocuments(ctx, "legacy/one.md", 0)
	if err != nil || len(rels) != 0 {
		t.Errorf("RelatedDocuments() = %v, %v; want empty, nil", rels, err)
	}
	wanted, err := store.WantedConcepts(ctx, 0)
	if err != nil || len(wanted) != 0 {
		t.Errorf("WantedConcepts() = %v, %v; want empty, nil", wanted, err)
	}

	// An unknown document is not an error either.
	rels, err = store.RelatedDocuments(ctx, "does/not/exist.md", 0)
	if err != nil || len(rels) != 0 {
		t.Errorf("RelatedDocuments(missing) = %v, %v; want empty, nil", rels, err)
	}
}
