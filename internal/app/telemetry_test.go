package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/telemetry"
)

// telemetryTestIngest is a minimal httptest-backed mock of the Better Stack
// ingest endpoint: it records every batch of Records it receives and always
// answers 202, exactly like the real endpoint on success.
type telemetryTestIngest struct {
	mu      sync.Mutex
	records []telemetry.Record
	srv     *httptest.Server
}

func newTelemetryTestIngest(t *testing.T) *telemetryTestIngest {
	t.Helper()
	ts := &telemetryTestIngest{}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var batch []telemetry.Record
		_ = json.Unmarshal(data, &batch)
		ts.mu.Lock()
		ts.records = append(ts.records, batch...)
		ts.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *telemetryTestIngest) snapshot() []telemetry.Record {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]telemetry.Record, len(ts.records))
	copy(out, ts.records)
	return out
}

func (ts *telemetryTestIngest) reset() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.records = nil
}

func findRecord(records []telemetry.Record, msg string) (telemetry.Record, bool) {
	for _, r := range records {
		if r.Message == msg {
			return r, true
		}
	}
	return telemetry.Record{}, false
}

// TestTelemetryRuntimeHotToggle drives telemetryRuntime.apply directly (the
// same reconciliation config.Bus events trigger via watch) against a mock
// ingest server: enabling starts a shipper whose records carry the
// configured debug id, and disabling clears the remote sink immediately so
// no further record is ever routed to it.
func TestTelemetryRuntimeHotToggle(t *testing.T) {
	ingest := newTelemetryTestIngest(t)
	t.Setenv("PANDO_TELEMETRY_ENDPOINT", ingest.srv.URL)
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	t.Cleanup(func() { logging.SetRemoteSink(nil) })

	if !telemetry.Available() {
		t.Fatal("telemetry.Available() = false with PANDO_TELEMETRY_TOKEN set")
	}

	// internal/config.Load normally installs a tee handler as slog's
	// default; this test exercises telemetryRuntime in isolation, so it
	// installs an equivalent one directly, and restores the previous
	// default afterwards to avoid leaking state into other tests in this
	// package.
	origDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(origDefault) })
	slog.SetDefault(slog.New(logging.NewTeeHandler(slog.NewTextHandler(io.Discard, nil))))

	tr := &telemetryRuntime{mode: "test"}

	// --- Enable ---
	tr.apply(&config.Config{Telemetry: config.TelemetryConfig{Enabled: true, DebugID: "1111111111111111", MinLevel: config.TelemetryLevelInfo}})
	if !logging.RemoteSinkActive() {
		t.Fatal("RemoteSinkActive() = false after enabling telemetry")
	}

	logging.Info("hello from hot toggle test")

	tr.mu.Lock()
	shipper := tr.shipper
	tr.mu.Unlock()
	if shipper == nil {
		t.Fatal("telemetryRuntime.shipper is nil after enabling")
	}
	if err := shipper.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := ingest.snapshot()
	rec, ok := findRecord(got, "hello from hot toggle test")
	if !ok {
		t.Fatalf("ingest never received the test record, got %+v", got)
	}
	// Record.DebugID carries the dashed display format, the same one shown
	// in Settings and copied into a support issue.
	if rec.DebugID != "1111-1111-1111-1111" {
		t.Fatalf("record debug_id = %q, want %q", rec.DebugID, "1111-1111-1111-1111")
	}
	if rec.App.Mode != "test" {
		t.Fatalf("record app.mode = %q, want %q", rec.App.Mode, "test")
	}
	// startLocked also ships its own "Telemetry enabled" record.
	if _, ok := findRecord(got, "Telemetry enabled"); !ok {
		t.Fatalf("ingest never received the telemetry.enabled lifecycle record, got %+v", got)
	}

	// --- Disable ---
	ingest.reset()
	tr.apply(&config.Config{Telemetry: config.TelemetryConfig{Enabled: false, DebugID: "1111111111111111", MinLevel: config.TelemetryLevelInfo}})
	if logging.RemoteSinkActive() {
		t.Fatal("RemoteSinkActive() = true after disabling telemetry")
	}

	logging.Info("should never be shipped")
	if got := ingest.snapshot(); len(got) != 0 {
		t.Fatalf("ingest received %d records after disable, want 0: %+v", len(got), got)
	}

	if err := tr.shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

// TestTelemetryRuntimeApplyNoopAfterShutdown verifies the race fix from code
// review: an apply() call that runs after shutdown() has already torn the
// runtime down (simulating a config.Bus event that was already in flight
// through watch() at shutdown time, or arrives just after) must never
// restart a shipper or reinstall the remote sink — both would leak a
// shipper goroutine nothing will ever stop again.
func TestTelemetryRuntimeApplyNoopAfterShutdown(t *testing.T) {
	ingest := newTelemetryTestIngest(t)
	t.Setenv("PANDO_TELEMETRY_ENDPOINT", ingest.srv.URL)
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	t.Cleanup(func() { logging.SetRemoteSink(nil) })

	tr := &telemetryRuntime{mode: "test"}
	cfg := &config.Config{Telemetry: config.TelemetryConfig{Enabled: true, DebugID: "2222222222222222", MinLevel: config.TelemetryLevelInfo}}

	tr.apply(cfg)
	if !logging.RemoteSinkActive() {
		t.Fatal("RemoteSinkActive() = false after enabling telemetry")
	}

	if err := tr.shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if logging.RemoteSinkActive() {
		t.Fatal("RemoteSinkActive() = true right after shutdown")
	}

	// A late apply(), as watch()'s goroutine could still deliver after
	// shutdown has cancelled its context but before the goroutine notices —
	// must be a no-op: no new shipper, no reinstalled sink.
	tr.apply(cfg)
	if logging.RemoteSinkActive() {
		t.Fatal("RemoteSinkActive() = true after apply() following shutdown — the closed guard did not hold")
	}
	tr.mu.Lock()
	shipper := tr.shipper
	tr.mu.Unlock()
	if shipper != nil {
		t.Fatal("telemetryRuntime.shipper is non-nil after apply() following shutdown")
	}
}

// TestTelemetryRuntimeDebugLevelClampedWithoutAppDebug verifies the
// effective-minimum-level rule from code review: a "debug" telemetry level
// only actually ships Debug records when the app-wide Debug flag is also on;
// otherwise it is silently clamped up to Info, both in what Options.MinLevel
// the shipper is constructed with and in what teeHandler/Shipper.Enabled
// then honor end to end.
func TestTelemetryRuntimeDebugLevelClampedWithoutAppDebug(t *testing.T) {
	ingest := newTelemetryTestIngest(t)
	t.Setenv("PANDO_TELEMETRY_ENDPOINT", ingest.srv.URL)
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	t.Cleanup(func() { logging.SetRemoteSink(nil) })

	origDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(origDefault) })
	slog.SetDefault(slog.New(logging.NewTeeHandler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))))

	tr := &telemetryRuntime{mode: "test"}
	tr.apply(&config.Config{
		Debug:     false,
		Telemetry: config.TelemetryConfig{Enabled: true, DebugID: "3333333333333333", MinLevel: config.TelemetryLevelDebug},
	})
	t.Cleanup(func() { _ = tr.shutdown(context.Background()) })

	tr.mu.Lock()
	got := tr.currentMinLevel
	tr.mu.Unlock()
	if got != slog.LevelInfo {
		t.Fatalf("currentMinLevel = %v, want %v (clamped up from Debug since cfg.Debug is false)", got, slog.LevelInfo)
	}

	logging.Debug("should not ship: app debug is off")
	logging.Info("should ship")

	tr.mu.Lock()
	shipper := tr.shipper
	tr.mu.Unlock()
	if err := shipper.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got2 := ingest.snapshot()
	if _, ok := findRecord(got2, "should not ship: app debug is off"); ok {
		t.Fatalf("a Debug record was shipped despite cfg.Debug == false: %+v", got2)
	}
	if _, ok := findRecord(got2, "should ship"); !ok {
		t.Fatalf("the Info record was not shipped: %+v", got2)
	}
}

// TestTelemetryRuntimeUnavailableBuildNeverStarts verifies apply() never
// starts a shipper when internal/telemetry.Available() is false (no token),
// even if the config says Enabled — matches internal/telemetry.Available's
// documented contract that a tokenless build always shows telemetry as
// unavailable regardless of what the config requests.
func TestTelemetryRuntimeUnavailableBuildNeverStarts(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	t.Cleanup(func() { logging.SetRemoteSink(nil) })

	if telemetry.Available() {
		t.Skip("this build carries a real telemetry token (ldflags); skipping the unavailable-build case")
	}

	tr := &telemetryRuntime{mode: "test"}
	tr.apply(&config.Config{Telemetry: config.TelemetryConfig{Enabled: true, DebugID: "1111111111111111", MinLevel: config.TelemetryLevelInfo}})

	if logging.RemoteSinkActive() {
		t.Fatal("RemoteSinkActive() = true with Enabled=true but no telemetry token available")
	}
	tr.mu.Lock()
	shipper := tr.shipper
	tr.mu.Unlock()
	if shipper != nil {
		t.Fatal("telemetryRuntime started a shipper despite telemetry.Available() = false")
	}
}
