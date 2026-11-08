package kb

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func paths(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Path
	}
	return out
}

func mustPut(t *testing.T, s *KBStore, d Doc) {
	t.Helper()
	if _, err := s.Put(context.Background(), d); err != nil {
		t.Fatalf("Put %s: %v", d.Path, err)
	}
}

// Filters must cut candidates before ranking: twenty strong matches outside
// the filter must not push the two weak matches inside it out of the result.
func TestSearchFiltersDoNotUnderReturn(t *testing.T) {
	ctx := context.Background()
	s, _ := openAPIStore(t, Options{Embedder: &apiEmbedder{}})
	for i := 0; i < 20; i++ {
		mustPut(t, s, Doc{Path: fmt.Sprintf("noise/%02d.md", i), Content: "widget widget widget gadget", Scope: "project/other"})
	}
	mustPut(t, s, Doc{Path: "acme/a.md", Content: "a widget mention among other words", Scope: "project/acme", Author: "ana", Tags: []string{"Auth"}})
	mustPut(t, s, Doc{Path: "acme/b.md", Content: "one more widget mention here", Scope: "project/acme/sub", Kind: "spec"})
	mustPut(t, s, Doc{Path: "acme2/c.md", Content: "widget", Scope: "project/acme2"})

	for name, tc := range map[string]struct {
		q    Query
		want string
	}{
		"scope":          {Query{Scopes: []string{"project/acme"}}, "acme/a.md,acme/b.md"},
		"path prefixes":  {Query{PathPrefixes: []string{"acme/", "acme2/"}}, "acme/a.md,acme/b.md,acme2/c.md"},
		"exclude":        {Query{ExcludePrefixes: []string{"noise/", "acme2/"}}, "acme/a.md,acme/b.md"},
		"kind":           {Query{Kinds: []string{"spec"}}, "acme/b.md"},
		"author":         {Query{Authors: []string{"ana"}}, "acme/a.md"},
		"tag (any case)": {Query{Tags: []string{"auth"}}, "acme/a.md"},
	} {
		for _, mode := range []SearchMode{ModeHybrid, ModeFTS, ModeVector} {
			tc.q.Limit, tc.q.Mode = 3, mode
			hits, _, err := s.Search(ctx, "widget", tc.q)
			if err != nil {
				t.Fatalf("%s/%d: %v", name, mode, err)
			}
			got := paths(hits)
			slices.Sort(got)
			if strings.Join(got, ",") != tc.want {
				t.Errorf("%s/mode %d: got %v, want %s", name, mode, got, tc.want)
			}
		}
	}
}

func TestSearchStatusDefaultsToActive(t *testing.T) {
	ctx := context.Background()
	s, _ := openAPIStore(t, Options{Embedder: &apiEmbedder{}})
	mustPut(t, s, Doc{Path: "live.md", Content: "rotation policy"})
	mustPut(t, s, Doc{Path: "draft.md", Content: "---\nstatus: proposed\n---\nrotation policy draft"})

	hits, _, _ := s.Search(ctx, "rotation", Query{})
	if strings.Join(paths(hits), ",") != "live.md" {
		t.Fatalf("default statuses: %v", paths(hits))
	}
	hits, _, _ = s.Search(ctx, "rotation", Query{Statuses: []string{"proposed"}})
	if strings.Join(paths(hits), ",") != "draft.md" {
		t.Fatalf("proposed: %v", paths(hits))
	}
	hits, _, _ = s.Search(ctx, "rotation", Query{Statuses: []string{AnyStatus}})
	if len(hits) != 2 {
		t.Fatalf("any status: %v", paths(hits))
	}
}

func TestSearchHitCarriesProvenance(t *testing.T) {
	ctx := context.Background()
	s, _ := openAPIStore(t, Options{Embedder: &apiEmbedder{}})
	mustPut(t, s, Doc{Path: "specs/login.md", Content: specDoc, GitBlob: "blob1", Status: "active", Meta: map[string]interface{}{"project": "acme"}})

	hits, stats, err := s.Search(ctx, "throttle failed logins", Query{})
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	h := hits[0]
	if h.StartLine != 7 || h.EndLine != 11 || h.Rev != ContentRev(specDoc) || h.GitBlob != "blob1" ||
		h.Author != "ana" || h.Kind != KindDocument || h.Meta["project"] != "acme" || h.Rank != 1 || h.Score <= 0 {
		t.Fatalf("hit = %+v", h)
	}
	if strings.Contains(h.Chunk, "labels:") {
		t.Fatalf("front matter leaked into the chunk: %q", h.Chunk)
	}
	if stats.VectorCandidates == 0 || stats.FTSCandidates == 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestSearchMatchesFrontMatterFields(t *testing.T) {
	ctx := context.Background()
	s, _ := openAPIStore(t, Options{})
	mustPut(t, s, Doc{Path: "a.md", Content: "---\ntitle: Kerberos handshake\naliases: [krb5]\n---\nBody without the words."})
	mustPut(t, s, Doc{Path: "b.md", Content: "Unrelated body."})

	for _, q := range []string{"kerberos", "krb5"} {
		hits, _, err := s.Search(ctx, q, Query{})
		if err != nil || strings.Join(paths(hits), ",") != "a.md" {
			t.Fatalf("%q: %v, %v", q, paths(hits), err)
		}
	}
	if _, _, err := s.Search(ctx, "x", Query{Mode: ModeVector}); err == nil {
		t.Fatal("vector mode without an embedder must fail")
	}
}

func TestSearchSkipsOtherModelsChunks(t *testing.T) {
	ctx := context.Background()
	s, db := openAPIStore(t, Options{Embedder: &apiEmbedder{model: "old"}})
	mustPut(t, s, Doc{Path: "a.md", Content: "rotation"})
	s2, _ := Open(db, Options{Embedder: &apiEmbedder{model: "new"}})
	hits, stats, err := s2.Search(ctx, "rotation", Query{Mode: ModeVector})
	if err != nil || len(hits) != 0 || stats.SkippedForModelMismatch != 1 {
		t.Fatalf("hits=%v stats=%+v err=%v", hits, stats, err)
	}
}

func TestFuseAcrossProjects(t *testing.T) {
	a := []Hit{{Project: "p1", Path: "x.md", StartLine: 1}, {Project: "p1", Path: "y.md", StartLine: 1}}
	b := []Hit{{Project: "p2", Path: "x.md", StartLine: 1}, {Project: "p2", Path: "z.md", StartLine: 1}}
	got := Fuse(3, a, b)
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	// Rank-1 hits of both projects tie; same path in different projects
	// stays distinct; ties order by project.
	if got[0].Project != "p1" || got[0].Path != "x.md" || got[1].Project != "p2" || got[1].Path != "x.md" || got[2].Rank != 3 {
		t.Fatalf("fused = %+v", got)
	}
	// The same hit in two lists is boosted above single-list hits.
	got = Fuse(0, []Hit{{Path: "q.md"}, {Path: "r.md"}}, []Hit{{Path: "s.md"}, {Path: "r.md"}})
	if got[0].Path != "r.md" || len(got) != 3 {
		t.Fatalf("boost = %+v", got)
	}
}
