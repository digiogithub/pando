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

	if got, err := r.resolveAgent(""); err != nil || got != config.AgentCoder {
		t.Fatalf("empty name should default to coder, got %q (%v)", got, err)
	}
	if _, err := r.resolveAgent("nope"); err == nil {
		t.Fatal("unknown agent should be rejected")
	}
	// Known to Pando but not exposed over AG-UI.
	if _, err := r.resolveAgent(string(config.AgentTitle)); err == nil {
		t.Fatal("unexposed agent should be rejected")
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
	if poolKey(config.AgentCoder, a) != poolKey(config.AgentCoder, b) {
		t.Fatal("tool order must not change the pool key")
	}
	if poolKey(config.AgentCoder, nil) == poolKey(config.AgentCoder, a) {
		t.Fatal("a declared toolset must not reuse the bare agent instance")
	}
	if poolKey(config.AgentCoder, a) == poolKey(config.AgentTask, a) {
		t.Fatal("different agents must not share an instance")
	}
	changed := []Tool{{Name: "a"}, {Name: "b", Description: "now documented"}}
	if poolKey(config.AgentCoder, b) == poolKey(config.AgentCoder, changed) {
		t.Fatal("a changed tool schema must produce a new key")
	}
}
