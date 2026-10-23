package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/userinput"
)

// busyElsewhereServer returns a server whose app reports "sess-ext" as run by
// another process.
func busyElsewhereServer(ag agent.Service) (*Server, *app.App) {
	a := &app.App{CoderAgent: ag}
	a.RecordExternalRuns("follower-1", []string{"sess-ext"})
	return &Server{app: a, bgRunner: NewBackgroundSessionManager()}, a
}

func TestChatRejectsSessionBusyElsewhere(t *testing.T) {
	s, _ := busyElsewhereServer(newSteerMockAgent())

	for _, tc := range []struct {
		name    string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"chat", "/api/v1/chat", s.handleChat},
		{"chat stream", "/api/v1/chat/stream", s.handleChatStream},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(`{"sessionId":"sess-ext","prompt":"hi"}`))
		rec := httptest.NewRecorder()
		tc.handler(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s: status = %d, want 409 (body %s)", tc.name, rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		if resp["code"] != "busy_elsewhere" {
			t.Fatalf("%s: code = %q, want busy_elsewhere", tc.name, resp["code"])
		}
	}

	ag := newSteerMockAgent()
	s, _ = busyElsewhereServer(ag)
	if rec := doSteer(t, s, "sess-ext", `{"prompt":"hello"}`); rec.Code != http.StatusConflict {
		t.Fatalf("steer: status = %d, want 409", rec.Code)
	}
	if len(ag.steerCalls) != 0 {
		t.Fatalf("steer must not reach the agent: %v", ag.steerCalls)
	}
	// Other sessions are unaffected.
	if rec := doSteer(t, s, "sess-local", `{"prompt":"hello"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("steer local: status = %d, want 202", rec.Code)
	}
}

func TestPendingReportsBusyElsewhere(t *testing.T) {
	s, a := busyElsewhereServer(newSteerMockAgent())
	a.Permissions = permission.NewPermissionService()
	a.UserInput = userinput.NewService()
	for id, want := range map[string]bool{"sess-ext": true, "sess-local": false} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+id+"/pending", nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		s.handleSessionPending(rec, req)
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["busy_elsewhere"] != want {
			t.Fatalf("%s: busy_elsewhere = %v, want %v", id, resp["busy_elsewhere"], want)
		}
	}
}

// flushRecorder is a ResponseWriter + Flusher safe to read while a streaming
// handler writes from another goroutine.
type flushRecorder struct {
	mu     sync.Mutex
	header http.Header
	buf    bytes.Buffer
}

func newFlushRecorder() *flushRecorder { return &flushRecorder{header: http.Header{}} }

func (r *flushRecorder) Header() http.Header { return r.header }
func (r *flushRecorder) WriteHeader(int)     {}
func (r *flushRecorder) Flush()              {}
func (r *flushRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}
func (r *flushRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// waitFor polls rec until it contains want.
func waitFor(t *testing.T, rec *flushRecorder, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(rec.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in %q", want, rec.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sseEvents parses an SSE body into (event, data) pairs.
func sseEvents(t *testing.T, body string) []struct {
	Event string
	Data  map[string]any
} {
	t.Helper()
	var out []struct {
		Event string
		Data  map[string]any
	}
	for _, block := range strings.Split(body, "\n\n") {
		var ev struct {
			Event string
			Data  map[string]any
		}
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev.Data); err != nil {
					t.Fatalf("bad data %q: %v", line, err)
				}
			}
		}
		if ev.Event != "" {
			out = append(out, ev)
		}
	}
	return out
}

type msgPublisher interface {
	Publish(pubsub.EventType, message.Message)
}
type sessPublisher interface {
	Publish(pubsub.EventType, session.Session)
}

func TestSessionWatchPushesCoalescedMessagesUntilIdle(t *testing.T) {
	a := &app.App{
		Sessions: session.NewService(nil),
		Messages: message.NewService(nil),
	}
	a.RecordExternalRuns("follower-1", []string{"sess-ext"})
	s := &Server{app: a, bgRunner: NewBackgroundSessionManager()}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-ext/watch", nil).WithContext(ctx)
	req.SetPathValue("id", "sess-ext")
	rec := newFlushRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSessionWatch(rec, req)
	}()
	waitFor(t, rec, "event: session")

	msgs := a.Messages.(msgPublisher)
	// Streaming deltas of one message inside one window: only the latest
	// state is pushed, as "created" content (the full message).
	msgs.Publish(pubsub.CreatedEvent, message.Message{ID: "m1", SessionID: "sess-ext", Role: message.Assistant})
	for _, text := range []string{"he", "hel", "hello"} {
		msgs.Publish(pubsub.UpdatedEvent, message.Message{
			ID: "m1", SessionID: "sess-ext", Role: message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: text}},
		})
	}
	msgs.Publish(pubsub.UpdatedEvent, message.Message{ID: "other", SessionID: "other-session"})
	msgs.Publish(pubsub.DeletedEvent, message.Message{ID: "m0", SessionID: "sess-ext"})
	a.Sessions.(sessPublisher).Publish(pubsub.UpdatedEvent, session.Session{ID: "sess-ext", Title: "subagent", MessageCount: 2})
	waitFor(t, rec, "event: session_update")

	a.RecordExternalRuns("follower-1", nil)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("watch did not end after the external run finished")
	}

	var messages, deletes, updates int
	var last map[string]any
	for _, ev := range sseEvents(t, rec.String()) {
		switch ev.Event {
		case "message":
			messages++
			m := ev.Data["message"].(map[string]any)
			if m["ID"] != "m1" {
				t.Fatalf("unexpected message %v", m)
			}
			parts := m["Parts"].([]any)
			if parts[0].(map[string]any)["text"] != "hello" {
				t.Fatalf("pushed stale content: %v", parts)
			}
		case "message_deleted":
			deletes++
			if ev.Data["id"] != "m0" {
				t.Fatalf("deleted id = %v", ev.Data["id"])
			}
		case "session_update":
			updates++
			last = ev.Data["session"].(map[string]any)
		}
	}
	if messages != 1 || deletes != 1 || updates < 1 {
		t.Fatalf("messages=%d deletes=%d updates=%d, want 1/1/>=1:\n%s", messages, deletes, updates, rec.String())
	}
	if last["busy_elsewhere"] != false {
		t.Fatalf("final session_update must report busy_elsewhere=false, got %v", last["busy_elsewhere"])
	}
	if !strings.Contains(rec.String(), "event: done") {
		t.Fatalf("missing done")
	}
}

func TestSessionEventsPushesListChanges(t *testing.T) {
	a := &app.App{
		Sessions: session.NewService(nil),
		Messages: message.NewService(nil),
	}
	a.RecordExternalRuns("follower-1", []string{"s-new"})
	s := &Server{app: a, bgRunner: NewBackgroundSessionManager()}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/events", nil).WithContext(ctx)
	rec := newFlushRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSessionEvents(rec, req)
	}()
	waitFor(t, rec, "event: ready")

	pub := a.Sessions.(sessPublisher)
	pub.Publish(pubsub.CreatedEvent, session.Session{ID: "s-new", Title: "draft", MessageCount: 1})
	pub.Publish(pubsub.UpdatedEvent, session.Session{ID: "s-new", Title: "subagent work", MessageCount: 3})
	pub.Publish(pubsub.CreatedEvent, session.Session{ID: "title-x", ParentSessionID: "x", MessageCount: 1})
	pub.Publish(pubsub.CreatedEvent, session.Session{ID: "empty", MessageCount: 0})
	pub.Publish(pubsub.DeletedEvent, session.Session{ID: "gone"})
	waitFor(t, rec, "event: session_deleted")
	cancel()
	<-done

	var upserts []map[string]any
	for _, ev := range sseEvents(t, rec.String()) {
		if ev.Event == "session_upsert" {
			upserts = append(upserts, ev.Data["session"].(map[string]any))
		}
	}
	if len(upserts) != 1 {
		t.Fatalf("upserts = %v, want only s-new (child and empty sessions are not listed)", upserts)
	}
	if upserts[0]["ID"] != "s-new" || upserts[0]["Title"] != "subagent work" || upserts[0]["busy_elsewhere"] != true {
		t.Fatalf("upsert = %v", upserts[0])
	}
}

func TestSessionWatchIdleSessionEndsImmediately(t *testing.T) {
	s := &Server{app: &app.App{}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/x/watch", nil)
	req.SetPathValue("id", "x")
	rec := newFlushRecorder()
	s.handleSessionWatch(rec, req)
	if out := rec.String(); !strings.Contains(out, `"busyElsewhere":false`) || !strings.Contains(out, "event: done") {
		t.Fatalf("unexpected output %q", out)
	}
}
