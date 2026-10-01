package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
)

func finishedRun(context.Context) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}

func TestRunSeqIncrementsPerRun(t *testing.T) {
	m := NewBackgroundSessionManager()
	if got := m.RunSeq("s1"); got != 0 {
		t.Fatalf("RunSeq before any run = %d, want 0", got)
	}
	for want := uint64(1); want <= 3; want++ {
		if err := m.Submit("s1", finishedRun); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if got := m.RunSeq("s1"); got != want {
			t.Fatalf("RunSeq = %d, want %d", got, want)
		}
		waitUntil(t, func() bool { return !m.IsBusy("s1") })
	}
	if got := m.RunSeq("other"); got != 0 {
		t.Fatalf("RunSeq of an unrelated session = %d, want 0", got)
	}
}

func TestRunSeqNotBumpedByBusyRejection(t *testing.T) {
	m := NewBackgroundSessionManager()
	block := make(chan agent.AgentEvent)
	if err := m.Submit("s1", func(context.Context) (<-chan agent.AgentEvent, error) { return block, nil }); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := m.Submit("s1", finishedRun); err != agent.ErrSessionBusy {
		t.Fatalf("second submit err = %v, want ErrSessionBusy", err)
	}
	if got := m.RunSeq("s1"); got != 1 {
		t.Fatalf("RunSeq = %d, want 1", got)
	}
	close(block)
}

func TestRunSeqSurvivesGC(t *testing.T) {
	m := NewBackgroundSessionManager()
	if err := m.Submit("s1", finishedRun); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitUntil(t, func() bool { return !m.IsBusy("s1") })

	// Age the finished run past its TTL and collect it.
	m.mu.Lock()
	s := m.sessions["s1"]
	m.mu.Unlock()
	s.mu.Lock()
	s.doneAt = time.Now().Add(-2 * bgSessionTTL)
	s.mu.Unlock()
	m.gc()
	if m.IsRunning("s1") {
		t.Fatal("test setup: finished run should have been collected")
	}
	if got := m.RunSeq("s1"); got != 1 {
		t.Fatalf("RunSeq after GC = %d, want 1", got)
	}
	if err := m.Submit("s1", finishedRun); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got := m.RunSeq("s1"); got != 2 {
		t.Fatalf("RunSeq after GC and a new run = %d, want 2", got)
	}
}

func TestPendingExposesRunSeq(t *testing.T) {
	s := newResumeTestServer(newResumeFakeAgent())
	pending := func() map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/s1/pending", nil)
		req.SetPathValue("id", "s1")
		rec := httptest.NewRecorder()
		s.handleSessionPending(rec, req)
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body
	}
	if got := pending()["run_seq"]; got != float64(0) {
		t.Fatalf("run_seq before a run = %v, want 0", got)
	}
	if err := s.bgRunner.Submit("s1", finishedRun); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitUntil(t, func() bool { return !s.sessionRunning("s1") })
	// The run is already over, yet a client that never saw it can still tell.
	body := pending()
	if body["run_seq"] != float64(1) || body["running"] != false {
		t.Fatalf("pending = %v, want run_seq 1 and running false", body)
	}
}
