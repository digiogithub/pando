package kb

import (
	"fmt"
	"testing"
	"time"
)

// TestSelfWriteMatchesRepeatedly covers PANDO-US-0004's first acceptance
// criterion at the map level: a single filesystem write commonly surfaces as
// more than one fsnotify event for the same path/mtime (e.g. a new file's
// Create followed by its Write), so a recorded self-write must keep matching
// every one of them, not just the first.
func TestSelfWriteMatchesRepeatedly(t *testing.T) {
	store := &KBStore{}
	store.recordSelfWrite("/tmp/a.md", 1234)

	if !store.consumeSelfWrite("/tmp/a.md", 1234) {
		t.Fatal("expected matching self-write to be recognized")
	}
	if !store.consumeSelfWrite("/tmp/a.md", 1234) {
		t.Fatal("expected a second event for the same write to also be recognized")
	}
}

// TestSelfWriteMismatchedMtimeNotConsumed covers the second acceptance
// criterion: an event for the same path with a different mtime (e.g. a hand
// edit that landed right after a mirror write) must not match.
func TestSelfWriteMismatchedMtimeNotConsumed(t *testing.T) {
	store := &KBStore{}
	store.recordSelfWrite("/tmp/a.md", 1234)

	if store.consumeSelfWrite("/tmp/a.md", 9999) {
		t.Fatal("expected a different mtime not to match the recorded self-write")
	}
}

func TestSelfDeleteMatchesRepeatedly(t *testing.T) {
	store := &KBStore{}
	store.recordSelfDelete("/tmp/a.md")

	if !store.consumeSelfDelete("/tmp/a.md") {
		t.Fatal("expected the recorded self-delete to be recognized")
	}
	if !store.consumeSelfDelete("/tmp/a.md") {
		t.Fatal("expected a second event for the same delete to also be recognized")
	}
}

func TestSelfWriteDoesNotMatchSelfDeleteEntry(t *testing.T) {
	store := &KBStore{}
	store.recordSelfDelete("/tmp/a.md")

	if store.consumeSelfWrite("/tmp/a.md", 0) {
		t.Fatal("a self-delete entry must not be reported as a matching self-write")
	}
}

func TestSelfWriteEntryExpiresAfterTTL(t *testing.T) {
	store := &KBStore{}
	store.recordSelfWrite("/tmp/a.md", 1234)

	// Force the recorded entry into the past instead of sleeping selfWriteTTL.
	store.selfWriteMu.Lock()
	entry := store.selfWrites["/tmp/a.md"]
	entry.expiresAt = time.Now().Add(-time.Second)
	store.selfWrites["/tmp/a.md"] = entry
	store.selfWriteMu.Unlock()

	if store.consumeSelfWrite("/tmp/a.md", 1234) {
		t.Fatal("expected an expired self-write entry not to match")
	}
}

// TestSelfWriteMapBoundedBySizeCap covers the third acceptance criterion:
// writing many documents (far more than selfWriteCap) must not grow the map
// past the cap.
func TestSelfWriteMapBoundedBySizeCap(t *testing.T) {
	store := &KBStore{}
	for i := 0; i < selfWriteCap*3; i++ {
		store.recordSelfWrite(fmt.Sprintf("/tmp/doc-%d.md", i), int64(i))
		if got := store.selfWriteCount(); got > selfWriteCap {
			t.Fatalf("selfWriteCount() = %d after %d writes, want <= %d (cap)", got, i+1, selfWriteCap)
		}
	}
	if got := store.selfWriteCount(); got > selfWriteCap {
		t.Fatalf("selfWriteCount() = %d, want <= %d (cap)", got, selfWriteCap)
	}
}

// TestSelfWriteMapBoundedEvenWithinOneTTLWindow ensures the cap is enforced
// by eviction, not just by the TTL sweep: every entry here is still fresh
// (recorded well within selfWriteTTL of each other), so only evictOldestSelfWriteLocked
// can be keeping the map bounded.
func TestSelfWriteMapBoundedEvenWithinOneTTLWindow(t *testing.T) {
	store := &KBStore{}
	for i := 0; i < selfWriteCap+500; i++ {
		store.recordSelfWrite(fmt.Sprintf("/tmp/burst-%d.md", i), int64(i))
	}
	if got := store.selfWriteCount(); got > selfWriteCap {
		t.Fatalf("selfWriteCount() = %d, want <= %d (cap) even within one TTL window", got, selfWriteCap)
	}
}
