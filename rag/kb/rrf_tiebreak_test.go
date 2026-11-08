package kb

import "testing"

// Two documents ranked 1+2 and 2+1 across the vector and FTS lists get the
// same RRF score; their order (and the top-limit cut) must not depend on map
// iteration order.
func TestRRFFuseBreaksTiesDeterministically(t *testing.T) {
	a := SearchResult{Document: Document{ID: 7, FilePath: "a.md"}, ChunkContent: "a"}
	b := SearchResult{Document: Document{ID: 3, FilePath: "b.md"}, ChunkContent: "b"}
	vec := []SearchResult{a, b}
	fts := []SearchResult{b, a}

	for i := 0; i < 200; i++ {
		got := rrfFuse(vec, fts, 1)
		if len(got) != 1 || got[0].Document.ID != 3 {
			t.Fatalf("run %d: top result = %+v, want document 3 (lower id wins a tie)", i, got)
		}
		all := rrfFuse(vec, fts, 10)
		if len(all) != 2 || all[0].Document.ID != 3 || all[1].Document.ID != 7 {
			t.Fatalf("run %d: order = %d,%d, want 3,7", i, all[0].Document.ID, all[1].Document.ID)
		}
	}
}
