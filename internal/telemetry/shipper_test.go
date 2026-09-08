package telemetry

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// testServer is a minimal httptest-backed mock of the Better Stack ingest
// endpoint: it records every batch it receives and returns a scripted status
// code per call (the last status repeats once the script is exhausted).
type testServer struct {
	mu       sync.Mutex
	requests [][]Record
	headers  []http.Header
	statuses []int
	calls    int
	srv      *httptest.Server
}

func newTestServer(statuses ...int) *testServer {
	ts := &testServer{statuses: statuses}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if r.Header.Get("Content-Encoding") == "gzip" {
			if gr, err := gzip.NewReader(bytes.NewReader(data)); err == nil {
				data, _ = io.ReadAll(gr)
			}
		}
		var records []Record
		_ = json.Unmarshal(data, &records)

		ts.mu.Lock()
		idx := ts.calls
		ts.calls++
		ts.requests = append(ts.requests, records)
		ts.headers = append(ts.headers, r.Header.Clone())
		status := http.StatusAccepted
		if len(ts.statuses) > 0 {
			if idx < len(ts.statuses) {
				status = ts.statuses[idx]
			} else {
				status = ts.statuses[len(ts.statuses)-1]
			}
		}
		ts.mu.Unlock()

		w.WriteHeader(status)
	}))
	return ts
}

func (ts *testServer) URL() string { return ts.srv.URL }
func (ts *testServer) Close()      { ts.srv.Close() }

func (ts *testServer) callCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.calls
}

func (ts *testServer) snapshot() ([][]Record, []http.Header) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	reqs := make([][]Record, len(ts.requests))
	copy(reqs, ts.requests)
	hdrs := make([]http.Header, len(ts.headers))
	copy(hdrs, ts.headers)
	return reqs, hdrs
}

func waitForCallCount(t *testing.T, ts *testServer, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ts.callCount() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := ts.callCount(); got < n {
		t.Fatalf("callCount = %d after %s, want >= %d", got, timeout, n)
	}
}

func baseOptions(endpoint string) Options {
	return Options{
		Endpoint:      endpoint,
		Token:         "test-token",
		DebugID:       "1234567890123456",
		Mode:          "cli",
		MinLevel:      slog.LevelDebug,
		QueueSize:     100,
		BatchSize:     100,
		BatchBytes:    1 << 20,
		FlushInterval: time.Hour, // effectively disabled unless a test wants it
		HTTPTimeout:   2 * time.Second,
	}
}

func TestShipperBatchesBySize(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	opts := baseOptions(ts.URL())
	opts.BatchSize = 3
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	for i := 0; i < 3; i++ {
		s.Enqueue(NewRecord(time.Now(), slog.LevelInfo, "msg", nil))
	}

	waitForCallCount(t, ts, 1, 2*time.Second)
	reqs, _ := ts.snapshot()
	if len(reqs[0]) != 3 {
		t.Fatalf("first batch has %d records, want 3", len(reqs[0]))
	}
}

func TestShipperBatchesByInterval(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	opts := baseOptions(ts.URL())
	opts.FlushInterval = 20 * time.Millisecond
	opts.BatchSize = 100
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	s.Enqueue(NewRecord(time.Now(), slog.LevelInfo, "msg", nil))

	waitForCallCount(t, ts, 1, time.Second)
}

func TestShipperPayloadShapeAndAuthHeader(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	opts := baseOptions(ts.URL())
	opts.DebugID = "9876543210123456"
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	s.Handle(ctx, time.Now(), slog.LevelInfo, "hello", []slog.Attr{slog.String("k", "v")})
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reqs, headers := ts.snapshot()
	if len(reqs) != 1 || len(reqs[0]) != 1 {
		t.Fatalf("unexpected requests: %#v", reqs)
	}
	rec := reqs[0][0]
	// Record.DebugID carries the dashed display format ("9876-5432-1012-3456"),
	// the same one users see in Settings and copy into a support issue —
	// not the raw undashed form Options.DebugID/config storage uses.
	if rec.DebugID != "9876-5432-1012-3456" {
		t.Errorf("debug_id = %q, want 9876-5432-1012-3456", rec.DebugID)
	}
	if rec.App.Mode != "cli" {
		t.Errorf("app.mode = %q, want cli", rec.App.Mode)
	}
	if rec.App.OS == "" || rec.App.Arch == "" || rec.App.Go == "" {
		t.Errorf("app metadata incomplete: %#v", rec.App)
	}
	if rec.Time.IsZero() {
		t.Errorf("dt is zero")
	}

	auth := headers[0].Get("Authorization")
	if auth != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", auth, "Bearer test-token")
	}
	if ct := headers[0].Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestShipper403Disables(t *testing.T) {
	ts := newTestServer(http.StatusForbidden)
	defer ts.Close()

	opts := baseOptions(ts.URL())
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	s.Enqueue(NewRecord(time.Now(), slog.LevelInfo, "msg", nil))
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !s.Stats().Disabled {
		t.Fatalf("Stats().Disabled = false after 403, want true")
	}
	if got := ts.callCount(); got != 1 {
		t.Fatalf("callCount = %d, want 1", got)
	}

	// Further records must be drained & dropped without another request.
	s.Enqueue(NewRecord(time.Now(), slog.LevelInfo, "msg2", nil))
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := ts.callCount(); got != 1 {
		t.Fatalf("callCount after disable = %d, want still 1", got)
	}
	if s.Stats().Dropped == 0 {
		t.Errorf("expected the post-disable record to be counted as dropped")
	}
}

func TestShipper5xxRetriedThenSucceeds(t *testing.T) {
	ts := newTestServer(http.StatusInternalServerError, http.StatusAccepted)
	defer ts.Close()

	opts := baseOptions(ts.URL())
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	s.Enqueue(NewRecord(time.Now(), slog.LevelInfo, "msg", nil))
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if got := ts.callCount(); got != 2 {
		t.Fatalf("callCount = %d, want 2 (one failure + one retry)", got)
	}
	if s.Stats().Sent != 1 {
		t.Errorf("Stats().Sent = %d, want 1", s.Stats().Sent)
	}
	if s.Stats().Failed != 0 {
		t.Errorf("Stats().Failed = %d, want 0", s.Stats().Failed)
	}
}

func TestShipperDropOnFullCounterReportedOnNextRecord(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	opts := baseOptions(ts.URL())
	opts.QueueSize = 2
	s := NewShipper(opts) // not started yet: nothing drains the queue

	for i := 0; i < 5; i++ {
		s.Enqueue(NewRecord(time.Now(), slog.LevelInfo, "msg", nil))
	}
	if got := s.Stats().Dropped; got != 3 {
		t.Fatalf("Stats().Dropped = %d, want 3 (5 enqueued into a queue of 2)", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reqs, _ := ts.snapshot()
	if len(reqs) != 1 || len(reqs[0]) == 0 {
		t.Fatalf("unexpected requests: %#v", reqs)
	}
	if reqs[0][0].Dropped != 3 {
		t.Errorf("first shipped record Dropped = %d, want 3", reqs[0][0].Dropped)
	}
	for _, rec := range reqs[0][1:] {
		if rec.Dropped != 0 {
			t.Errorf("later record Dropped = %d, want 0", rec.Dropped)
		}
	}
}

func TestShipperStopFlushesAndIsIdempotent(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	opts := baseOptions(ts.URL())
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	s.Enqueue(NewRecord(time.Now(), slog.LevelInfo, "msg", nil))

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := s.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := ts.callCount(); got != 1 {
		t.Fatalf("callCount after Stop = %d, want 1", got)
	}

	// Idempotent: a second Stop must not block or error.
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestShipperHandleSkipAttrKeyIsIgnored(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	opts := baseOptions(ts.URL())
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	s.Handle(ctx, time.Now(), slog.LevelError, "must not ship", []slog.Attr{slog.Bool(SkipAttrKey, true)})
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := ts.callCount(); got != 0 {
		t.Fatalf("callCount = %d, want 0 (SkipAttrKey record must never ship)", got)
	}
}

func TestShipperHandleMinLevelFiltering(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	opts := baseOptions(ts.URL())
	opts.MinLevel = slog.LevelWarn
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	s.Handle(ctx, time.Now(), slog.LevelInfo, "info dropped", nil)
	s.Handle(ctx, time.Now(), slog.LevelError, "error kept", nil)
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reqs, _ := ts.snapshot()
	if len(reqs) != 1 || len(reqs[0]) != 1 {
		t.Fatalf("unexpected requests: %#v", reqs)
	}
	if reqs[0][0].Message != "error kept" {
		t.Errorf("shipped message = %q, want %q", reqs[0][0].Message, "error kept")
	}
}

func TestShipper413SplitsBatchOnce(t *testing.T) {
	ts := newTestServer(http.StatusRequestEntityTooLarge, http.StatusAccepted, http.StatusAccepted)
	defer ts.Close()

	opts := baseOptions(ts.URL())
	opts.BatchSize = 10
	s := NewShipper(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(context.Background())

	for i := 0; i < 4; i++ {
		s.Enqueue(NewRecord(time.Now(), slog.LevelInfo, "msg", nil))
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if got := ts.callCount(); got != 3 {
		t.Fatalf("callCount = %d, want 3 (1 rejected whole batch + 2 halves)", got)
	}
	if s.Stats().Sent != 4 {
		t.Errorf("Stats().Sent = %d, want 4", s.Stats().Sent)
	}
	if s.Stats().Dropped != 0 {
		t.Errorf("Stats().Dropped = %d, want 0", s.Stats().Dropped)
	}
}
