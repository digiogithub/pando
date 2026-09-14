package agui

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// -------------------------------------------------------- PANDO-US-0022
// graceful drain on shutdown.

// waitForRegisteredRun polls r.runs for threadID, the same pattern
// TestPostWithNoNewMessageReattachesToALiveRun and the PANDO-US-0021 tests
// use to synchronize with handleRun's own goroutine without a fixed sleep.
func waitForRegisteredRun(t *testing.T, r *Runtime, threadID string) *activeRun {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if run, ok := r.runs.get(threadID); ok {
			return run
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("run for thread %q never registered", threadID)
	return nil
}

// TestDrainingRejectsNewRunsWithoutOpeningAStream is the PANDO-US-0022
// acceptance criterion: while draining, a new run POST gets 503 +
// Retry-After and no stream -- it reuses the exact PANDO-US-0021 rejection
// path, so no RUN_ERROR is emitted either.
func TestDrainingRejectsNewRunsWithoutOpeningAStream(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	r.StartDraining()

	rec := httptest.NewRecorder()
	r.handleRun(rec, newRunRequest("t-drain", "r1", "hello"))

	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Fatal("missing Retry-After header")
	}
	if strings.Contains(rec.Body.String(), "RUN_ERROR") {
		t.Fatal("a draining rejection must not emit RUN_ERROR: no run was started")
	}
}

// TestCloseWaitsForInFlightRunWithinGrace is the PANDO-US-0022 acceptance
// criterion: a run in flight when shutdown begins is allowed to finish
// within ShutdownGrace rather than being cut off -- the fake agent service
// here stands in for the real internal/llm/agent, which is what actually
// persists messages to the store as it streams (see internal/llm/agent.go's
// a.messages.Update calls); allowing the run to reach RUN_FINISHED, as this
// test asserts, is what lets that persistence complete undisturbed.
func TestCloseWaitsForInFlightRunWithinGrace(t *testing.T) {
	// No real DB is needed for this test, and opening one would add its own
	// background goroutine (database/sql's connectionOpener) that outlives
	// this test body -- newThreadTestRuntime's threadStore already degrades
	// cleanly to in-memory-only with a nil DB.
	r, _, _ := newThreadTestRuntime(t, nil)
	r.cfg.AgentPoolSize = defaultPoolSize
	r.cfg.AgentPoolTTL = defaultPoolTTL
	r.cfg.ShutdownGrace = 2 * time.Second
	r.pool = newAgentPool(r.deps, r.cfg, r.perms, nil, r.pending)

	svc := newFakeAgentService()
	svc.runDelay = 50 * time.Millisecond
	r.pool.entries["coder"] = &poolEntry{svc: svc, lastUsed: time.Now()}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		r.handleRun(rec, newRunRequest("shutdown-thread", "run-1", "go"))
		done <- rec
	}()
	waitForRegisteredRun(t, r, "shutdown-thread")

	r.Close()

	select {
	case rec := <-done:
		if !strings.Contains(rec.Body.String(), `"outcome":"success"`) {
			t.Fatalf("a run within the shutdown grace must finish normally: %s", rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handleRun never returned")
	}
}

// TestCloseCancelsRunsThatExceedGraceAndCountsThem is the PANDO-US-0022
// acceptance criterion: a run that outlives the grace is cancelled, and
// Close does not block forever waiting for it.
func TestCloseCancelsRunsThatExceedGraceAndCountsThem(t *testing.T) {
	// No real DB is needed for this test, and opening one would add its own
	// background goroutine (database/sql's connectionOpener) that outlives
	// this test body -- newThreadTestRuntime's threadStore already degrades
	// cleanly to in-memory-only with a nil DB.
	r, _, _ := newThreadTestRuntime(t, nil)
	r.cfg.AgentPoolSize = defaultPoolSize
	r.cfg.AgentPoolTTL = defaultPoolTTL
	r.cfg.ShutdownGrace = 20 * time.Millisecond
	r.pool = newAgentPool(r.deps, r.cfg, r.perms, nil, r.pending)

	svc := newFakeAgentService()
	svc.runDelay = time.Hour // never finishes on its own inside this test
	r.pool.entries["coder"] = &poolEntry{svc: svc, lastUsed: time.Now()}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		r.handleRun(rec, newRunRequest("stuck-thread", "run-1", "go"))
		done <- rec
	}()
	waitForRegisteredRun(t, r, "stuck-thread")

	closeReturned := make(chan struct{})
	go func() {
		r.Close()
		close(closeReturned)
	}()
	select {
	case <-closeReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("Close must not block forever past its grace period")
	}

	select {
	case rec := <-done:
		if !strings.Contains(rec.Body.String(), `"type":"RUN_ERROR"`) {
			t.Fatalf("a run cut off by shutdown must end in RUN_ERROR: %s", rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handleRun never returned after Close cancelled the stuck run")
	}
}

// TestCloseReleasesSuspendedRunsImmediately is the PANDO-US-0022 acceptance
// criterion: a suspended run (parked on a permission/frontend-tool prompt)
// is never waited on, even with a huge grace -- it is checkpointed and
// released right away, since nothing is going to answer a human-in-the-loop
// prompt during a shutdown window.
func TestCloseReleasesSuspendedRunsImmediately(t *testing.T) {
	// No real DB is needed for this test, and opening one would add its own
	// background goroutine (database/sql's connectionOpener) that outlives
	// this test body -- newThreadTestRuntime's threadStore already degrades
	// cleanly to in-memory-only with a nil DB.
	r, _, _ := newThreadTestRuntime(t, nil)
	r.cfg.AgentPoolSize = defaultPoolSize
	r.cfg.AgentPoolTTL = defaultPoolTTL
	r.cfg.ShutdownGrace = time.Hour // deliberately huge
	r.pool = newAgentPool(r.deps, r.cfg, r.perms, nil, r.pending)

	svc := newFakeAgentService()
	svc.suspendCall = "call-1"
	svc.resumeSignal = make(chan struct{}) // never closed
	r.pool.entries["coder"] = &poolEntry{svc: svc, lastUsed: time.Now()}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		r.handleRun(rec, newRunRequest("susp-thread", "run-1", "go"))
		done <- rec
	}()
	run := waitForRegisteredRun(t, r, "susp-thread")
	r.pending.register(run.sessionID, suspension{callID: "call-1"})

	select {
	case rec := <-done:
		if !strings.Contains(rec.Body.String(), `"outcome":"interrupt"`) {
			t.Fatalf("expected the segment to end in an interrupt: %s", rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the interrupted segment never returned")
	}

	closeReturned := make(chan struct{})
	go func() {
		r.Close()
		close(closeReturned)
	}()
	select {
	case <-closeReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Close must not wait on a suspended run even with a huge ShutdownGrace")
	}
	if _, ok := r.runs.get("susp-thread"); ok {
		t.Fatal("a suspended run must be released on Close, not left registered")
	}
}

// TestCloseNoGoroutineLeak is the PANDO-US-0022 acceptance criterion mirrored
// on PANDO-US-0017's TestParkedRunExpiryLeavesNoGoroutine: after Close cuts
// off a run that exceeded its grace, the pump goroutine (and the fake
// agent's own goroutine, standing in for the real provider call) must both
// have exited -- not be left blocked forever.
func TestCloseNoGoroutineLeak(t *testing.T) {
	before := goruntime.NumGoroutine()

	// No real DB is needed for this test, and opening one would add its own
	// background goroutine (database/sql's connectionOpener) that outlives
	// this test body -- newThreadTestRuntime's threadStore already degrades
	// cleanly to in-memory-only with a nil DB.
	r, _, _ := newThreadTestRuntime(t, nil)
	r.cfg.AgentPoolSize = defaultPoolSize
	r.cfg.AgentPoolTTL = defaultPoolTTL
	r.cfg.ShutdownGrace = 20 * time.Millisecond
	r.pool = newAgentPool(r.deps, r.cfg, r.perms, nil, r.pending)

	svc := newFakeAgentService()
	svc.runDelay = time.Hour
	r.pool.entries["coder"] = &poolEntry{svc: svc, lastUsed: time.Now()}

	handlerDone := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		r.handleRun(rec, newRunRequest("leak-thread", "run-1", "go"))
		close(handlerDone)
	}()
	waitForRegisteredRun(t, r, "leak-thread")

	r.Close()

	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("handleRun never returned")
	}

	deadline := time.Now().Add(2 * time.Second)
	for goruntime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := goruntime.NumGoroutine(); got > before {
		t.Fatalf("goroutine count grew from %d to %d after Close", before, got)
	}
}

// -------------------------------------------------------- PANDO-US-0023
// the token must never reach a log line, including the startup config dump.

// TestNewNeverLogsTheToken is the PANDO-US-0023 acceptance criterion applied
// to internal/agui/runtime.go's own startup log line (New's "AG-UI adapter
// ready", which dumps the resolved config): it must not carry Deps.Token or
// any config field derived from it, at any log level.
func TestNewNeverLogsTheToken(t *testing.T) {
	const secret = "super-secret-agui-token-0023"

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := testConfig()
	r, err := New(Deps{
		Sessions: newFakeSessionService(),
		Messages: newFakeMessageService(),
		Token:    secret,
	}, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(r.Close)

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("the token leaked into a log line: %s", buf.String())
	}
}
