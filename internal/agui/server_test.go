package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
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
		Path:           defaultPath,
		Agents:         []config.AgentName{config.AgentCoder},
		AllowedOrigins: []string{"https://app.test"},
		RequireToken:   true,
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

func TestHandleRunRejectsInputWithoutUserMessage(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	body := `{"threadId":"t1","runId":"r1","messages":[{"id":"m1","role":"assistant","content":"hi"}]}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("agent", "coder")
	r.handleRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
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
