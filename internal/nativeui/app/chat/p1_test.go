package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func TestToolClassification(t *testing.T) {
	cases := []struct {
		name, input, kind string
		class             Class
		cat               Category
		summary           string
	}{
		{"bash", `{"command":"go test ./...\nmore"}`, "", ClassExec, CatCommands, "go test ./..."},
		{"read", `{"file_path":"/a/b/c/main.go"}`, "", ClassRead, CatRead, "c/main.go"},
		{"edit", `{"file_path":"/a/b.go","old_string":"x","new_string":"y"}`, "", ClassEdit, CatEdited, "a/b.go"},
		{"write", `{"file_path":"/a/b.go","content":"x"}`, "", ClassWrite, CatEdited, "a/b.go"},
		{"grep", `{"pattern":"TODO"}`, "", ClassSearch, CatSearched, "TODO"},
		{"fetch", `{"url":"https://x.dev"}`, "", ClassFetch, CatFetched, "https://x.dev"},
		{"code_hybrid_search", `{"query":"q"}`, "", ClassSearch, CatSearched, "q"},
		{"mystery", `{"a":1}`, "", ClassJSON, CatTools, ""},
		{"mystery", `{}`, "execute", ClassExec, CatCommands, ""},
		{"mystery", `{}`, "read", ClassRead, CatRead, ""},
	}
	for _, c := range cases {
		m := MetaOf(&Tool{Name: c.name, Input: c.input, Kind: c.kind})
		if m.Class != c.class || m.Category != c.cat || m.Summary != c.summary {
			t.Errorf("%s/%s: class %v cat %v summary %q", c.name, c.kind, m.Class, m.Category, m.Summary)
		}
	}
	// A server title wins over the local summary.
	if m := MetaOf(&Tool{Name: "bash", Input: `{"command":"ls"}`, Title: "List files"}); m.Summary != "List files" {
		t.Errorf("title lost: %q", m.Summary)
	}
}

func TestBlocksFoldConsecutiveCalls(t *testing.T) {
	mk := func(k PartKind, name string) *Part {
		p := &Part{Kind: k, Text: "x"}
		if k == PTool {
			p.Tool = &Tool{Name: name, Input: `{}`, Status: "completed"}
		}
		return p
	}
	parts := []*Part{mk(PText, ""), mk(PTool, "bash"), mk(PTool, "bash"), mk(PTool, "read"), mk(PThinking, ""), mk(PText, ""), mk(PTool, "edit")}
	bl := Blocks(parts)
	if len(bl) != 4 || bl[0].Text == nil || len(bl[1].Events) != 4 || bl[2].Text == nil || len(bl[3].Events) != 1 {
		t.Fatalf("blocks = %+v", bl)
	}
	sum, fails, live := GroupSummary(bl[1].Events, app.NewContext(context.Background(), nil).T)
	if sum != "ran 2 commands · read 1 file · thought" || fails != 0 || live != nil {
		t.Errorf("summary = %q %d %v", sum, fails, live)
	}
}

func TestStreamKeepsTextToolOrder(t *testing.T) {
	cv := newConversation("s")
	cv.BeginRun("go")
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "before "})
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "tool"})
	cv.Apply(client.StreamEvent{Type: client.EventToolCall, ToolCall: &client.ToolCallEvent{ID: "t1", Name: "bash", Input: `{"command":"ls"}`}})
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "after"})
	a := cv.Items[1]
	if len(a.Parts) != 3 || a.Parts[0].Kind != PText || a.Parts[0].Text != "before tool" || a.Parts[1].Kind != PTool || a.Parts[2].Text != "after" {
		t.Fatalf("parts = %+v", a.Parts)
	}
}

func TestFoldHistoryMergesAssistantSteps(t *testing.T) {
	msgs := []client.Message{
		{Role: client.RoleUser, Parts: []client.Part{{Kind: client.PartText, Text: "hi"}}},
		{Role: client.RoleAssistant, Parts: []client.Part{{Kind: client.PartText, Text: "a"}}},
		{Role: client.RoleAssistant, Parts: []client.Part{{Kind: client.PartText, Text: "b"}}},
		{Role: client.RoleSystem, Parts: []client.Part{{Kind: client.PartText, Text: "persona prompt"}}},
	}
	items := FoldHistory(msgs)
	if len(items) != 3 || items[1].Kind != ItemAssistant || len(items[1].Parts) != 2 || items[2].Kind != ItemPersona {
		t.Fatalf("items = %d %+v", len(items), items)
	}
}

func TestSystemMessageRoutingAndFilter(t *testing.T) {
	cv := newConversation("s")
	cv.BeginRun("hi")
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "x"})
	p := 0.93
	raw, _ := json.Marshal(client.RoutingInfo{Kind: "routed", Model: "m1", RouteID: "r", Probability: &p})
	cv.Apply(client.StreamEvent{Type: client.EventSystemMessage, Routing: raw})
	raw, _ = json.Marshal(client.ContextFilterInfo{Kept: 4, Dropped: 5})
	cv.Apply(client.StreamEvent{Type: client.EventSystemMessage, ContextFilter: raw})
	kinds := []ItemKind{}
	for _, it := range cv.Items {
		kinds = append(kinds, it.Kind)
	}
	// The muted rows land before the assistant turn being streamed.
	if fmt.Sprint(kinds) != fmt.Sprint([]ItemKind{ItemUser, ItemRouting, ItemContextFilter, ItemAssistant}) {
		t.Fatalf("kinds = %v", kinds)
	}
	if got := routingLabel(cv.Items[1].Routing, ""); got != "Auto → m1 · r · p=0.93" {
		t.Errorf("label = %q", got)
	}
}

func TestPlanGoalAndFileChanges(t *testing.T) {
	cv := newConversation("s")
	cv.BeginRun("x")
	cv.Apply(client.StreamEvent{Type: client.EventPlanUpdate, Plan: []client.PlanEntry{{Title: "a", Status: "completed"}, {Title: "b", Status: "in_progress"}}})
	cv.Apply(client.StreamEvent{Type: client.EventGoalStatus, Goal: &client.GoalStatus{Objective: "ship", Status: "running"}})
	diff := &client.ToolDiff{FilePath: "/w/a.go", OldString: "a\nb", NewString: "a\nb\nc"}
	cv.Apply(client.StreamEvent{Type: client.EventToolResult, ToolResult: &client.ToolResultEvent{ToolCallID: "t", Name: "edit", Diff: diff}})
	cv.Apply(client.StreamEvent{Type: client.EventToolResult, ToolResult: &client.ToolResultEvent{ToolCallID: "t2", Name: "patch",
		RawOutput: json.RawMessage(`{"metadata":{"files_changed":["/w/b.go"]}}`)}})
	if len(cv.Plan) != 2 || cv.Goal.Objective != "ship" {
		t.Errorf("plan %d goal %+v", len(cv.Plan), cv.Goal)
	}
	fl := cv.FileList()
	if len(fl) != 2 || fl[1].Path != "/w/a.go" || fl[1].Additions != 3 || fl[1].Removals != 2 {
		t.Errorf("files = %+v", fl)
	}
	cv.MergeVCS([]client.VCSDiffEntry{{Path: "/w/a.go"}, {Path: "c.txt"}})
	if len(cv.Files) != 3 || !cv.Files["c.txt"].FromVCS {
		t.Errorf("vcs merge: %d", len(cv.Files))
	}
}

func TestRevertEditsAndSnippets(t *testing.T) {
	cur := "head\nNEW\ntail"
	if got := revertEdits(cur, []Edit{{"OLD", "NEW"}}); got != "head\nOLD\ntail" {
		t.Errorf("revert = %q", got)
	}
	o, n := snippetsDiff(FileChange{Edits: []Edit{{"a", "b"}, {"b", "c"}}})
	if o != "a" || n != "c" {
		t.Errorf("snippets = %q %q", o, n)
	}
	if relPath("/w/p/a.go", "/w/p") != "p/a.go" && relPath("/w/p/a.go", "/w/p") != "a.go" {
		t.Error("relPath")
	}
	if relPath("/etc/x", "/w") != "" {
		t.Error("outside path should have no relative form")
	}
}

func TestPendingMergeFailClosedQueue(t *testing.T) {
	cv := newConversation("s")
	cv.AddPermission(client.PermissionRequest{ID: "p1"})
	cv.AddPermission(client.PermissionRequest{ID: "p1"}) // replayed
	cv.AddQuestion(client.QuestionRequest{ID: "q1", Questions: []client.Question{{ID: "a"}}})
	if len(cv.Permissions) != 1 || len(cv.Questions) != 1 {
		t.Fatal("dedupe failed")
	}
	cv.Resolve("p1")
	cv.AddPermission(client.PermissionRequest{ID: "p1"}) // replayed after the answer
	if len(cv.Permissions) != 0 {
		t.Error("an answered request reopened")
	}
	// The server no longer lists q1: it was answered elsewhere.
	cv.MergePending(client.Pending{})
	if len(cv.Questions) != 0 {
		t.Error("stale question kept")
	}
}

func TestMentionToken(t *testing.T) {
	cases := []struct {
		in    string
		query string
		start int
	}{
		{"look at @mai", "mai", 8}, {"@x", "x", 0}, {"mail a@b.c", "", -1}, {"@a b", "", -1}, {"none", "", -1},
	}
	for _, c := range cases {
		q, s := mentionToken(c.in)
		if q != c.query || s != c.start {
			t.Errorf("%q: %q %d", c.in, q, s)
		}
	}
}

func TestSessionBuckets(t *testing.T) {
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.Local)
	for ts, want := range map[time.Time]string{
		now.Add(-time.Hour):           "Today",
		now.Add(-26 * time.Hour):      "Yesterday",
		now.Add(-4 * 24 * time.Hour):  "Previous 7 days",
		now.Add(-20 * 24 * time.Hour): "Previous 30 days",
		now.Add(-90 * 24 * time.Hour): "Older",
	} {
		if got := bucketOf(now, ts); got != want {
			t.Errorf("%v: %s want %s", ts, got, want)
		}
	}
	if !matchesSearch(client.SessionSummary{Session: client.Session{Title: "Fix Bug"}}, "bug") || matchesSearch(client.SessionSummary{}, "zzz") {
		t.Error("search")
	}
}

func TestFormatModel(t *testing.T) {
	for in, want := range map[string]string{
		"claude-sonnet-4-6": "Claude Sonnet 4.6", "copilot.gpt-4o": "Copilot GPT-4o", "gemini-2.5": "Gemini 2.5",
		"ollama/llama3": "llama3", "": "",
	} {
		if got := formatModel(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
}

// api records the calls the dialogs make.
type api struct {
	mu    sync.Mutex
	calls []string
	srv   *httptest.Server
}

func (a *api) log(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.calls = append(a.calls, r.Method+" "+r.URL.Path+" "+string(b))
	a.mu.Unlock()
}

func (a *api) called(sub string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func newAPI(t *testing.T) *api {
	a := &api{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"token":"tok"}`) })
	mux.HandleFunc("GET /api/v1/commands", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"goal","description":"Start goal mode","acceptsArgs":true},{"name":"help","description":"Help","acceptsArgs":false}]`)
	})
	mux.HandleFunc("GET /api/v1/files/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[{"name":"main.go","path":"cmd/main.go"}]}`)
	})
	mux.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"default_model":"claude-sonnet-4-6"}`) })
	mux.HandleFunc("GET /api/v1/project", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"cwd":"/work/proj"}`) })
	mux.HandleFunc("GET /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"sessions":[],"total":0}`) })
	mux.HandleFunc("GET /api/v1/sessions/events", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "event: ready\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("GET /api/v1/personas", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"personas":["architect"]}`) })
	mux.HandleFunc("GET /api/v1/personas/active", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"active":"","auto":true}`) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { a.log(r); fmt.Fprint(w, `{}`) })
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

type harness struct {
	ch *Chat
	tt *ui.Tester
	q  *app.QueueDispatcher
	a  *api
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	a := newAPI(t)
	c, err := client.New(client.Options{BaseURL: a.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ac := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	ac.SetDispatcher(q)
	ch := New(ac)
	t.Cleanup(ch.Close)
	tt := ui.NewTester(func(c *ui.Context) { ch.Build(c, nil) }, 1300, 850)
	h := &harness{ch, tt, q, a}
	h.settle(func() bool { return ch.cwd != "" && len(ch.comp.commands) == 2 })
	return h
}

func (h *harness) settle(cond func() bool) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.q.Drain() > 0 {
			h.tt.Frame()
		}
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	panic(fmt.Sprintf("timed out; texts %q", h.tt.Texts()))
}

func TestPermissionDialogEscapeDenies(t *testing.T) {
	h := newHarness(t)
	cv := h.ch.conv()
	cv.ID = "s1"
	h.ch.convs["s1"], h.ch.current, h.ch.draftConv = cv, "s1", nil
	cv.AddPermission(client.PermissionRequest{ID: "p1", SessionID: "s1", ToolName: "bash", Description: "run ls"})
	h.tt.Frame()
	if !h.tt.HasText("Permission required") || !h.tt.HasText("Deny") || !h.tt.HasText("Allow for session") {
		t.Fatalf("dialog missing: %q", h.tt.Texts())
	}
	// A click on the scrim must not answer the request.
	h.tt.ClickAt(5, 5)
	h.tt.Frame()
	if len(cv.Permissions) != 1 {
		t.Fatal("click outside answered the request")
	}
	h.tt.Key(0, ui.KeyEscape)
	h.settle(func() bool { return h.a.called("/api/v1/permissions/respond") })
	if !h.a.called(`"action":"deny"`) || len(cv.Permissions) != 0 {
		t.Errorf("escape did not deny: %v", h.a.calls)
	}
}

func TestQuestionDialogStepperFlow(t *testing.T) {
	h := newHarness(t)
	cv := h.ch.conv()
	cv.ID = "s1"
	h.ch.convs["s1"], h.ch.current, h.ch.draftConv = cv, "s1", nil
	cv.AddQuestion(client.QuestionRequest{ID: "q1", SessionID: "s1", Questions: []client.Question{
		{ID: "a", Question: "Pick one", Options: []client.QuestionOption{{Label: "Red"}, {Label: "Blue", Description: "cold"}}},
		{ID: "b", Question: "Pick many", MultiSelect: true, Options: []client.QuestionOption{{Label: "X"}, {Label: "Y"}}},
	}})
	h.tt.Frame()
	if !h.tt.HasText("Pick one") || !h.tt.HasText("QUESTION 1/2") {
		t.Fatalf("first question missing: %q", h.tt.Texts())
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(h.tt.Click("Blue"))
	must(h.tt.Click("Next"))
	if !h.tt.HasText("Pick many") {
		t.Fatalf("second question missing: %q", h.tt.Texts())
	}
	must(h.tt.Click("X"))
	must(h.tt.Click("Y"))
	must(h.tt.Click("Review"))
	if !h.tt.HasText("Review your answers") || !h.tt.HasText("→ Blue") || !h.tt.HasText("→ X, Y") {
		t.Fatalf("review missing: %q", h.tt.Texts())
	}
	must(h.tt.Click("Confirm"))
	h.settle(func() bool { return h.a.called("/api/v1/questions/respond") })
	body := ""
	h.a.mu.Lock()
	for _, c := range h.a.calls {
		if strings.Contains(c, "questions/respond") {
			body = c
		}
	}
	h.a.mu.Unlock()
	for _, want := range []string{`"questionId":"a"`, `"selected":["Blue"]`, `"questionId":"b"`, `"selected":["X","Y"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s lacks %s", body, want)
		}
	}
}

func TestQuestionEscapeCancels(t *testing.T) {
	h := newHarness(t)
	cv := h.ch.conv()
	cv.ID = "s1"
	h.ch.convs["s1"], h.ch.current, h.ch.draftConv = cv, "s1", nil
	cv.AddQuestion(client.QuestionRequest{ID: "q9", SessionID: "s1", Questions: []client.Question{{ID: "a", Question: "Q?", Options: []client.QuestionOption{{Label: "A"}}}}})
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEscape)
	h.settle(func() bool { return h.a.called(`"cancelled":true`) })
}

func TestSlashMenuAndHistory(t *testing.T) {
	h := newHarness(t)
	h.tt.Type("/g")
	h.tt.Frame()
	if !h.tt.HasText("/goal") || h.tt.HasText("/help") {
		t.Fatalf("slash menu not filtered: %q", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyTab)
	h.tt.Frame()
	if h.ch.draft != "/goal " {
		t.Fatalf("tab did not complete: %q", h.ch.draft)
	}
	// Escape closes the popup without clearing the draft.
	h.tt.Key(0, ui.KeyBackspace)
	h.tt.Type("/")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.tt.HasText("Commands") {
		t.Error("popup still open after Escape")
	}
	// History: a sent prompt is recalled with Up.
	h.ch.draft = ""
	h.ch.pushHistory("first prompt")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyUp)
	h.tt.Frame()
	if h.ch.draft != "first prompt" {
		t.Errorf("history recall: %q", h.ch.draft)
	}
}

func TestMentionPopup(t *testing.T) {
	h := newHarness(t)
	h.tt.Type("see @ma")
	h.settle(func() bool { h.tt.Frame(); return len(h.ch.comp.mentionHits) == 1 })
	h.tt.Frame()
	if !h.tt.HasText("cmd/main.go") {
		t.Fatalf("mention hit missing: %q", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if h.ch.draft != "see @cmd/main.go " {
		t.Errorf("draft = %q", h.ch.draft)
	}
}

func TestModelPickerPerSession(t *testing.T) {
	h := newHarness(t)
	cv := h.ch.conv()
	cv.ID = "s1"
	h.ch.convs["s1"], h.ch.current, h.ch.draftConv = cv, "s1", nil
	h.ch.model.models = []client.Model{{ID: "m1", Name: "M One", Provider: "p"}, {ID: "auto", Name: "Auto", Provider: "auto"}}
	h.ch.model.open = true
	h.tt.Frame()
	if !h.tt.HasText("Switch Model") || !h.tt.HasText("M One") {
		t.Fatalf("picker missing: %q", h.tt.Texts())
	}
	if err := h.tt.Click("M One"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return h.a.called("/api/v1/models/active") })
	if !h.a.called(`"sessionId":"s1"`) || !h.a.called(`"model":"m1"`) {
		t.Errorf("selection not session scoped: %v", h.a.calls)
	}
}

func TestInfoSidebarAndBanners(t *testing.T) {
	h := newHarness(t)
	cv := h.ch.conv()
	cv.ID = "s1"
	h.ch.convs["s1"], h.ch.current, h.ch.draftConv = cv, "s1", nil
	h.ch.sessions = []client.SessionSummary{{Session: client.Session{ID: "s1", Title: "My session", PromptTokens: 1500, CompletionTokens: 500, Cost: 0.02}}}
	cv.ContextWindow = 8000
	cv.Plan = []client.PlanEntry{{Title: "step one", Status: "completed"}, {Title: "step two", Status: "in_progress"}}
	cv.Goal = &client.GoalStatus{Objective: "ship it", Status: "running", Iteration: 2, MaxIterations: 5}
	cv.addChange("/w/a.go", 3, 1, "a", "b")
	h.ch.tasks = []client.Task{{ID: "t1", Name: "worker", Status: "running"}}
	h.tt.Frame()
	for _, want := range []string{"My session", "ship it", "Iteration 2/5", "1 file changed", "a.go", "USAGE", "2.0K / 8.0K (25%)", "worker", "step two"} {
		if !h.tt.HasText(want) {
			t.Errorf("missing %q in %q", want, h.tt.Texts())
		}
	}
}

func TestToolCardsFoldAndDiffExpand(t *testing.T) {
	h := newHarness(t)
	cv := h.ch.conv()
	cv.BeginRun("please change the file")
	cv.Apply(client.StreamEvent{Type: client.EventContentDelta, Text: "working"})
	for i, name := range []string{"bash", "read", "edit"} {
		cv.Apply(client.StreamEvent{Type: client.EventToolCall, ToolCall: &client.ToolCallEvent{ID: fmt.Sprint("t", i), Name: name,
			Input: `{"command":"ls","file_path":"/w/a.go","old_string":"old line","new_string":"new line"}`, Status: "completed"}})
	}
	cv.Finish()
	h.tt.Frame()
	if !h.tt.HasText("ran 1 command · read 1 file · edited 1 file") {
		t.Fatalf("group summary missing: %q", h.tt.Texts())
	}
	if err := h.tt.Click("ran 1 command · read 1 file · edited 1 file"); err != nil {
		t.Fatal(err)
	}
	for range 20 { // let the follow-end animation settle
		h.tt.Frame()
		time.Sleep(20 * time.Millisecond)
	}
	cv.List.ScrollToEnd()
	for range 20 {
		h.tt.Frame()
		time.Sleep(20 * time.Millisecond)
	}
	if err := h.tt.Click("edit w/a.go"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.tt.HasText("-1") || !h.tt.HasText("+1") {
		t.Errorf("edit diff not shown: %q", h.tt.Texts())
	}
}

func TestSimpleChatHasFooterAndNoBanners(t *testing.T) {
	a := newAPI(t)
	c, _ := client.New(client.Options{BaseURL: a.srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ac := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	ac.SetDispatcher(q)
	ch := NewSimple(ac)
	defer ch.Close()
	cv := ch.conv()
	cv.Goal = &client.GoalStatus{Objective: "hidden goal", Status: "running"}
	tt := ui.NewTester(func(c *ui.Context) { ch.Build(c, nil) }, 1100, 700)
	for range 10 {
		q.Drain()
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if !tt.HasText("Connected") || tt.HasText("hidden goal") {
		t.Errorf("simple layout: %q", tt.Texts())
	}
}
