package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func fixtureEvents(t *testing.T, name string) []client.StreamEvent {
	t.Helper()
	raw, err := os.ReadFile("../../client/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var p client.SSEParser
	var out []client.StreamEvent
	for _, f := range append(p.Push(raw), p.Finish()...) {
		out = append(out, client.ParseStreamEvent(f))
	}
	return out
}

func TestApplyToolRun(t *testing.T) {
	cv := newConversation("")
	cv.BeginRun("read hello.txt")
	for _, ev := range fixtureEvents(t, "chat_tool_read.sse") {
		cv.Apply(ev)
	}
	if cv.Running || cv.Streaming {
		t.Error("run not finished by done")
	}
	if cv.ID == "" {
		t.Error("session id not learned")
	}
	var tools, text int
	for _, it := range cv.Items {
		if it.Kind == ItemAssistant {
			tools += len(it.Tools())
			text += len(it.PlainText())
			if !it.Done {
				t.Error("assistant row still streaming after done")
			}
		}
	}
	if tools == 0 || text == 0 {
		t.Errorf("tools %d text %d; items %d", tools, text, len(cv.Items))
	}
	for _, it := range cv.Items {
		for _, tl := range it.Tools() {
			if tl.Status != "completed" || tl.Output == "" {
				t.Errorf("tool %s status %q output %d bytes", tl.Name, tl.Status, len(tl.Output))
			}
		}
	}
}

func TestApplyReattachDiscardsPartialTurn(t *testing.T) {
	cv := newConversation("s1")
	cv.BeginRun("hi")
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "par"})
	cv.Apply(client.StreamEvent{Type: client.EventSystemMessage, Text: "routing"})
	cv.Apply(client.StreamEvent{Type: client.EventReattach})
	if len(cv.Items) != 1 || cv.Items[0].Kind != ItemUser {
		t.Fatalf("items after reattach = %d", len(cv.Items))
	}
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "partial"})
	if reload := cv.Apply(client.StreamEvent{Type: client.EventDone}); reload {
		t.Error("run with content should not ask for a reload")
	}
	if cv.Items[1].PlainText() != "partial" || !cv.Items[1].Done {
		t.Errorf("assistant = %+v", cv.Items[1])
	}
	// A replay of a run already over brings no content: reload history.
	cv.BeginAttach()
	if !cv.Apply(client.StreamEvent{Type: client.EventDone}) {
		t.Error("empty replay should ask for a reload")
	}
}

func TestSteeringInjectedOpensNewTurn(t *testing.T) {
	cv := newConversation("s1")
	cv.BeginRun("hi")
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "a"})
	cv.Apply(client.StreamEvent{Type: client.EventSteeringInjected, Message: "also b"})
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "b"})
	kinds := []ItemKind{}
	for _, it := range cv.Items {
		kinds = append(kinds, it.Kind)
	}
	want := []ItemKind{ItemUser, ItemAssistant, ItemUser, ItemAssistant}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Errorf("kinds = %v want %v", kinds, want)
	}
}

func TestFoldHistory(t *testing.T) {
	raw, _ := os.ReadFile("../../client/testdata/session_detail.json")
	var d client.SessionDetail
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	items := FoldHistory(d.Messages)
	if len(items) == 0 || items[0].Kind != ItemUser {
		t.Fatalf("items = %d", len(items))
	}
	foundTool := false
	for _, it := range items {
		for _, tl := range it.Tools() {
			foundTool = true
			if tl.Output == "" {
				t.Errorf("tool %s has no folded result", tl.Name)
			}
		}
	}
	if !foundTool {
		t.Error("no tool call folded")
	}
}

// fakeAPI serves the endpoints the chat MVP uses.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	list, _ := os.ReadFile("../../client/testdata/sessions_list.json")
	detail, _ := os.ReadFile("../../client/testdata/session_detail.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token":"tok"}`)
	})
	mux.HandleFunc("GET /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) { w.Write(list) })
	mux.HandleFunc("GET /api/v1/sessions/events", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "event: ready\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("GET /api/v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) { w.Write(detail) })
	mux.HandleFunc("POST /api/v1/chat/stream", func(w http.ResponseWriter, r *http.Request) {
		var req client.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, line := range []string{
			`event: session` + "\n" + `data: {"sessionId":"new-1","running":true}`,
			`event: run` + "\n" + `data: {"sessionId":"new-1","runSeq":1}`,
			`event: content_delta` + "\n" + `data: {"text":"Hello **from** "}`,
			`event: content_delta` + "\n" + `data: {"text":"the fake server"}`,
			`event: done` + "\n" + `data: {}`,
		} {
			fmt.Fprint(w, line+"\n\n")
			w.(http.Flusher).Flush()
		}
	})
	return httptest.NewServer(mux)
}

func waitFor(t *testing.T, tt *ui.Tester, q *app.QueueDispatcher, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if q.Drain() > 0 {
			tt.Frame()
		}
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out; texts %q", tt.Texts())
}

func TestChatViewAgainstFakeServer(t *testing.T) {
	srv := fakeAPI(t)
	defer srv.Close()
	c, err := client.New(client.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	root := app.NewRoot(a)
	defer root.Close()
	tt := ui.NewTester(root.View, 1200, 800)

	// The sessions list loads.
	waitFor(t, tt, q, func() bool { return tt.HasText("pong") })
	if !tt.HasText("Use the read tool to read hello.txt and quote its single line. Nothing else.") && !tt.HasText("/help") {
		t.Errorf("sessions missing: %q", tt.Texts())
	}

	// A new chat streams a reply.
	tt.Type("hi there")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, q, func() bool { return tt.HasText("the fake server") || tt.HasText("Hello from the fake server") })
	if !tt.HasText("hi there") {
		t.Errorf("user prompt missing: %q", tt.Texts())
	}

	// Opening a session renders its history.
	if err := tt.Click("pong"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool {
		return strings.Contains(strings.Join(tt.Texts(), "\n"), "Use the read tool")
	})
}

func TestSpikeConversationRendersVirtualized(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, nil)
	ch := New(a)
	cv := SpikeConversation(1000)
	ch.convs[cv.ID] = cv
	ch.current = cv.ID
	ch.started = true
	tt := ui.NewTester(func(c *ui.Context) {
		ch.Build(c, nil)
	}, 1200, 800)
	start := time.Now()
	for range 20 {
		tt.Frame()
	}
	per := time.Since(start) / 20
	t.Logf("1000-row transcript: %v per frame (headless)", per)
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, "Answer 500") {
		t.Errorf("end of the transcript not shown (FollowEnd)")
	}
	if strings.Contains(texts, "Question 1:") {
		t.Errorf("rows far above the viewport were built: list not virtualized")
	}
	// Scroll up through the transcript: every frame lays out new rows
	// (markdown parse + text layout), the worst case of the spike.
	start = time.Now()
	const steps = 60
	for range steps {
		tt.Scroll(600, 400, 0, -400)
	}
	t.Logf("1000-row transcript: %v per scrolled frame (headless, 400 DIP steps)", time.Since(start)/steps)
	if cv.List.AtEnd() {
		t.Error("scrolling did not leave the end")
	}
}

// TestComposerInputMethod checks dead-key and IME composition in the
// composer: Enter while composing belongs to the input method and must not
// send, and committed text lands in the draft.
func TestComposerInputMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, nil)
	ch := New(a)
	ch.started = true
	tt := ui.NewTester(func(c *ui.Context) { ch.Build(c, nil) }, 1000, 700)
	// Spanish dead key: ´ then a.
	tt.Compose("´", 1)
	tt.Key(0, ui.KeyEnter) // the input method's, not a send
	tt.Compose("", 0)
	tt.Type("á")
	tt.Type(" ñ ")
	// Japanese IME: preedit then commit.
	tt.Compose("にほん", 3)
	tt.Compose("", 0)
	tt.Type("日本")
	if ch.draft != "á ñ 日本" {
		t.Fatalf("draft = %q", ch.draft)
	}
	if len(ch.conv().Items) != 0 {
		t.Error("Enter during composition sent the draft")
	}
}
