package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/telemetry"
)

// fakeSink is a minimal in-memory RemoteSink implementation used to assert
// exactly what teeHandler forwards to the remote side.
type fakeSink struct {
	mu    sync.Mutex
	calls []sinkCall

	// minLevel gates Enabled. Its zero value is slog.LevelInfo, which is
	// low enough that every existing test in this file (none of which logs
	// below Info through a fakeSink) keeps behaving as "always enabled";
	// a test that specifically exercises level gating sets it explicitly.
	minLevel slog.Level
	disabled bool
}

type sinkCall struct {
	time  time.Time
	level slog.Level
	msg   string
	attrs []slog.Attr
}

func (f *fakeSink) Enabled(level slog.Level) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.disabled && level >= f.minLevel
}

func (f *fakeSink) Handle(_ context.Context, t time.Time, level slog.Level, msg string, attrs []slog.Attr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sinkCall{time: t, level: level, msg: msg, attrs: append([]slog.Attr{}, attrs...)})
}

func (f *fakeSink) snapshot() []sinkCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sinkCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// attrString returns the string value of the first attr in attrs whose key
// matches, plus whether it was found.
func attrString(attrs []slog.Attr, key string) (string, bool) {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value.String(), true
		}
	}
	return "", false
}

func withCleanRemoteSink(t *testing.T) {
	t.Helper()
	SetRemoteSink(nil)
	t.Cleanup(func() { SetRemoteSink(nil) })
}

// TestTeeHandlerNilSinkPrimaryOnly verifies that with no remote sink set,
// every record still reaches the primary handler unchanged, and Enabled()
// reflects only the primary's own level gate.
func TestTeeHandlerNilSinkPrimaryOnly(t *testing.T) {
	withCleanRemoteSink(t)

	var buf bytes.Buffer
	h := NewTeeHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger := slog.New(h)

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("Enabled(Debug) = true with no sink and primary at Info, want false")
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled(Info) = false with primary at Info, want true")
	}

	logger.Info("hello primary", "n", 42)

	out := buf.String()
	if !strings.Contains(out, `"msg":"hello primary"`) {
		t.Fatalf("primary output = %q, want it to contain the logged message", out)
	}
	if !strings.Contains(out, `"n":42`) {
		t.Fatalf("primary output = %q, want it to contain the record attr", out)
	}
}

// TestTeeHandlerForwardsAttrsAndGroups verifies WithAttrs/WithGroup state is
// correctly flattened into dotted keys for the remote sink: an attr added
// before WithGroup stays unprefixed, one added after is prefixed with the
// group name, and so are a record's own attrs logged while the group is
// open.
func TestTeeHandlerForwardsAttrsAndGroups(t *testing.T) {
	withCleanRemoteSink(t)
	sink := &fakeSink{}
	SetRemoteSink(sink)

	var buf bytes.Buffer
	h := NewTeeHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger := slog.New(h)

	logger = logger.With("a", 1).WithGroup("g").With("b", 2)
	logger.Info("hello", "c", 3)

	calls := sink.snapshot()
	if len(calls) != 1 {
		t.Fatalf("sink received %d calls, want 1", len(calls))
	}
	got := calls[0]
	if got.msg != "hello" || got.level != slog.LevelInfo {
		t.Fatalf("sink call = %+v, want msg=hello level=INFO", got)
	}

	want := map[string]string{"a": "1", "g.b": "2", "g.c": "3"}
	for key, wantVal := range want {
		gotVal, ok := attrString(got.attrs, key)
		if !ok {
			t.Fatalf("sink attrs %+v missing key %q", got.attrs, key)
		}
		if gotVal != wantVal {
			t.Fatalf("sink attr %q = %q, want %q", key, gotVal, wantVal)
		}
	}
	if len(got.attrs) != len(want) {
		t.Fatalf("sink attrs = %+v, want exactly %v", got.attrs, want)
	}

	// The primary handler must also have received the record, independent
	// of anything routed to the sink.
	if !strings.Contains(buf.String(), `"msg":"hello"`) {
		t.Fatalf("primary output = %q, want it to contain the logged message", buf.String())
	}
}

// TestTeeHandlerSkipAttrNotForwarded verifies a record carrying
// telemetry.SkipAttrKey reaches the primary handler but never the sink —
// the shipper's own loop-guarded diagnostic logs rely on this.
func TestTeeHandlerSkipAttrNotForwarded(t *testing.T) {
	withCleanRemoteSink(t)
	sink := &fakeSink{}
	SetRemoteSink(sink)

	var buf bytes.Buffer
	h := NewTeeHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger := slog.New(h)

	logger.Warn("shipping disabled", telemetry.SkipAttrKey, true)

	if calls := sink.snapshot(); len(calls) != 0 {
		t.Fatalf("sink received %d calls for a SkipAttrKey record, want 0: %+v", len(calls), calls)
	}
	if !strings.Contains(buf.String(), `"msg":"shipping disabled"`) {
		t.Fatalf("primary output = %q, want it to still contain the skip-tagged message", buf.String())
	}
}

// TestTeeHandlerEnabledWhenOnlySinkWantsLevel verifies the documented
// Enabled() rule: it returns true when the sink is active even if the
// primary alone would not want the level, and Handle then still only
// forwards to primary when primary.Enabled agrees — the sink's own minimum
// level filtering is its own responsibility (see
// internal/telemetry.Shipper.Handle).
func TestTeeHandlerEnabledWhenOnlySinkWantsLevel(t *testing.T) {
	withCleanRemoteSink(t)
	sink := &fakeSink{}
	SetRemoteSink(sink)

	var buf bytes.Buffer
	// Primary only wants Warn and above.
	h := NewTeeHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	logger := slog.New(h)

	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled(Info) = false with an active sink, want true")
	}

	logger.Info("info level record")

	if buf.Len() != 0 {
		t.Fatalf("primary output = %q, want empty (primary is Warn-and-above)", buf.String())
	}
	if calls := sink.snapshot(); len(calls) != 1 || calls[0].msg != "info level record" {
		t.Fatalf("sink calls = %+v, want exactly one for the Info record", calls)
	}
}

// TestTeeHandlerSkipAttrNotForwardedUnderGroup is the WithGroup counterpart
// of TestTeeHandlerSkipAttrNotForwarded: a group-scoped logger flattens
// telemetry.SkipAttrKey into "g.$_telemetry_skip" before it reaches the
// handler, and an exact-match check on the marker would miss it, letting
// the shipper's own loop-guarded diagnostic log (or any other grouped
// caller) leak into the sink.
func TestTeeHandlerSkipAttrNotForwardedUnderGroup(t *testing.T) {
	withCleanRemoteSink(t)
	sink := &fakeSink{}
	SetRemoteSink(sink)

	var buf bytes.Buffer
	h := NewTeeHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger := slog.New(h)

	logger.WithGroup("g").Warn("grouped skip", telemetry.SkipAttrKey, true)
	logger.Info("normal record", "k", "v")

	calls := sink.snapshot()
	if len(calls) != 1 || calls[0].msg != "normal record" {
		t.Fatalf("sink calls = %+v, want exactly the non-skip-tagged record", calls)
	}
}

// TestTeeHandlerRespectsSinkMinLevel verifies the level-gating rule added in
// review: a Debug record is dispatched to neither primary nor sink when
// both are configured above Debug, and Enabled() reflects that (so
// logging.Debug()'s underlying slog call does not even build the record).
// This is what keeps a sink whose effective minimum level has been clamped
// to Info (see internal/app's telemetry runtime, for cfg.Debug == false)
// from ever receiving Debug-level content just because remote telemetry
// happens to be enabled.
func TestTeeHandlerRespectsSinkMinLevel(t *testing.T) {
	withCleanRemoteSink(t)
	sink := &fakeSink{minLevel: slog.LevelInfo}
	SetRemoteSink(sink)

	var buf bytes.Buffer
	h := NewTeeHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	logger := slog.New(h)

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("Enabled(Debug) = true with primary at Warn and sink at Info, want false")
	}

	logger.Debug("should not ship")
	logger.Info("should ship")

	if buf.Len() != 0 {
		t.Fatalf("primary output = %q, want empty (primary is Warn-and-above)", buf.String())
	}
	calls := sink.snapshot()
	if len(calls) != 1 || calls[0].msg != "should ship" {
		t.Fatalf("sink calls = %+v, want exactly the Info-level record", calls)
	}
}

// TestTeeHandlerNeverPanicsUnderConcurrency is a light smoke/race test: many
// goroutines logging concurrently while the sink is toggled on and off must
// never panic or deadlock (also run with -race).
func TestTeeHandlerNeverPanicsUnderConcurrency(t *testing.T) {
	withCleanRemoteSink(t)

	var buf bytes.Buffer
	h := NewTeeHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger := slog.New(h)
	sink := &fakeSink{}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if j%2 == 0 {
					SetRemoteSink(sink)
				} else {
					SetRemoteSink(nil)
				}
				logger.Info("concurrent", "n", n, "j", j)
			}
		}(i)
	}
	wg.Wait()
}
