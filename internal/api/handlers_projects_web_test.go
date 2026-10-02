package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/project"
)

func newProjectsWebHTTPServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	server := httptest.NewServer(s.corsMiddleware(s.basicAuthMiddleware(s.authMiddleware(mux))))
	t.Cleanup(server.Close)
	return server
}

func newProjectsWebServer(t *testing.T, lookup func(string) (string, string, http.RoundTripper, bool)) (*Server, *httptest.Server) {
	t.Helper()
	s := &Server{
		token:                  "parent-token",
		projectWebCookieSecret: "cookie-secret",
		config:                 ServerConfig{Host: "127.0.0.1", Port: 8765, StartupMode: "serve"},
		projectWebProxyLookup:  lookup,
	}
	return s, newProjectsWebHTTPServer(t, s)
}

func newChildTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestProjectsWebProxyPassesThroughAndStripsHeaders(t *testing.T) {
	var sawAuth, sawParentToken, sawChildToken, sawTokenQuery, sawClient string
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawParentToken = r.Header.Get("X-Pando-Token")
		sawChildToken = r.Header.Get("X-Pando-Token")
		sawTokenQuery = r.URL.Query().Get("token")
		sawClient = r.Header.Get("X-Pando-Client")
		if r.URL.Path != "/api/v1/echo" {
			t.Fatalf("child path = %q, want /api/v1/echo", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Pando-Token", "child-token")
		w.Header().Set("Authorization", "Bearer child-token")
		w.Header().Set("Access-Control-Allow-Origin", "https://child.example")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path":  r.URL.Path,
			"token": r.Header.Get("X-Pando-Token"),
		})
	}))

	_, parent := newProjectsWebServer(t, func(projectID string) (string, string, http.RoundTripper, bool) {
		if projectID != "p1" {
			return "", "", nil, false
		}
		return child.URL, "child-token", child.Client().Transport, true
	})

	req, err := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web/api/v1/echo?token=parent-token&keep=1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Basic should-not-pass")
	resp, err := parent.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if sawAuth != "" {
		t.Fatalf("child saw Authorization header %q", sawAuth)
	}
	if sawParentToken != "child-token" {
		t.Fatalf("child token = %q, want child-token", sawParentToken)
	}
	if sawChildToken != "child-token" {
		t.Fatalf("child injected token = %q, want child-token", sawChildToken)
	}
	if sawTokenQuery != "" {
		t.Fatalf("child saw token query %q, want empty", sawTokenQuery)
	}
	if sawClient != "web" {
		t.Fatalf("child saw X-Pando-Client = %q, want web", sawClient)
	}
	if got := resp.Header.Get("X-Pando-Token"); got != "" {
		t.Fatalf("browser saw X-Pando-Token = %q, want empty", got)
	}
	if got := resp.Header.Get("Authorization"); got != "" {
		t.Fatalf("browser saw Authorization = %q, want empty", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
}

func TestProjectsWebProxyReplacesQueryTokenForSSEStyleCalls(t *testing.T) {
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("token"); got != "" {
			t.Fatalf("child query token = %q, want empty", got)
		}
		if got := r.Header.Get("X-Pando-Token"); got != "child-token" {
			t.Fatalf("child header token = %q, want child-token", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	_, parent := newProjectsWebServer(t, func(projectID string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, projectID == "p1"
	})

	resp, err := parent.Client().Get(parent.URL + "/api/v1/projects/p1/web/api/v1/projects/events?token=parent-token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

func TestProjectsWebProxyStreamsSSEWithoutBuffering(t *testing.T) {
	finished := make(chan struct{})
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("backend response writer does not flush")
		}
		_, _ = io.WriteString(w, "data: first\n\n")
		flusher.Flush()
		time.Sleep(700 * time.Millisecond)
		_, _ = io.WriteString(w, "data: second\n\n")
		flusher.Flush()
		close(finished)
	}))

	_, parent := newProjectsWebServer(t, func(projectID string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, projectID == "p1"
	})

	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web/api/v1/chat/stream?token=parent-token", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := parent.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("first SSE event arrived after %v, want it before backend finished", elapsed)
	}
	if line != "data: first\n" {
		t.Fatalf("first SSE line = %q, want %q", line, "data: first\n")
	}
	select {
	case <-finished:
		t.Fatal("backend finished before the client saw the first SSE event")
	default:
	}
}

func TestProjectsWebProxyWebSocketPTY(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/terminal/pty" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Pando-Token"); got != "child-token" {
			t.Fatalf("child websocket token = %q, want child-token", got)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Upgrade: %v", err)
		}
		defer conn.Close()
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		if err := conn.WriteMessage(msgType, append([]byte("echo:"), payload...)); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
	}))

	_, parent := newProjectsWebServer(t, func(projectID string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, projectID == "p1"
	})

	wsURL := "ws" + strings.TrimPrefix(parent.URL, "http") + "/api/v1/projects/p1/web/api/v1/terminal/pty?token=parent-token"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	msgType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msgType != websocket.TextMessage || string(payload) != "echo:hello" {
		t.Fatalf("echo = type %d payload %q, want type %d payload %q", msgType, payload, websocket.TextMessage, "echo:hello")
	}
}

func TestProjectsWebProxyNotOpen(t *testing.T) {
	_, parent := newProjectsWebServer(t, func(string) (string, string, http.RoundTripper, bool) {
		return "", "", nil, false
	})

	req, _ := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web/api/v1/sessions", nil)
	req.Header.Set("X-Pando-Token", "parent-token")
	resp, err := parent.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if body := strings.TrimSpace(readAllString(t, resp.Body)); body != `{"error":"project_web_not_open"}` {
		t.Fatalf("body = %q", body)
	}
}

func TestProjectsWebProxyUnavailableWhenChildIsDown(t *testing.T) {
	child := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	baseURL := child.URL
	transport := child.Client().Transport
	child.Close()

	_, parent := newProjectsWebServer(t, func(projectID string) (string, string, http.RoundTripper, bool) {
		return baseURL, "child-token", transport, projectID == "p1"
	})

	req, _ := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web/api/v1/sessions", nil)
	req.Header.Set("X-Pando-Token", "parent-token")
	resp, err := parent.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	if body := strings.TrimSpace(readAllString(t, resp.Body)); body != `{"error":"project_web_unavailable"}` {
		t.Fatalf("body = %q", body)
	}
}

func TestProjectsWebProxyRejectsTraversal(t *testing.T) {
	var hits atomic.Int32
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	_, parent := newProjectsWebServer(t, func(projectID string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, projectID == "p1"
	})

	client := parent.Client()
	for _, rawURL := range []string{
		parent.URL + "/api/v1/projects/p1/web/%2e%2e/api/v1/sessions",
		parent.URL + "/api/v1/projects/p1/web/%2e%2e%2fapi/v1/sessions",
		parent.URL + "/api/v1/projects/p1/web/a%2fb",
	} {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatalf("NewRequest(%q): %v", rawURL, err)
		}
		req.Header.Set("X-Pando-Token", "parent-token")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do(%q): %v", rawURL, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", rawURL, resp.StatusCode)
		}
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("child backend was hit %d times, want 0", got)
	}
}

func TestProjectsWebProxyRedirectsMissingSlash(t *testing.T) {
	_, parent := newProjectsWebServer(t, func(string) (string, string, http.RoundTripper, bool) {
		return "", "", nil, false
	})

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("stop redirect") },
	}
	req, err := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web?token=parent-token&keep=1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.Do(req)
	if err == nil || !strings.Contains(err.Error(), "stop redirect") {
		t.Fatalf("expected redirect stop error, got %v", err)
	}
	if resp == nil {
		t.Fatal("redirect response is nil")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want 308", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/api/v1/projects/p1/web/?token=parent-token&keep=1" {
		t.Fatalf("Location = %q, want %q", got, "/api/v1/projects/p1/web/?token=parent-token&keep=1")
	}
}

func TestProjectsWebProxyRequiresAuth(t *testing.T) {
	_, parent := newProjectsWebServer(t, func(string) (string, string, http.RoundTripper, bool) {
		return "", "", nil, false
	})

	resp, err := parent.Client().Get(parent.URL + "/api/v1/projects/p1/web/api/v1/sessions")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestProjectsWebRoutesPreferProxyAndKeepProjectRoute(t *testing.T) {
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ping" {
			t.Fatalf("child path = %q, want /api/v1/ping", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))

	s := &Server{
		token: "parent-token",
		app: &app.App{
			Projects:       stubProjectService{projects: map[string]*project.Project{"p1": {ID: "p1", Name: "One", Path: t.TempDir()}}},
			ProjectManager: &project.Manager{},
		},
		config: ServerConfig{Host: "127.0.0.1", Port: 8765, StartupMode: "serve"},
		projectWebProxyLookup: func(projectID string) (string, string, http.RoundTripper, bool) {
			return child.URL, "child-token", child.Client().Transport, projectID == "p1"
		},
	}
	parent := newProjectsWebHTTPServer(t, s)

	projectReq, _ := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1", nil)
	projectReq.Header.Set("X-Pando-Token", "parent-token")
	projectResp, err := parent.Client().Do(projectReq)
	if err != nil {
		t.Fatalf("project Do: %v", err)
	}
	defer projectResp.Body.Close()
	if projectResp.StatusCode != http.StatusOK {
		t.Fatalf("project status = %d, want 200", projectResp.StatusCode)
	}
	if body := readAllString(t, projectResp.Body); !strings.Contains(body, `"id":"p1"`) {
		t.Fatalf("project body = %q, want project payload", body)
	}

	webReq, _ := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web/api/v1/ping", nil)
	webReq.Header.Set("X-Pando-Token", "parent-token")
	webResp, err := parent.Client().Do(webReq)
	if err != nil {
		t.Fatalf("web Do: %v", err)
	}
	defer webResp.Body.Close()
	if webResp.StatusCode != http.StatusOK {
		t.Fatalf("web status = %d, want 200", webResp.StatusCode)
	}
	if body := strings.TrimSpace(readAllString(t, webResp.Body)); body != `{"ok":true}` {
		t.Fatalf("web body = %q", body)
	}
}

func TestProjectsWebPrefixedIndexInjectsBeforeBootstrapAndRewritesBase(t *testing.T) {
	s := &Server{
		config: ServerConfig{
			UIBaseURL:      "https://127.0.0.1:8765",
			PublicBasePath: "/api/v1/projects/p1/web",
		},
		staticFS: fakeStaticFS{files: map[string]string{
			"index.html": `<!doctype html><html><head><meta charset="utf-8" /><base href="/" /><script>bootstrap()</script></head><body></body></html>`,
		}},
	}

	rec := httptest.NewRecorder()
	s.serveIndexHTML(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(body, `<base href="/" />`) {
		t.Fatalf("base href was not rewritten: %q", body)
	}
	if !strings.Contains(body, `<base href="/api/v1/projects/p1/web/" />`) {
		t.Fatalf("rewritten base href missing: %q", body)
	}

	injected := `<script>window.__PANDO_API_BASE__="/api/v1/projects/p1/web";window.__PANDO_ROUTER_BASENAME__="/api/v1/projects/p1/web";</script>`
	headIdx := strings.Index(body, "<head>")
	injectIdx := strings.Index(body, injected)
	metaIdx := strings.Index(body, `<meta charset="utf-8" />`)
	bootstrapIdx := strings.Index(body, "<script>bootstrap()</script>")
	if headIdx < 0 || injectIdx < 0 || metaIdx < 0 || bootstrapIdx < 0 {
		t.Fatalf("expected head/injection/meta/bootstrap markers in %q", body)
	}
	if !(headIdx < injectIdx && injectIdx < metaIdx && injectIdx < bootstrapIdx) {
		t.Fatalf("injection order is wrong: %q", body)
	}
}

func TestProjectsWebPrefixedIndexSkipsCompressedVariant(t *testing.T) {
	s := &Server{
		config: ServerConfig{PublicBasePath: "/api/v1/projects/p1/web"},
		staticFS: fakeStaticFS{files: map[string]string{
			"index.html":    `<!doctype html><html><head><base href="/" /></head><body>plain</body></html>`,
			"index.html.br": "compressed",
		}},
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	s.serveIndexHTML(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "plain") || strings.Contains(body, "compressed") {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestProjectsWebMainIndexInjectsAfterHead(t *testing.T) {
	s := &Server{config: ServerConfig{UIBaseURL: "https://127.0.0.1:8765"}}
	body := string(s.InjectRuntimeConfig([]byte(`<!doctype html><html><head><meta charset="utf-8" /><script>bootstrap()</script></head></html>`)))
	injected := `<script>window.__PANDO_API_BASE__="https://127.0.0.1:8765";</script>`
	headIdx := strings.Index(body, "<head>")
	injectIdx := strings.Index(body, injected)
	metaIdx := strings.Index(body, `<meta charset="utf-8" />`)
	if headIdx < 0 || injectIdx < 0 || metaIdx < 0 {
		t.Fatalf("expected head/injection/meta markers in %q", body)
	}
	if !(headIdx < injectIdx && injectIdx < metaIdx) {
		t.Fatalf("main-instance injection order is wrong: %q", body)
	}
}

func TestProjectsWebHealthReportsPublicBasePath(t *testing.T) {
	s := &Server{config: ServerConfig{PublicBasePath: "/api/v1/projects/p1/web"}}
	rec := httptest.NewRecorder()
	s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var payload healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.PublicBasePath != "/api/v1/projects/p1/web" {
		t.Fatalf("public_base_path = %q, want %q", payload.PublicBasePath, "/api/v1/projects/p1/web")
	}
}

func readAllString(t *testing.T, r io.Reader) string {
	t.Helper()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}

func TestProjectsWebProxyAcceptsCookieAndNeverForwardsIt(t *testing.T) {
	var sawCookie, sawOther, sawToken string
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(projectWebCookieName); err == nil {
			sawCookie = c.Value
		}
		if c, err := r.Cookie("other"); err == nil {
			sawOther = c.Value
		}
		sawToken = r.Header.Get("X-Pando-Token")
		w.WriteHeader(http.StatusOK)
	}))

	_, parent := newProjectsWebServer(t, func(projectID string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, projectID == "p1"
	})

	req, err := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web/assets/app.js", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: projectWebCookieName, Value: "cookie-secret"})
	req.AddCookie(&http.Cookie{Name: "other", Value: "kept"})
	resp, err := parent.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if sawCookie != "" {
		t.Fatalf("child saw the proxy cookie %q", sawCookie)
	}
	if sawOther != "kept" {
		t.Fatalf("child saw other cookie %q, want kept", sawOther)
	}
	if sawToken != "child-token" {
		t.Fatalf("child token = %q, want child-token", sawToken)
	}
}

func TestProjectsWebCookieIsScopedToProxyPaths(t *testing.T) {
	_, parent := newProjectsWebServer(t, func(string) (string, string, http.RoundTripper, bool) {
		return "", "", nil, false
	})

	cases := []struct {
		name, method, path, cookie string
		wantUnauthorized           bool
	}{
		{"proxy path, valid cookie", http.MethodGet, "/api/v1/projects/p1/web/", "cookie-secret", false},
		{"proxy path, wrong cookie", http.MethodGet, "/api/v1/projects/p1/web/", "nope", true},
		{"proxy path, no cookie", http.MethodGet, "/api/v1/projects/p1/web/", "", true},
		{"open control endpoint", http.MethodPost, "/api/v1/projects/p1/web/open", "cookie-secret", true},
		{"close control endpoint", http.MethodPost, "/api/v1/projects/p1/web/close", "cookie-secret", true},
		{"project resource", http.MethodGet, "/api/v1/projects/p1", "cookie-secret", true},
		{"web instance list", http.MethodGet, "/api/v1/projects/web", "cookie-secret", true},
		{"unrelated api", http.MethodGet, "/api/v1/sessions", "cookie-secret", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, parent.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: projectWebCookieName, Value: tc.cookie})
			}
			resp, err := parent.Client().Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()
			if got := resp.StatusCode == http.StatusUnauthorized; got != tc.wantUnauthorized {
				t.Fatalf("status = %d, unauthorized = %v, want %v", resp.StatusCode, got, tc.wantUnauthorized)
			}
		})
	}
}

func TestSetProjectWebCookieAttributes(t *testing.T) {
	s := &Server{token: "parent-token", projectWebCookieSecret: "cookie-secret"}
	rec := httptest.NewRecorder()
	s.setProjectWebCookie(rec, httptest.NewRequest(http.MethodGet, "https://localhost/api/v1/projects/web", nil))

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != projectWebCookieName || c.Value != "cookie-secret" {
		t.Fatalf("cookie = %s=%s", c.Name, c.Value)
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != projectWebCookiePath {
		t.Fatalf("attributes: HttpOnly=%v Secure=%v SameSite=%v Path=%q", c.HttpOnly, c.Secure, c.SameSite, c.Path)
	}
}

func TestProjectsWebCookieValueIsNotTheAPIToken(t *testing.T) {
	s, _ := newProjectsWebServer(t, nil)
	s.projectWebCookieSecret = "cookie-secret"
	rec := httptest.NewRecorder()
	s.setProjectWebCookie(rec, httptest.NewRequest(http.MethodGet, "https://localhost/api/v1/projects/web", nil))
	if got := rec.Result().Cookies()[0].Value; got == s.token {
		t.Fatal("proxy cookie carries the API token")
	}
	// The API token itself is not a valid cookie value.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/web/", nil)
	req.AddCookie(&http.Cookie{Name: projectWebCookieName, Value: s.token})
	if s.hasValidProjectWebCookie(req) {
		t.Fatal("API token accepted as the proxy cookie")
	}
}

func TestProjectsWebCookieOnlyRequestsNeedSameOrigin(t *testing.T) {
	var hits atomic.Int32
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	_, parent := newProjectsWebServer(t, func(string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, true
	})
	host := strings.TrimPrefix(parent.URL, "http://")

	cases := []struct {
		name, method string
		headers      map[string]string
		wantStatus   int
	}{
		{"GET no proof", http.MethodGet, nil, http.StatusOK},
		{"HEAD no proof", http.MethodHead, nil, http.StatusOK},
		{"POST no proof", http.MethodPost, nil, http.StatusForbidden},
		{"POST same-origin fetch metadata", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"POST same-site fetch metadata", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"POST cross-site fetch metadata", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://" + host}, http.StatusForbidden},
		{"POST same Origin", http.MethodPost, map[string]string{"Origin": "http://" + host}, http.StatusOK},
		{"POST other port Origin", http.MethodPost, map[string]string{"Origin": "http://127.0.0.1:1"}, http.StatusForbidden},
		{"DELETE other scheme Origin", http.MethodDelete, map[string]string{"Origin": "https://" + host}, http.StatusForbidden},
		{"WS upgrade no proof", http.MethodGet, map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}, http.StatusForbidden},
		{"WS upgrade cross-origin", http.MethodGet, map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Origin": "http://evil.example"}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, parent.URL+"/api/v1/projects/p1/web/api/v1/x", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.AddCookie(&http.Cookie{Name: projectWebCookieName, Value: "cookie-secret"})
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			resp, err := parent.Client().Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusForbidden {
				if body := readAllString(t, resp.Body); !strings.Contains(body, "cross_origin_forbidden") {
					t.Fatalf("body = %q, want cross_origin_forbidden", body)
				}
			}
		})
	}

	// A valid header token is unaffected by the origin rule.
	req, _ := http.NewRequest(http.MethodPost, parent.URL+"/api/v1/projects/p1/web/api/v1/x", nil)
	req.Header.Set("X-Pando-Token", "parent-token")
	resp, err := parent.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("header-token POST status = %d, want 200", resp.StatusCode)
	}
}

func TestProjectsWebProxyAnswersTokenExchangeItself(t *testing.T) {
	var hits atomic.Int32
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"token":"child-token"}`))
	}))
	_, parent := newProjectsWebServer(t, func(string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, true
	})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, _ := http.NewRequest(method, parent.URL+"/api/v1/projects/p1/web/api/v1/token", strings.NewReader("{}"))
		req.Header.Set("X-Pando-Token", "parent-token")
		resp, err := parent.Client().Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		body := readAllString(t, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"proxied"`) || strings.Contains(body, "child-token") {
			t.Fatalf("%s token exchange = %d %q", method, resp.StatusCode, body)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("token exchange reached the child %d times", hits.Load())
	}
}

func TestProjectsWebProxyRefusesPreview(t *testing.T) {
	var hits atomic.Int32
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	_, parent := newProjectsWebServer(t, func(string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, true
	})
	for _, path := range []string{"/preview/", "/preview/abc/index.html", "/preview"} {
		req, _ := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web"+path, nil)
		req.Header.Set("X-Pando-Token", "parent-token")
		resp, err := parent.Client().Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		body := readAllString(t, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "not_available_in_project_tab") {
			t.Fatalf("%s = %d %q", path, resp.StatusCode, body)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("preview request reached the child")
	}
}

func TestProjectsWebProxyResponseHardening(t *testing.T) {
	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "evil=1; Path=/")
		switch r.URL.Path {
		case "/html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors *")
			_, _ = w.Write([]byte("<html></html>"))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
		case "/redir-rel":
			http.Redirect(w, r, "/login?next=1", http.StatusFound)
		case "/redir-abs-child":
			http.Redirect(w, r, "https://"+r.Host+"/dest#frag", http.StatusFound)
		case "/redir-abs-evil":
			http.Redirect(w, r, "https://evil.example/", http.StatusFound)
		case "/redir-proto":
			w.Header().Set("Location", "//evil.example/x")
			w.WriteHeader(http.StatusFound)
		}
	}))
	_, parent := newProjectsWebServer(t, func(string) (string, string, http.RoundTripper, bool) {
		return child.URL, "child-token", child.Client().Transport, true
	})
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web"+path, nil)
		req.Header.Set("X-Pando-Token", "parent-token")
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	html := get("/html")
	if len(html.Header.Values("Set-Cookie")) != 0 {
		t.Fatal("Set-Cookie leaked through the proxy")
	}
	if csp := html.Header.Get("Content-Security-Policy"); csp != "default-src 'self'; frame-ancestors 'self'" {
		t.Fatalf("CSP = %q", csp)
	}
	if html.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q", html.Header.Get("X-Frame-Options"))
	}
	if js := get("/json"); js.Header.Get("X-Frame-Options") != "" || js.Header.Get("Content-Security-Policy") != "" {
		t.Fatal("non-HTML response got framing headers")
	}
	if loc := get("/redir-rel").Header.Get("Location"); loc != "/api/v1/projects/p1/web/login?next=1" {
		t.Fatalf("relative Location = %q", loc)
	}
	if loc := get("/redir-abs-child").Header.Get("Location"); loc != "/api/v1/projects/p1/web/dest#frag" {
		t.Fatalf("absolute child Location = %q", loc)
	}
	for _, path := range []string{"/redir-abs-evil", "/redir-proto"} {
		resp := get(path)
		if resp.StatusCode != http.StatusBadGateway || resp.Header.Get("Location") != "" {
			t.Fatalf("%s = %d Location=%q, want 502 without Location", path, resp.StatusCode, resp.Header.Get("Location"))
		}
		if body := readAllString(t, resp.Body); !strings.Contains(body, "project_web_bad_redirect") {
			t.Fatalf("%s body = %q", path, body)
		}
	}
}

func TestProjectChildTokenEndpointRequiresToken(t *testing.T) {
	s := &Server{
		token:  "minted-by-parent",
		config: ServerConfig{Host: "127.0.0.1", Port: 8765, StartupMode: "project-child"},
	}
	srv := newProjectsWebHTTPServer(t, s)

	resp, err := http.Get(srv.URL + TokenPath)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	body := readAllString(t, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || strings.Contains(body, "minted-by-parent") {
		t.Fatalf("unauthenticated token request = %d %q", resp.StatusCode, body)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+TokenPath, nil)
	req.Header.Set("X-Pando-Token", "minted-by-parent")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated token request = %d, want 200", resp.StatusCode)
	}
}

func TestProjectChildHealthReportsPID(t *testing.T) {
	child := &Server{config: ServerConfig{StartupMode: "project-child"}}
	rec := httptest.NewRecorder()
	child.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var got struct {
		PID int `json:"pid"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.PID != os.Getpid() {
		t.Fatalf("child pid = %d, want %d", got.PID, os.Getpid())
	}

	plain := &Server{config: ServerConfig{StartupMode: "serve"}}
	rec = httptest.NewRecorder()
	plain.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if strings.Contains(rec.Body.String(), `"pid"`) {
		t.Fatalf("non-child health leaks pid: %s", rec.Body.String())
	}
}

func TestInjectRuntimeConfigEscapesBaseHref(t *testing.T) {
	s := &Server{config: ServerConfig{PublicBasePath: `/x"><script>alert(1)</script>`}}
	out := string(s.InjectRuntimeConfig([]byte(`<head><base href="/" /></head>`)))
	if strings.Contains(out, `"><script>alert`) && !strings.Contains(out, "&#34;&gt;") {
		t.Fatalf("base href not escaped: %s", out)
	}
	if !strings.Contains(out, `<base href="/x&#34;&gt;&lt;script&gt;`) {
		t.Fatalf("unexpected base href: %s", out)
	}
}
