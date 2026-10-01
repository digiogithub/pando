package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
		token:                 "parent-token",
		config:                ServerConfig{Host: "127.0.0.1", Port: 8765, StartupMode: "serve"},
		projectWebProxyLookup: lookup,
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
