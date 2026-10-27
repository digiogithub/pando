package client

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSSEParserChunksCommentsAndCRLF(t *testing.T) {
	var p SSEParser
	if got := p.Push([]byte(": hi\n\nevent: a\ndata: {\"a\"")); len(got) != 0 {
		t.Fatalf("early frames: %+v", got)
	}
	got := p.Push([]byte(":1}\n\ndata:{\"b\":2}\r\n\r\n"))
	if len(got) != 2 {
		t.Fatalf("frames = %d, want 2", len(got))
	}
	if got[0].Event != "a" || got[0].Data != `{"a":1}` {
		t.Errorf("frame 0 = %+v", got[0])
	}
	if got[1].Event != "" || got[1].Data != `{"b":2}` {
		t.Errorf("frame 1 = %+v", got[1])
	}
}

func TestSSEParserMultilineIDAndTail(t *testing.T) {
	var p SSEParser
	got := p.Push([]byte("id: 7\nevent: x\ndata: l1\ndata: l2\n\ndata: tail"))
	if len(got) != 1 || got[0].Data != "l1\nl2" || got[0].ID != "7" || got[0].Event != "x" {
		t.Fatalf("got %+v", got)
	}
	tail := p.Finish()
	if len(tail) != 1 || tail[0].Data != "tail" || tail[0].Event != "" {
		t.Fatalf("tail %+v", tail)
	}
}

func TestSSEParserUTF8AndCRSplitAcrossChunks(t *testing.T) {
	raw := []byte("data: \U0001f9e0 ok\r\n\r\n")
	for cut := 1; cut < len(raw); cut++ {
		var p SSEParser
		got := append(p.Push(raw[:cut]), p.Push(raw[cut:])...)
		if len(got) != 1 || got[0].Data != "\U0001f9e0 ok" {
			t.Fatalf("cut %d: %+v", cut, got)
		}
	}
}

func TestSSEFixturesReplayInTinyChunks(t *testing.T) {
	for _, name := range []string{"chat_simple.sse", "chat_tool_read.sse", "session_reattach.sse", "chat_cancel.sse"} {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		var p SSEParser
		var names []string
		for i := 0; i < len(raw); i += 7 {
			for _, f := range p.Push(raw[i:min(i+7, len(raw))]) {
				names = append(names, f.Event)
			}
		}
		for _, f := range p.Finish() {
			names = append(names, f.Event)
		}
		if len(names) == 0 || names[0] != "session" || names[len(names)-1] != "done" {
			t.Errorf("%s: events %v", name, names)
		}
	}
}

func parseFixture(t *testing.T, name string) []StreamEvent {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var p SSEParser
	var out []StreamEvent
	for _, f := range append(p.Push(raw), p.Finish()...) {
		out = append(out, ParseStreamEvent(f))
	}
	return out
}

func TestParseStreamEventSynthetic(t *testing.T) {
	evs := parseFixture(t, "synthetic_events.sse")
	byType := map[string]StreamEvent{}
	for _, e := range evs {
		byType[e.Type] = e
	}
	if e := byType[EventRun]; e.RunSeq != 2 || e.SessionID != "synthetic-session" {
		t.Errorf("run = %+v", e)
	}
	if e := byType[EventPermissionRequest]; e.PermissionRequest == nil || e.PermissionRequest.ToolName != "bash" || !e.PermissionRequest.NeverAutoApprove {
		t.Errorf("permission = %+v", e.PermissionRequest)
	}
	if e := byType[EventQuestionRequest]; e.QuestionRequest == nil || len(e.QuestionRequest.Questions) != 1 || len(e.QuestionRequest.Questions[0].Options) != 2 {
		t.Errorf("question = %+v", e.QuestionRequest)
	}
	if e := byType[EventPlanUpdate]; len(e.Plan) != 2 || e.Plan[0].Status != "in_progress" {
		t.Errorf("plan = %+v", e.Plan)
	}
	if e := byType[EventTodosUpdate]; len(e.Todos) != 1 {
		t.Errorf("todos = %+v", e.Todos)
	}
	if e := byType[EventToolCall]; e.ToolCall == nil || e.ToolCall.Kind != "edit" || len(e.ToolCall.Locations) != 1 {
		t.Errorf("tool_call = %+v", e.ToolCall)
	}
	if e := byType[EventToolResult]; e.ToolResult == nil || e.ToolResult.Diff == nil || e.ToolResult.Diff.NewString != "b" {
		t.Errorf("tool_result = %+v", e.ToolResult)
	}
	if e := byType[EventSteeringInjected]; e.Message != "also add docs" {
		t.Errorf("steering = %+v", e)
	}
	if e := byType["some_future_event"]; len(e.Raw) == 0 {
		t.Errorf("unknown event lost its payload")
	}
	if e := byType[EventError]; e.Error == "" {
		t.Errorf("error event without text")
	}
}

func TestParseStreamEventRealChat(t *testing.T) {
	evs := parseFixture(t, "chat_simple.sse")
	var text strings.Builder
	var usage *TokenUsage
	routing := false
	for _, e := range evs {
		switch e.Type {
		case EventContentDelta:
			text.WriteString(e.Text)
		case EventTokenUsage:
			usage = e.TokenUsage
		case EventSystemMessage:
			routing = routing || e.Routing != nil
		case EventGoalStatus:
			if e.Goal != nil {
				t.Errorf("goal should be nil: %+v", e.Goal)
			}
		}
	}
	if text.String() != "pong" {
		t.Errorf("text = %q", text.String())
	}
	if usage == nil || usage.ContextWindow != 400000 {
		t.Errorf("usage = %+v", usage)
	}
	if !routing {
		t.Error("routing system_message not decoded")
	}
}

func TestDecodeSessionFixtures(t *testing.T) {
	var list SessionList
	raw, _ := os.ReadFile("testdata/sessions_list.json")
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 3 || list.Sessions[0].Title != "/help" || list.Sessions[0].RunSeq != 1 {
		t.Fatalf("list = %+v", list)
	}
	var d SessionDetail
	raw, _ = os.ReadFile("testdata/session_detail.json")
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	kinds := map[PartKind]int{}
	for _, m := range d.Messages {
		for _, p := range m.Parts {
			kinds[p.Kind]++
		}
	}
	if kinds[PartText] == 0 || kinds[PartToolCall] == 0 || kinds[PartToolResult] == 0 || kinds[PartFinish] == 0 {
		t.Errorf("part kinds = %v", kinds)
	}
	var models ModelList
	raw, _ = os.ReadFile("testdata/models.json")
	if err := json.Unmarshal(raw, &models); err != nil || len(models.Models) == 0 || models.Models[0].ID != "auto" {
		t.Errorf("models = %+v err %v", models, err)
	}
}

// fakeServer is a minimal Pando API: token endpoint with rotation, chat
// stream that drops before `done`, the reattach stream and /pending.
type fakeServer struct {
	mu        sync.Mutex
	token     string
	tokenHits int
	running   bool
	streams   int
	steers    int
}

func (f *fakeServer) auth(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get(HeaderToken) != f.token {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	if r.Header.Get(HeaderClient) != ClientName {
		http.Error(w, "missing client header", http.StatusBadRequest)
		return false
	}
	return true
}

func sse(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	w.(http.Flusher).Flush()
}

func (f *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.tokenHits++
		tok := f.token
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
	})
	mux.HandleFunc("POST /api/v1/chat/stream", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.running = true
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, "session", `{"sessionId":"s1","running":true}`)
		sse(w, "run", `{"sessionId":"s1","runSeq":3}`)
		sse(w, "content_delta", `{"text":"par"}`)
		// Body closes without done: the run goes on server side.
	})
	mux.HandleFunc("GET /api/v1/sessions/{id}/pending", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		running := f.running
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"running": running, "run_seq": 3})
	})
	mux.HandleFunc("GET /api/v1/sessions/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		f.streams++
		f.running = false
		f.mu.Unlock()
		sse(w, "session", `{"sessionId":"s1","running":true}`)
		sse(w, "run", `{"sessionId":"s1","runSeq":3}`)
		sse(w, "content_delta", `{"text":"par"}`)
		sse(w, "content_delta", `{"text":"tial"}`)
		sse(w, "done", `{}`)
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/steer", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		f.steers++
		f.mu.Unlock()
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_busy"})
	})
	mux.HandleFunc("GET /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		raw, _ := os.ReadFile("testdata/sessions_list.json")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	return mux
}

func TestChatStreamReattachesDroppedRun(t *testing.T) {
	f := &fakeServer{token: "t1"}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	var turn strings.Builder
	var types []string
	out, err := c.ChatStream(context.Background(), ChatRequest{Prompt: "hi"}, func(ev StreamEvent) {
		types = append(types, ev.Type)
		switch ev.Type {
		case EventReattach:
			turn.Reset() // the replay re-delivers the run from its start
		case EventContentDelta:
			turn.WriteString(ev.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Done || out.Reattached != 1 || out.SessionID != "s1" {
		t.Errorf("outcome = %+v", out)
	}
	if turn.String() != "partial" {
		t.Errorf("turn = %q (events %v)", turn.String(), types)
	}
	if f.streams != 1 {
		t.Errorf("reattach streams = %d", f.streams)
	}
}

func TestChatStreamDoesNotReattachFinishedRun(t *testing.T) {
	f := &fakeServer{token: "t1"}
	mux := http.NewServeMux()
	mux.Handle("/", f.handler())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/chat/stream" {
			// A slash command: answered without `done`, no run behind it.
			sse(w, "session", `{"sessionId":"s1","running":true}`)
			sse(w, "content_delta", `{"text":"help text"}`)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Token: "t1"})
	reattached := false
	out, err := c.ChatStream(context.Background(), ChatRequest{Prompt: "/help"}, func(ev StreamEvent) {
		reattached = reattached || ev.Type == EventReattach
	})
	if err != nil || out.Done || reattached || f.streams != 0 {
		t.Fatalf("out %+v err %v reattached %v streams %d", out, err, reattached, f.streams)
	}
}

func TestTokenRefreshOn401(t *testing.T) {
	f := &fakeServer{token: "new"}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Token: "stale"})
	list, err := c.ListSessions(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 3 {
		t.Errorf("sessions = %d", len(list.Sessions))
	}
	if tok, _ := c.Token(context.Background()); tok != "new" || f.tokenHits != 1 {
		t.Errorf("token %q hits %d", tok, f.tokenHits)
	}
	// Concurrent calls with a stale token refresh once.
	f.mu.Lock()
	f.token = "newer"
	f.mu.Unlock()
	var wg sync.WaitGroup
	var fails atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.ListSessions(context.Background(), 0, 0); err != nil {
				fails.Add(1)
			}
		}()
	}
	wg.Wait()
	if fails.Load() != 0 {
		t.Errorf("%d concurrent calls failed", fails.Load())
	}
	if f.tokenHits > 3 {
		t.Errorf("token fetched %d times for one rotation", f.tokenHits)
	}
}

func TestSteerNotBusy(t *testing.T) {
	f := &fakeServer{token: "t"}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL})
	if err := c.Steer(context.Background(), "s1", "more"); err != ErrNotBusy {
		t.Fatalf("err = %v, want ErrNotBusy", err)
	}
}

func TestHTTPSTrustsCAFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ServerInfo{Status: "healthy", Version: "v9"})
	}))
	defer srv.Close()
	ca := filepath.Join(t.TempDir(), "ca.crt")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{BaseURL: srv.URL, CAFile: ca})
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Health(context.Background())
	if err != nil || info.Version != "v9" {
		t.Fatalf("health %+v err %v", info, err)
	}
	// An explicit CA file that cannot be read is an error, not a silent fallback.
	c2, _ := New(Options{BaseURL: srv.URL, CAFile: filepath.Join(t.TempDir(), "missing.crt")})
	if c2 != nil {
		t.Fatal("missing explicit CA file should fail")
	}
}

func TestSubscribeSessionEventsReconnects(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/token" {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "t"})
			return
		}
		n := conns.Add(1)
		sse(w, "ready", `{}`)
		fmt.Fprint(w, ": keepalive\n\n")
		if n == 1 {
			sse(w, "session_upsert", `{"session":{"ID":"a","Title":"A","is_running":true,"run_seq":2}}`)
			return // drop: the subscriber reconnects
		}
		sse(w, "session_deleted", `{"id":"a"}`)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []SessionEvent
	err := c.SubscribeSessionEvents(ctx, func(ev SessionEvent) {
		got = append(got, ev)
		if ev.Type == "session_deleted" {
			cancel()
		}
	})
	if err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 4 || got[1].Session.Title != "A" || !got[1].Session.IsRunning || got[3].ID != "a" {
		t.Errorf("events = %+v", got)
	}
}
