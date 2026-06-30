package readmode

import (
	"sync"
	"testing"
)

func TestEscalateLadder(t *testing.T) {
	cases := []struct {
		base  Mode
		steps int
		want  Mode
	}{
		{ModeSignatures, 0, ModeSignatures},
		{ModeSignatures, 1, ModeMap},
		{ModeSignatures, 2, ModeFull},
		{ModeSignatures, 5, ModeFull}, // clamps
		{ModeMap, 0, ModeMap},
		{ModeMap, 1, ModeFull},
		{ModeFull, 3, ModeFull},
	}
	for _, c := range cases {
		if got := escalate(c.base, c.steps); got != c.want {
			t.Errorf("escalate(%s, %d)=%s want %s", c.base, c.steps, got, c.want)
		}
	}
}

func TestBounceUpgradesNextAutoRead(t *testing.T) {
	bt := NewBounceTracker()
	path := "/repo/pkg/server.go"

	// Before any bounce, an auto-resolved signatures read is honored as-is.
	if got := bt.Upgrade(path, ModeSignatures, false); got != ModeSignatures {
		t.Fatalf("pre-bounce upgrade=%s want signatures", got)
	}

	// Simulate a bounce: compressed read followed by a full read of the same path.
	bt.RecordCompressed(path, ModeSignatures, 400)
	if !bt.RecordFull(path) {
		t.Fatal("expected RecordFull to report a bounce")
	}

	// The next auto read of that path/extension is escalated one tier.
	if got := bt.Upgrade(path, ModeSignatures, false); got != ModeMap {
		t.Fatalf("post-1-bounce upgrade=%s want map", got)
	}

	// A second bounce escalates all the way to full.
	bt.RecordCompressed(path, ModeMap, 300)
	if !bt.RecordFull(path) {
		t.Fatal("expected second bounce")
	}
	if got := bt.Upgrade(path, ModeSignatures, false); got != ModeFull {
		t.Fatalf("post-2-bounce upgrade=%s want full", got)
	}

	st := bt.Stats()
	if st.Bounces != 2 {
		t.Fatalf("Bounces=%d want 2", st.Bounces)
	}
	if st.WastedBytes != 700 {
		t.Fatalf("WastedBytes=%d want 700", st.WastedBytes)
	}
}

func TestBounceAppliesPerExtension(t *testing.T) {
	bt := NewBounceTracker()
	// Bounce on one .go file...
	bt.RecordCompressed("/repo/a.go", ModeSignatures, 100)
	bt.RecordFull("/repo/a.go")

	// ...escalates a different, never-bounced .go file too (per-extension signal).
	if got := bt.Upgrade("/repo/b.go", ModeSignatures, false); got != ModeMap {
		t.Fatalf("sibling .go upgrade=%s want map", got)
	}
	// But an unrelated extension is unaffected.
	if got := bt.Upgrade("/repo/c.py", ModeSignatures, false); got != ModeSignatures {
		t.Fatalf("unrelated .py upgrade=%s want signatures", got)
	}
}

func TestRecordFullWithoutPendingIsNoBounce(t *testing.T) {
	bt := NewBounceTracker()
	if bt.RecordFull("/repo/x.go") {
		t.Fatal("full read with no pending compressed read must not bounce")
	}
	if bt.Stats().Bounces != 0 {
		t.Fatal("no bounce should be recorded")
	}
}

func TestLearningEscalationOffByDefault(t *testing.T) {
	bt := NewBounceTracker()
	// Accumulate a high posterior bounce rate on .ts without per-path hard bounces
	// being available for the *next* file (clear pending each time via full reads on
	// distinct paths so path counters don't drive the escalation).
	for i := range 4 {
		p := "/repo/mod" + string(rune('a'+i)) + ".ts"
		bt.RecordCompressed(p, ModeSignatures, 100)
		bt.RecordFull(p)
	}
	// Deterministic (learning=false): a fresh .ts path still escalates because the
	// per-extension hard-bounce counter is high — that is the deterministic signal.
	if got := bt.Upgrade("/repo/fresh.ts", ModeSignatures, false); got != ModeFull {
		t.Fatalf("deterministic ext escalation=%s want full (>=2 ext bounces)", got)
	}
}

func TestLearningEscalationUsesPosterior(t *testing.T) {
	bt := NewBounceTracker()
	// One compressed read that held (no bounce) then nothing else: posterior is
	// dominated by alpha, so learning must NOT escalate.
	bt.RecordCompressed("/repo/held.rs", ModeSignatures, 100)
	if got := bt.Upgrade("/repo/other.rs", ModeSignatures, true); got != ModeSignatures {
		t.Fatalf("low bounce-rate learning upgrade=%s want signatures", got)
	}

	// Now drive the .rs posterior bounce-rate high enough with several bounces but
	// on paths whose per-path counters won't help the target; the per-extension
	// hard counter will also rise, so to isolate the *learning* path we check a
	// fresh extension via a synthetic high-beta state.
	lt := NewBounceTracker()
	lt.beta[".kt"] = &betaCounter{alpha: 1, beta: 3} // 75% posterior bounce rate, 4 obs
	if got := lt.Upgrade("/repo/new.kt", ModeSignatures, true); got != ModeMap {
		t.Fatalf("learning escalation=%s want map (one extra step)", got)
	}
	// With learning disabled the same state does nothing.
	if got := lt.Upgrade("/repo/new.kt", ModeSignatures, false); got != ModeSignatures {
		t.Fatalf("learning-off=%s want signatures", got)
	}
}

func TestBounceResetClears(t *testing.T) {
	bt := NewBounceTracker()
	bt.RecordCompressed("/repo/a.go", ModeSignatures, 100)
	bt.RecordFull("/repo/a.go")
	if bt.Stats().Bounces == 0 {
		t.Fatal("expected a bounce before reset")
	}
	bt.Reset()
	if bt.Stats().Bounces != 0 || bt.Stats().WastedBytes != 0 {
		t.Fatal("Reset should clear counters")
	}
	if got := bt.Upgrade("/repo/a.go", ModeSignatures, false); got != ModeSignatures {
		t.Fatalf("after reset upgrade=%s want signatures", got)
	}
}

func TestBounceTrackerConcurrent(t *testing.T) {
	bt := NewBounceTracker()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p := "/repo/f" + string(rune('a'+n%5)) + ".go"
			bt.RecordCompressed(p, ModeSignatures, 10)
			bt.RecordFull(p)
			bt.Upgrade(p, ModeSignatures, true)
			bt.Stats()
		}(i)
	}
	wg.Wait()
}

func TestNilBounceTrackerSafe(t *testing.T) {
	var bt *BounceTracker
	bt.RecordCompressed("/x.go", ModeSignatures, 1) // no panic
	if bt.RecordFull("/x.go") {
		t.Fatal("nil tracker should not bounce")
	}
	if got := bt.Upgrade("/x.go", ModeSignatures, true); got != ModeSignatures {
		t.Fatalf("nil tracker upgrade=%s want signatures", got)
	}
	_ = bt.Stats()
	bt.Reset()
}
