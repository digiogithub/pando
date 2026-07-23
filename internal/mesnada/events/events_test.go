package events

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTestLog(t *testing.T, maxEntries int) (*Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := Open(path, maxEntries)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return l, path
}

func TestAppendAssignsIncreasingSeq(t *testing.T) {
	l, _ := openTestLog(t, 0)

	first, err := l.Append(Event{Kind: KindCompleted, TaskID: "t1"})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	second, err := l.Append(Event{Kind: KindFailed, TaskID: "t2"})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seq = %d, %d; want 1, 2", first.Seq, second.Seq)
	}
	if first.CreatedAt.IsZero() {
		t.Error("Append() did not stamp CreatedAt")
	}
	if l.LastSeq() != 2 {
		t.Errorf("LastSeq() = %d, want 2", l.LastSeq())
	}
}

func TestAppendRejectsEventWithoutKind(t *testing.T) {
	l, _ := openTestLog(t, 0)
	if _, err := l.Append(Event{TaskID: "t1"}); err == nil {
		t.Fatal("Append() with no kind should fail")
	}
}

func TestUnseenAndAckAdvanceCursor(t *testing.T) {
	l, _ := openTestLog(t, 0)
	l.Append(Event{Kind: KindCompleted, TaskID: "t1"})
	l.Append(Event{Kind: KindCompleted, TaskID: "t2"})

	unseen := l.Unseen("sup")
	if len(unseen) != 2 {
		t.Fatalf("Unseen() returned %d events, want 2", len(unseen))
	}
	if unseen[0].TaskID != "t1" || unseen[1].TaskID != "t2" {
		t.Fatalf("Unseen() out of order: %s, %s", unseen[0].TaskID, unseen[1].TaskID)
	}

	if err := l.Ack("sup", unseen[0].Seq); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	rest := l.Unseen("sup")
	if len(rest) != 1 || rest[0].TaskID != "t2" {
		t.Fatalf("after ack, Unseen() = %+v; want only t2", rest)
	}

	// Another subscription has its own cursor and still sees everything.
	if got := len(l.Unseen("other")); got != 2 {
		t.Errorf("second subscription saw %d events, want 2", got)
	}
}

func TestAckNeverMovesCursorBackwards(t *testing.T) {
	l, _ := openTestLog(t, 0)
	l.Append(Event{Kind: KindCompleted, TaskID: "t1"})
	l.Append(Event{Kind: KindCompleted, TaskID: "t2"})

	l.Ack("sup", 2)
	l.Ack("sup", 1)

	if got := l.Cursor("sup"); got != 2 {
		t.Fatalf("Cursor() = %d, want 2 (a stale ack must not rewind)", got)
	}
	if got := len(l.Unseen("sup")); got != 0 {
		t.Errorf("Unseen() = %d events, want 0", got)
	}
}

func TestUnackedEventsSurviveReopen(t *testing.T) {
	l, path := openTestLog(t, 0)
	l.Append(Event{Kind: KindCompleted, TaskID: "t1", ParentSessionID: "s1"})
	l.Append(Event{Kind: KindCompleted, TaskID: "t2", ParentSessionID: "s1"})
	l.Ack("sup", 1)

	// Simulate a restart: a fresh Log over the same files.
	reopened, err := Open(path, 0)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	unseen := reopened.Unseen("sup")
	if len(unseen) != 1 || unseen[0].TaskID != "t2" {
		t.Fatalf("after restart Unseen() = %+v; want the single unacked t2", unseen)
	}
	if unseen[0].ParentSessionID != "s1" {
		t.Errorf("ParentSessionID = %q, want s1", unseen[0].ParentSessionID)
	}
	if reopened.LastSeq() != 2 {
		t.Errorf("LastSeq() after reopen = %d, want 2", reopened.LastSeq())
	}
}

func TestUnknownSubscriptionReplaysRetainedLog(t *testing.T) {
	l, _ := openTestLog(t, 0)
	l.Append(Event{Kind: KindCompleted, TaskID: "t1"})

	// A consumer that never acked (or lost its cursor file) must see history
	// rather than silently skip it.
	if got := len(l.Unseen("brand-new")); got != 1 {
		t.Fatalf("Unseen() for unknown subscription = %d, want 1", got)
	}
}

func TestCompactionTrimsLogAndClampsLaggingCursor(t *testing.T) {
	l, path := openTestLog(t, 2) // compaction triggers past 2*2 = 4 retained events

	for range 5 {
		if _, err := l.Append(Event{Kind: KindCompleted, TaskID: "t"}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	retained := l.Recent(100)
	if len(retained) != 2 {
		t.Fatalf("after compaction %d events retained, want 2", len(retained))
	}
	if retained[0].Seq != 4 || retained[1].Seq != 5 {
		t.Fatalf("retained seqs = %d, %d; want the newest 4, 5", retained[0].Seq, retained[1].Seq)
	}
	// A consumer that never acked is clamped to the new floor, so it does not
	// stall waiting for events that no longer exist.
	if got := l.Cursor("lagging"); got != 0 {
		t.Errorf("unknown cursor = %d, want 0 (only registered cursors are clamped)", got)
	}
	if got := len(l.Unseen("lagging")); got != 2 {
		t.Errorf("Unseen() = %d, want the 2 retained events", got)
	}

	// The rewritten file must match the trimmed in-memory view.
	reopened, err := Open(path, 2)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if got := len(reopened.Recent(100)); got != 2 {
		t.Errorf("reopened log has %d events, want 2", got)
	}
	if reopened.LastSeq() != 5 {
		t.Errorf("reopened LastSeq() = %d, want 5", reopened.LastSeq())
	}
}

func TestCompactionClampsRegisteredCursor(t *testing.T) {
	l, _ := openTestLog(t, 2)
	l.Append(Event{Kind: KindCompleted, TaskID: "t"})
	l.Ack("sup", 1) // registered, now far behind
	for range 4 {
		l.Append(Event{Kind: KindCompleted, TaskID: "t"})
	}

	if got := l.Cursor("sup"); got != 3 {
		t.Fatalf("Cursor() = %d, want 3 (clamped to the retained floor)", got)
	}
	if got := len(l.Unseen("sup")); got != 2 {
		t.Errorf("Unseen() = %d, want 2", got)
	}
}

func TestNilLogIsNoOp(t *testing.T) {
	var l *Log
	if _, err := l.Append(Event{Kind: KindCompleted}); err != nil {
		t.Errorf("nil Append() error = %v", err)
	}
	if err := l.Ack("sup", 5); err != nil {
		t.Errorf("nil Ack() error = %v", err)
	}
	if l.Unseen("sup") != nil || l.Recent(10) != nil || l.LastSeq() != 0 || l.Cursor("s") != 0 {
		t.Error("nil log should return zero values")
	}
}

func TestKindIsTerminal(t *testing.T) {
	terminal := []Kind{KindCompleted, KindFailed, KindCancelled}
	other := []Kind{KindGateFailed, KindBreakerTripped, KindRespawnRefused, KindReclaimed, Kind("")}
	for _, k := range terminal {
		if !k.IsTerminal() {
			t.Errorf("%s should be terminal", k)
		}
	}
	for _, k := range other {
		if k.IsTerminal() {
			t.Errorf("%s should not be terminal", k)
		}
	}
}

func TestCorruptLineIsSkipped(t *testing.T) {
	l, path := openTestLog(t, 0)
	l.Append(Event{Kind: KindCompleted, TaskID: "t1", CreatedAt: time.Now()})

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open error = %v", err)
	}
	f.WriteString("{not json\n")
	f.Close()

	reopened, err := Open(path, 0)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if got := len(reopened.Recent(10)); got != 1 {
		t.Fatalf("reopened log has %d events, want 1 (corrupt line skipped)", got)
	}
}
