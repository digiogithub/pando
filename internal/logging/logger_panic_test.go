package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestRecoverPanicSendsPanicRecordToRemoteSink verifies that RecoverPanic,
// when a remote sink is active, ships a dedicated "panic" event carrying the
// panic value and stack trace, and does so synchronously (RecoverPanic
// returns only after FlushRemoteSink completes), before continuing its
// existing local-log/file/cleanup behavior.
func TestRecoverPanicSendsPanicRecordToRemoteSink(t *testing.T) {
	t.Chdir(t.TempDir())
	withCleanRemoteSink(t)

	sink := &flushableFakeSink{fakeSink: &fakeSink{}}
	SetRemoteSink(sink)

	cleaned := false
	func() {
		defer RecoverPanic("test-panic", func() { cleaned = true })
		panic("boom")
	}()

	if !cleaned {
		t.Fatal("cleanup was not invoked")
	}

	calls := sink.snapshot()
	if len(calls) != 1 {
		t.Fatalf("sink received %d calls, want 1", len(calls))
	}
	got := calls[0]
	if got.level != slog.LevelError {
		t.Fatalf("panic record level = %v, want Error", got.level)
	}
	if !strings.Contains(got.msg, "boom") {
		t.Fatalf("panic record message = %q, want it to mention the panic value", got.msg)
	}
	event, _ := attrString(got.attrs, "event")
	if event != "panic" {
		t.Fatalf("panic record event attr = %q, want %q", event, "panic")
	}
	stack, ok := attrString(got.attrs, "stack")
	if !ok || !strings.Contains(stack, "TestRecoverPanicSendsPanicRecordToRemoteSink") {
		t.Fatalf("panic record stack attr = %q, want it to contain a real stack trace", stack)
	}

	if sink.flushCalls == 0 {
		t.Fatal("Flush was not called on the remote sink after a panic")
	}
}

// TestRecoverPanicNoSinkStillRecoversNormally verifies RecoverPanic's
// existing behavior (recover + cleanup) is unaffected when no remote sink is
// set.
func TestRecoverPanicNoSinkStillRecoversNormally(t *testing.T) {
	t.Chdir(t.TempDir())
	withCleanRemoteSink(t)

	cleaned := false
	func() {
		defer RecoverPanic("test-panic-nosink", func() { cleaned = true })
		panic("boom again")
	}()

	if !cleaned {
		t.Fatal("cleanup was not invoked")
	}
}

// TestRecoverPanicShipsExactlyOnePanicRecord verifies RecoverPanic does not
// double-ship a panic: with slog's default logger wrapped by the tee handler
// (as internal/config.Load wires it in production), ErrorPersist's own
// generic "Panic in ..." log line must not also reach the remote sink —
// only the single, dedicated "event=panic" record (built directly against
// the sink, with the full stack trace) should.
func TestRecoverPanicShipsExactlyOnePanicRecord(t *testing.T) {
	t.Chdir(t.TempDir())
	withCleanRemoteSink(t)

	origDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(origDefault) })
	slog.SetDefault(slog.New(NewTeeHandler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))))

	sink := &flushableFakeSink{fakeSink: &fakeSink{}}
	SetRemoteSink(sink)

	func() {
		defer RecoverPanic("test-panic-dedup", func() {})
		panic("boom once")
	}()

	// ErrorPersist's own generic "Panic in ..." line and the dedicated
	// "event=panic" record share the exact same message text — that's the
	// duplication finding 11 flagged — so the regression check is "exactly
	// one shipped record carries this message", not "exactly one call
	// total" (RecoverPanic also ships an unrelated, legitimate "Panic
	// details written to <file>" InfoPersist line once the panic file is
	// written, which is not part of this duplication and is out of scope
	// here).
	const panicMsg = "Panic in test-panic-dedup: boom once"
	var matches []sinkCall
	for _, c := range sink.snapshot() {
		if c.msg == panicMsg {
			matches = append(matches, c)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("sink received %d records with message %q, want exactly 1 (no double-ship): %+v", len(matches), panicMsg, matches)
	}
	if event, _ := attrString(matches[0].attrs, "event"); event != "panic" {
		t.Fatalf("the one shipped record's event = %q, want %q (the dedicated record, not ErrorPersist's)", event, "panic")
	}
}

// flushableFakeSink adds a synchronous Flush (RemoteSinkFlusher) on top of
// fakeSink, so RecoverPanic's FlushRemoteSink call has something to invoke.
type flushableFakeSink struct {
	*fakeSink
	flushCalls int
}

func (f *flushableFakeSink) Flush(ctx context.Context) error {
	f.flushCalls++
	return nil
}
