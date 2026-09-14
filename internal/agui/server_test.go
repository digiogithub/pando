package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

// newTestRuntime builds a Runtime without agents or services. Everything these
// tests exercise (auth, CORS, discovery, agent resolution) runs before any
// dependency is touched.
func newTestRuntime(cfg Config, token string) *Runtime {
	return &Runtime{
		deps:    Deps{Token: token},
		cfg:     cfg,
		threads: newThreadStore(nil),
		pending: newPendingRegistry(),
		runs:    newRunStore(),
		baseCtx: context.Background(),
	}
}

func testConfig() Config {
	return Config{
		Path:            defaultPath,
		Agents:          []config.AgentName{config.AgentCoder},
		AllowedOrigins:  []string{"https://app.test"},
		RequireToken:    true,
		DisconnectGrace: time.Minute,
	}
}

func TestAuthorizeRejectsMissingToken(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", nil)

	if r.authorize(rec, req) {
		t.Fatal("expected the request to be rejected")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthorizeAcceptsBearerAndQueryToken(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")

	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", nil)
	req.Header.Set("Authorization", "Bearer secret")
	if !r.authorize(httptest.NewRecorder(), req) {
		t.Fatal("bearer token should be accepted")
	}

	// EventSource cannot set headers, so the query parameter is supported too.
	req = httptest.NewRequest(http.MethodPost, defaultPath+"/coder?token=secret", nil)
	if !r.authorize(httptest.NewRecorder(), req) {
		t.Fatal("query token should be accepted")
	}
}

// TestAuthorizeFailsClosedWithoutToken guards the case where the token was never
// provisioned: a required-but-missing token must never degrade into an open
// endpoint.
func TestAuthorizeFailsClosedWithoutToken(t *testing.T) {
	r := newTestRuntime(testConfig(), "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", nil)
	req.Header.Set("Authorization", "Bearer anything")

	if r.authorize(rec, req) {
		t.Fatal("expected the request to be rejected")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestAuthorizeRejectsDisallowedOrigin(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Origin", "https://evil.test")

	if r.authorize(rec, req) {
		t.Fatal("expected the cross-origin request to be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// TestNoImplicitWildcardCORS pins the security decision that separates this
// endpoint from the Web-UI chat stream: an unlisted origin is never echoed back
// and the wildcard is never emitted implicitly.
func TestNoImplicitWildcardCORS(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedOrigins = nil
	r := newTestRuntime(cfg, "secret")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", nil)
	req.Header.Set("Authorization", "Bearer secret")
	r.authorize(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected CORS header %q", got)
	}
}

func TestAllowedOriginIsEchoed(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Origin", "https://app.test")

	if !r.authorize(rec, req) {
		t.Fatal("allowed origin should pass")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.test" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestHandleInfoListsConfiguredAgents(t *testing.T) {
	cfg := testConfig()
	cfg.Agents = []config.AgentName{config.AgentCoder, config.AgentTask}
	r := newTestRuntime(cfg, "secret")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, defaultPath+"/info", nil)
	req.Header.Set("Authorization", "Bearer secret")
	r.handleInfo(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp InfoResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Protocol != "ag-ui" || len(resp.Agents) != 2 {
		t.Fatalf("unexpected info payload: %+v", resp)
	}
	// The URL must be absolute: CopilotKit's HttpAgent is given it verbatim.
	if want := "http://" + req.Host + defaultPath + "/coder"; resp.Agents[0].URL != want {
		t.Fatalf("agent URL = %q, want %q", resp.Agents[0].URL, want)
	}
	if resp.Path != defaultPath {
		t.Fatalf("path = %q", resp.Path)
	}
	if !resp.Capabilities.SharedState || !resp.Capabilities.Interrupts {
		t.Fatalf("state and interrupts are implemented and must be advertised: %+v", resp.Capabilities)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

// TestHandleInfoListsProfilesWithEffectiveModelAndNoAgentInstantiation is the
// PANDO-US-0013 acceptance criterion: GET {path}/info lists every declared
// profile with its effective model, and does so without instantiating an
// agent. r.pool is left nil (as newTestRuntime always builds it): a
// nil-pointer panic here would prove handleInfo tried to warm the pool.
func TestHandleInfoListsProfilesWithEffectiveModelAndNoAgentInstantiation(t *testing.T) {
	prevGlobal := config.Get()
	config.SetForTests(&config.Config{
		Agents: map[config.AgentName]config.Agent{
			config.AgentCoder: {Model: "claude-sonnet-4-20250514"},
		},
	})
	t.Cleanup(func() { config.SetForTests(prevGlobal) })

	cfg := testConfig()
	cfg.Profiles = map[string]Profile{
		"backlog-assistant": {Name: "backlog-assistant", Base: config.AgentCoder},
	}
	r := newTestRuntime(cfg, "secret")
	if r.pool != nil {
		t.Fatal("test setup: expected a nil pool so this test can prove /info never touches it")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, defaultPath+"/info", nil)
	req.Header.Set("Authorization", "Bearer secret")
	r.handleInfo(rec, req) // would panic on r.pool.get(...) if this warmed the pool

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp InfoResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var profileEntry *AgentDescriptor
	for i := range resp.Agents {
		if resp.Agents[i].Name == "backlog-assistant" {
			profileEntry = &resp.Agents[i]
		}
	}
	if profileEntry == nil {
		t.Fatalf("profile not listed in /info: %+v", resp.Agents)
	}
	if want := "http://" + req.Host + defaultPath + "/backlog-assistant"; profileEntry.URL != want {
		t.Fatalf("profile URL = %q, want %q", profileEntry.URL, want)
	}
	if profileEntry.Model == nil || profileEntry.Model.ID != "claude-sonnet-4-20250514" {
		t.Fatalf("profile model = %+v, want the Base agent's configured model", profileEntry.Model)
	}
}

// TestHandleInfoIgnoresForwardedHost: an attacker-controlled X-Forwarded-Host
// must not make discovery hand out URLs pointing at somebody else's server.
func TestHandleInfoIgnoresForwardedHost(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, defaultPath+"/info", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Forwarded-Host", "evil.test")
	r.handleInfo(rec, req)

	if strings.Contains(rec.Body.String(), "evil.test") {
		t.Fatalf("discovery echoed a forwarded host: %s", rec.Body.String())
	}
}

// TestHandleInfoRequiresToken: discovery reports the configured agents and the
// model behind them, so it is authenticated like every other route.
func TestHandleInfoRequiresToken(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")

	rec := httptest.NewRecorder()
	r.handleInfo(rec, httptest.NewRequest(http.MethodGet, defaultPath+"/info", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestResolveAgent(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")

	if got, profile, err := r.resolveAgent(""); err != nil || got != config.AgentCoder || profile != nil {
		t.Fatalf("empty name should default to coder with no profile, got %q profile=%v (%v)", got, profile, err)
	}
	if _, _, err := r.resolveAgent("nope"); err == nil {
		t.Fatal("unknown agent should be rejected")
	}
	// Known to Pando but not exposed over AG-UI.
	if _, _, err := r.resolveAgent(string(config.AgentTitle)); err == nil {
		t.Fatal("unexposed agent should be rejected")
	}
}

// TestResolveAgentProfile is the PANDO-US-0013 acceptance criterion: a
// declared profile resolves to its Base agent plus its own overrides, and an
// undeclared name still 404s.
func TestResolveAgentProfile(t *testing.T) {
	cfg := testConfig()
	cfg.Profiles = map[string]Profile{
		"backlog-assistant": {
			Name:      "backlog-assistant",
			Base:      config.AgentCoder,
			Tools:     []string{"gintrack__*"},
			DenyTools: []string{"bash"},
			Mesnada:   false,
		},
	}
	r := newTestRuntime(cfg, "secret")

	base, profile, err := r.resolveAgent("backlog-assistant")
	if err != nil {
		t.Fatalf("resolveAgent(backlog-assistant): %v", err)
	}
	if base != config.AgentCoder {
		t.Fatalf("base = %q, want coder", base)
	}
	if profile == nil || profile.Name != "backlog-assistant" || len(profile.Tools) != 1 || profile.Tools[0] != "gintrack__*" {
		t.Fatalf("profile = %+v, want the declared backlog-assistant profile", profile)
	}

	if _, _, err := r.resolveAgent("undeclared-profile"); err == nil {
		t.Fatal("an undeclared name must still 404, not resolve to a profile")
	}
}

// TestHandleRunRejectsInputWithoutUserMessage: a request with no trailing
// user message and no live run for the thread to reattach to is neither a
// new turn nor a resumption nor a reattach (PANDO-US-0018) — it 404s rather
// than starting a run.
func TestHandleRunRejectsInputWithoutUserMessage(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	body := `{"threadId":"t1","runId":"r1","messages":[{"id":"m1","role":"assistant","content":"hi"}]}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("agent", "coder")
	r.handleRun(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSessionTitleIsNamespaced(t *testing.T) {
	if got := sessionTitle("fix the build"); got != "agui: fix the build" {
		t.Fatalf("title = %q", got)
	}
	if got := sessionTitle(""); got != "agui: new thread" {
		t.Fatalf("empty title = %q", got)
	}
	long := sessionTitle(strings.Repeat("x", 200))
	if len([]rune(long)) > 70 {
		t.Fatalf("title not truncated: %d runes", len([]rune(long)))
	}
}

func TestPoolKeyIsToolsetAwareAndOrderIndependent(t *testing.T) {
	a := []Tool{{Name: "b"}, {Name: "a"}}
	b := []Tool{{Name: "a"}, {Name: "b"}}
	if poolKey("coder", a) != poolKey("coder", b) {
		t.Fatal("tool order must not change the pool key")
	}
	if poolKey("coder", nil) == poolKey("coder", a) {
		t.Fatal("a declared toolset must not reuse the bare agent instance")
	}
	if poolKey("coder", a) == poolKey("task", a) {
		t.Fatal("different agents must not share an instance")
	}
	changed := []Tool{{Name: "a"}, {Name: "b", Description: "now documented"}}
	if poolKey("coder", b) == poolKey("coder", changed) {
		t.Fatal("a changed tool schema must produce a new key")
	}
}

// TestPoolKeyDistinguishesProfilesOverSameBase is the PANDO-US-0013
// acceptance criterion: two profiles over the same Base agent must key to
// different pool entries, so building one never evicts or reuses the other's
// instance.
func TestPoolKeyDistinguishesProfilesOverSameBase(t *testing.T) {
	if poolKey("backlog-assistant", nil) == poolKey("docs-assistant", nil) {
		t.Fatal("two profiles sharing a Base agent must not collapse onto one pool key")
	}
}

// -------------------------------------------------------- PANDO-US-0016
// MESSAGES_SNAPSHOT resync, exercised directly against runPrelude: it is the
// exact code path handleRun uses to open a run, without needing a real agent
// pool.

// newRunPreludeTestRuntime builds a Runtime with just enough (Deps.Messages
// and cfg) for runPrelude, which touches neither the agent pool nor the
// thread store.
func newRunPreludeTestRuntime(messages *fakeMessageService) *Runtime {
	return &Runtime{
		deps: Deps{Messages: messages},
		cfg:  testConfig(),
	}
}

func seedUserMessage(messages *fakeMessageService, sessionID, id, text string) {
	messages.seed(sessionID, message.Message{
		ID: id, SessionID: sessionID, Role: message.User,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	})
}

// TestRunPreludeOrderingOnPreExistingThreadResync is the PANDO-US-0016
// acceptance criterion: on a pre-existing thread's first attach, event
// ordering is RUN_STARTED -> STATE_SNAPSHOT -> MESSAGES_SNAPSHOT.
func TestRunPreludeOrderingOnPreExistingThreadResync(t *testing.T) {
	messages := newFakeMessageService()
	seedUserMessage(messages, "sess1", "m1", "hi")
	seedUserMessage(messages, "sess1", "m2", "hello back")

	r := newRunPreludeTestRuntime(messages)
	state := newTestTracker()
	tr := newTranslator("t1", "r1").withState(state)

	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	if err := r.runPrelude(context.Background(), sse, tr, state, "sess1", true); err != nil {
		t.Fatalf("runPrelude: %v", err)
	}

	types := frameTypes(rec.Body.String())
	started := indexOf(types, string(EventRunStarted))
	stateSnap := indexOf(types, string(EventStateSnapshot))
	msgsSnap := indexOf(types, string(EventMessagesSnapshot))
	if started < 0 || stateSnap < 0 || msgsSnap < 0 {
		t.Fatalf("missing an expected event: %v", types)
	}
	if !(started < stateSnap && stateSnap < msgsSnap) {
		t.Fatalf("want RUN_STARTED -> STATE_SNAPSHOT -> MESSAGES_SNAPSHOT, got: %v", types)
	}
	if !strings.Contains(rec.Body.String(), `"hello back"`) {
		t.Fatalf("expected the seeded history in the snapshot: %s", rec.Body.String())
	}
}

// TestRunPreludeBrandNewThreadEmitsNoMessagesSnapshot is the PANDO-US-0016
// acceptance criterion: a brand-new thread (resync=false, as handleRun
// computes it from sessionForThread's existed=false) gets no
// MESSAGES_SNAPSHOT, only the two events every run always opens with.
func TestRunPreludeBrandNewThreadEmitsNoMessagesSnapshot(t *testing.T) {
	messages := newFakeMessageService()
	r := newRunPreludeTestRuntime(messages)
	state := newTestTracker()
	tr := newTranslator("t1", "r1").withState(state)

	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	if err := r.runPrelude(context.Background(), sse, tr, state, "sess1", false); err != nil {
		t.Fatalf("runPrelude: %v", err)
	}

	types := frameTypes(rec.Body.String())
	if indexOf(types, string(EventMessagesSnapshot)) >= 0 {
		t.Fatalf("a brand-new thread must not emit MESSAGES_SNAPSHOT: %v", types)
	}
	if indexOf(types, string(EventRunStarted)) < 0 || indexOf(types, string(EventStateSnapshot)) < 0 {
		t.Fatalf("expected RUN_STARTED and STATE_SNAPSHOT regardless of resync: %v", types)
	}
}

// TestRunPreludeSkipsResyncWhenNotRequestedEvenWithHistory guards against
// resync being (re)derived from the message store instead of the caller's
// decision: a thread WITH history still gets no snapshot when resync=false,
// which is what "once per attach" (not "once per pre-existing thread")
// requires.
func TestRunPreludeSkipsResyncWhenNotRequestedEvenWithHistory(t *testing.T) {
	messages := newFakeMessageService()
	seedUserMessage(messages, "sess1", "m1", "already have this")

	r := newRunPreludeTestRuntime(messages)
	state := newTestTracker()
	tr := newTranslator("t1", "r1").withState(state)

	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	if err := r.runPrelude(context.Background(), sse, tr, state, "sess1", false); err != nil {
		t.Fatalf("runPrelude: %v", err)
	}
	if strings.Contains(rec.Body.String(), string(EventMessagesSnapshot)) {
		t.Fatalf("resync=false must suppress MESSAGES_SNAPSHOT even with history present: %s", rec.Body.String())
	}
}

// TestRunPreludeTruncatesLargeHistoryWithinLatencyBudget is the PANDO-US-0016
// acceptance criterion: a history above the configured cap yields a
// truncated, most-recent-first-complete snapshot flagged as truncated, built
// before the run starts (so it cannot itself become the slow part of a run).
func TestRunPreludeTruncatesLargeHistoryWithinLatencyBudget(t *testing.T) {
	messages := newFakeMessageService()
	for i := 1; i <= 10; i++ {
		seedUserMessage(messages, "sess1", fmt.Sprintf("m%d", i), fmt.Sprintf("message number %d", i))
	}

	r := newRunPreludeTestRuntime(messages)
	r.cfg.MessagesSnapshotMaxMessages = 3
	state := newTestTracker()
	tr := newTranslator("t1", "r1").withState(state)

	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}

	start := time.Now()
	if err := r.runPrelude(context.Background(), sse, tr, state, "sess1", true); err != nil {
		t.Fatalf("runPrelude: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("runPrelude took too long for a 10-message history: %v", elapsed)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"truncated":true`) {
		t.Fatalf("expected the snapshot to report truncated:true: %s", body)
	}
	if strings.Contains(body, `"message number 1"`) || strings.Contains(body, `"message number 7"`) {
		t.Fatalf("expected only the most recent messages, oldest ones leaked through: %s", body)
	}
	if !strings.Contains(body, `"message number 10"`) || !strings.Contains(body, `"message number 8"`) {
		t.Fatalf("expected the 3 most recent messages (8, 9, 10) kept: %s", body)
	}
}

// TestRunPreludeToolCallAndResultSurviveIntoTheSnapshot is the PANDO-US-0016
// acceptance criterion: tool calls and their results survive the conversion
// with matching toolCallIds.
func TestRunPreludeToolCallAndResultSurviveIntoTheSnapshot(t *testing.T) {
	messages := newFakeMessageService()
	messages.seed("sess1",
		message.Message{ID: "m1", SessionID: "sess1", Role: message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "read the file"}}},
		message.Message{ID: "m2", SessionID: "sess1", Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{ID: "call-1", Name: "view", Input: `{"file_path":"a.go"}`, Finished: true},
			}},
		message.Message{ID: "m3", SessionID: "sess1", Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "call-1", Name: "view", Content: "package main"},
			}},
	)

	r := newRunPreludeTestRuntime(messages)
	state := newTestTracker()
	tr := newTranslator("t1", "r1").withState(state)

	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	if err := r.runPrelude(context.Background(), sse, tr, state, "sess1", true); err != nil {
		t.Fatalf("runPrelude: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"id":"call-1"`) {
		t.Fatalf("expected the assistant's tool call in the snapshot: %s", body)
	}
	if !strings.Contains(body, `"toolCallId":"call-1"`) {
		t.Fatalf("expected the tool result's matching toolCallId in the snapshot: %s", body)
	}
}

// -------------------------------------------------------- PANDO-US-0018
// one-run-per-thread: end-to-end handleRun races against a fake agent pool.

// fakeAgentService is a minimal agent.Service stand-in: enough for handleRun
// to run a full request through sessionForThread/pool.get/svc.Run without a
// real model provider. Run counts every call it actually makes, which is
// what the TOCTOU race test asserts on.
type fakeAgentService struct {
	*pubsub.Broker[agent.AgentEvent]
	runCalls int32
	// runDelay, when set, is waited (or the context's cancellation observed)
	// before the run produces its response, widening the race window a real
	// concurrent bug would need.
	runDelay time.Duration
}

func newFakeAgentService() *fakeAgentService {
	return &fakeAgentService{Broker: pubsub.NewBroker[agent.AgentEvent]()}
}

func (f *fakeAgentService) Model() models.Model { return models.Model{ID: "fake-model", Name: "fake"} }

func (f *fakeAgentService) Run(ctx context.Context, sessionID, content string, _ ...message.Attachment) (<-chan agent.AgentEvent, error) {
	atomic.AddInt32(&f.runCalls, 1)
	ch := make(chan agent.AgentEvent, 1)
	go func() {
		defer close(ch)
		if f.runDelay > 0 {
			select {
			case <-time.After(f.runDelay):
			case <-ctx.Done():
				return
			}
		}
		ch <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
	}()
	return ch, nil
}

func (f *fakeAgentService) LastRunSystemMessages(string) []string { return nil }
func (f *fakeAgentService) Cancel(string)                         {}
func (f *fakeAgentService) Steer(string, string, ...message.Attachment) error {
	return agent.ErrSessionNotBusy
}
func (f *fakeAgentService) PendingSteering(string) int                   { return 0 }
func (f *fakeAgentService) InjectConclusion(string, string) error        { return agent.ErrSessionNotBusy }
func (f *fakeAgentService) Resume(context.Context, string, string) error { return nil }
func (f *fakeAgentService) ResurrectionCount(string) int                 { return 0 }
func (f *fakeAgentService) IsSessionBusy(string) bool                    { return false }
func (f *fakeAgentService) IsBusy() bool                                 { return false }
func (f *fakeAgentService) Update(config.AgentName, models.ModelID) (models.Model, error) {
	return models.Model{}, nil
}
func (f *fakeAgentService) Summarize(context.Context, string) error { return nil }
func (f *fakeAgentService) SummarizeStream(context.Context, string) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}
func (f *fakeAgentService) SetLuaManager(*luaengine.FilterManager) {}
func (f *fakeAgentService) GetTools() []tools.BaseTool             { return nil }

// newRaceTestRuntime builds a Runtime whose agent pool is pre-seeded with a
// fake agent.Service under the "coder" route key, so handleRun's full
// decide/create path runs for real (sessionForThread, pool.get, svc.Run)
// without needing a real model provider.
func newRaceTestRuntime(t *testing.T, svc agent.Service) *Runtime {
	t.Helper()
	db := newThreadDB(t)
	r, _, _ := newThreadTestRuntime(t, db)
	// AgentPoolSize/TTL default to their zero values in testConfig(), which
	// would make evictLocked reap the pre-seeded entry on the very next
	// pool.get -- give it real headroom, as ConfigFromApp always would.
	r.cfg.AgentPoolSize = defaultPoolSize
	r.cfg.AgentPoolTTL = defaultPoolTTL
	r.pool = newAgentPool(r.deps, r.cfg, r.perms, nil, r.pending)
	r.pool.entries["coder"] = &poolEntry{svc: svc, lastUsed: time.Now()}
	return r
}

func newRunRequest(threadID, runID, prompt string) *http.Request {
	body := fmt.Sprintf(`{"threadId":%q,"runId":%q,"messages":[{"id":"m1","role":"user","content":%q}]}`,
		threadID, runID, prompt)
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("agent", "coder")
	return req
}

// TestConcurrentPostsProduceExactlyOneRun is the PANDO-US-0018 acceptance
// criterion: two (here, many) concurrent POSTs on one threadId produce
// exactly one run; the loser(s) receive one documented error, and repeating
// the race shows no RUN_ERROR{session_busy} leaking from the old
// check-then-act window between runStore and svc.Run.
func TestConcurrentPostsProduceExactlyOneRun(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		svc := newFakeAgentService()
		svc.runDelay = 20 * time.Millisecond
		r := newRaceTestRuntime(t, svc)

		const n = 8
		var wg sync.WaitGroup
		results := make([]*httptest.ResponseRecorder, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				rec := httptest.NewRecorder()
				r.handleRun(rec, newRunRequest("race-thread", fmt.Sprintf("run-%d", i), "go"))
				results[i] = rec
			}(i)
		}
		wg.Wait()

		if got := atomic.LoadInt32(&svc.runCalls); got != 1 {
			t.Fatalf("attempt %d: svc.Run was called %d times, want exactly 1", attempt, got)
		}

		var winners, losers int
		for _, rec := range results {
			body := rec.Body.String()
			if strings.Contains(body, `"code":"session_busy"`) {
				t.Fatalf("attempt %d: a request surfaced RUN_ERROR{session_busy} from the TOCTOU path: %s", attempt, body)
			}
			switch {
			case rec.Code == http.StatusConflict:
				losers++
			case rec.Code == http.StatusOK && strings.Contains(body, `"outcome":"success"`):
				winners++
			default:
				t.Fatalf("attempt %d: unexpected response status=%d body=%s", attempt, rec.Code, body)
			}
		}
		if winners != 1 {
			t.Fatalf("attempt %d: %d requests won the race, want exactly 1 (losers=%d)", attempt, winners, losers)
		}
		if winners+losers != n {
			t.Fatalf("attempt %d: accounted for %d of %d requests", attempt, winners+losers, n)
		}
	}
}

// TestHandleRunLoserGetsDocumentedErrorWithoutAbandoningTheRun is the
// PANDO-US-0018 "do NOT abandon a live run" constraint: a second POST
// carrying a new user message on a thread that already has a live run is
// rejected outright, and the original run is left completely alone (still
// registered, its context never cancelled).
func TestHandleRunLoserGetsDocumentedErrorWithoutAbandoningTheRun(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 1)
	run := newSuspendableRun(events, make(chan suspension, 1))
	cancelled := false
	run.cancel = func() { cancelled = true }
	r.runs.put(run)

	rec := httptest.NewRecorder()
	r.handleRun(rec, newRunRequest("t1", "r2", "a second message"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if cancelled {
		t.Fatal("the run in progress must not be abandoned/cancelled by the loser")
	}
	if _, ok := r.runs.get("t1"); !ok {
		t.Fatal("the run in progress must remain registered")
	}
}

// TestPostWithNoNewMessageReattachesToALiveRun is PANDO-US-0018's "a POST
// carrying no new user message" reattach path: it behaves exactly like GET
// {path}/threads/{id}/stream.
func TestPostWithNoNewMessageReattachesToALiveRun(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)
	go r.pump(run)

	// Only finish the run once the reattach below has actually subscribed:
	// handleRun's own attach runs synchronously in this test's main
	// goroutine, so without this handshake the response could race ahead of
	// it and finish (and remove) the run before the reattach even attaches.
	go func() {
		for i := 0; i < 2000 && run.subscriberCount() < 1; i++ {
			time.Sleep(time.Millisecond)
		}
		events <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
	}()

	body := `{"threadId":"t1","runId":"r2","messages":[{"id":"m1","role":"assistant","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("agent", "coder")
	rec := httptest.NewRecorder()
	r.handleRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"outcome":"success"`) {
		t.Fatalf("reattach via POST did not observe the run finishing: %s", rec.Body.String())
	}
}
