package tools

import "testing"

func TestRecordReadNewUnchangedChanged(t *testing.T) {
	c := NewSessionCache("sess-dedup")

	// First read of a window → new, gets label F1.
	r1 := c.RecordRead("/abs/foo.go", 0, 10, "alpha\nbeta", "full")
	if r1.Status != ReadDedupNew {
		t.Fatalf("first read status=%q want new", r1.Status)
	}
	if r1.Label != "F1" {
		t.Fatalf("first label=%q want F1", r1.Label)
	}

	// Identical re-read → unchanged, same label, dedup hit recorded.
	r2 := c.RecordRead("/abs/foo.go", 0, 10, "alpha\nbeta", "full")
	if r2.Status != ReadDedupUnchanged {
		t.Fatalf("re-read status=%q want unchanged", r2.Status)
	}
	if r2.Label != "F1" {
		t.Fatalf("re-read label=%q want F1", r2.Label)
	}
	if got := c.Stats().DedupHits; got != 1 {
		t.Fatalf("DedupHits=%d want 1", got)
	}

	// Changed content for the same window → changed, exposes previous content.
	r3 := c.RecordRead("/abs/foo.go", 0, 10, "alpha\nGAMMA", "full")
	if r3.Status != ReadDedupChanged {
		t.Fatalf("changed status=%q want changed", r3.Status)
	}
	if r3.PrevContent != "alpha\nbeta" {
		t.Fatalf("changed PrevContent=%q want previous", r3.PrevContent)
	}

	// A different window of the same file → new, gets a fresh label F2.
	r4 := c.RecordRead("/abs/foo.go", 20, 30, "other", "full")
	if r4.Status != ReadDedupNew || r4.Label != "F2" {
		t.Fatalf("different window: status=%q label=%q want new/F2", r4.Status, r4.Label)
	}
}

func TestRecordReadFidelityAware(t *testing.T) {
	c := NewSessionCache("sess-fidelity")
	const body = "func F() {}\nfunc G() {}"

	// First read delivered at signatures fidelity.
	r1 := c.RecordRead("/abs/svc.go", 0, 10, body, "signatures")
	if r1.Status != ReadDedupNew {
		t.Fatalf("first read status=%q want new", r1.Status)
	}

	// Re-read at the same fidelity → collapses to unchanged.
	if got := c.RecordRead("/abs/svc.go", 0, 10, body, "signatures").Status; got != ReadDedupUnchanged {
		t.Fatalf("same-fidelity re-read status=%q want unchanged", got)
	}

	// Re-read wanting full fidelity → must NOT collapse (upgraded), so the full
	// read proceeds instead of being masked by a stub referencing a compressed read.
	r3 := c.RecordRead("/abs/svc.go", 0, 10, body, "full")
	if r3.Status != ReadDedupUpgraded {
		t.Fatalf("upgrade re-read status=%q want upgraded", r3.Status)
	}
	if r3.Label != "F1" {
		t.Fatalf("upgrade keeps label, got %q want F1", r3.Label)
	}

	// Now that full was delivered, any further re-read collapses (full covers all).
	if got := c.RecordRead("/abs/svc.go", 0, 10, body, "signatures").Status; got != ReadDedupUnchanged {
		t.Fatalf("post-full re-read status=%q want unchanged", got)
	}

	// Only the two genuine collapses counted as dedup hits.
	if got := c.Stats().DedupHits; got != 2 {
		t.Fatalf("DedupHits=%d want 2", got)
	}
}

func TestRecordReadClearResets(t *testing.T) {
	c := NewSessionCache("sess-clear")
	c.RecordRead("/abs/a.go", 0, 5, "x", "full")
	c.RecordRead("/abs/a.go", 0, 5, "x", "full") // dedup hit
	if c.Stats().DedupHits != 1 {
		t.Fatal("expected a dedup hit before clear")
	}
	c.Clear()
	if c.Stats().DedupHits != 0 {
		t.Fatal("Clear should reset dedup hits")
	}
	// After clear, the same window is treated as new again (label restarts at F1).
	r := c.RecordRead("/abs/a.go", 0, 5, "x", "full")
	if r.Status != ReadDedupNew || r.Label != "F1" {
		t.Fatalf("after clear: status=%q label=%q want new/F1", r.Status, r.Label)
	}
}
