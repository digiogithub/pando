package preview

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleDoc = `<!doctype html>
<html><head><title>Landing</title></head>
<body><h1 data-pando-id="n1">Hello</h1>
</body>
</html>
`

// newArtifactDir writes a small artifact and returns its directory.
func newArtifactDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(sampleDoc), 0o644); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.css"), []byte("body{margin:0}"), 0o644); err != nil {
		t.Fatalf("write css: %v", err)
	}
	return dir
}

func newPublishedServer(t *testing.T, opts Options) (*Server, Grant, string) {
	t.Helper()
	dir := newArtifactDir(t)
	server := New(opts)
	grant, err := server.Publish("dsg_1", "ses_1", dir, "index.html")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return server, grant, dir
}

func get(t *testing.T, server *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServesTheEntryDocumentAndAssets(t *testing.T) {
	server, grant, _ := newPublishedServer(t, Options{})

	root := get(t, server, Prefix+grant.Token+"/")
	if root.Code != http.StatusOK {
		t.Fatalf("entry document: got %d", root.Code)
	}
	if !strings.Contains(root.Body.String(), "Hello") {
		t.Fatalf("entry body not served: %q", root.Body.String())
	}
	if !strings.Contains(root.Body.String(), "_live") {
		t.Fatalf("live reload script missing from HTML: %q", root.Body.String())
	}

	asset := get(t, server, Prefix+grant.Token+"/assets/app.css")
	if asset.Code != http.StatusOK {
		t.Fatalf("asset: got %d", asset.Code)
	}
	if ctype := asset.Header().Get("Content-Type"); !strings.Contains(ctype, "css") {
		t.Fatalf("asset content type %q", ctype)
	}
	if body := asset.Body.String(); body != "body{margin:0}" {
		t.Fatalf("non-HTML asset was rewritten: %q", body)
	}
}

func TestUnknownTokenIsIndistinguishableFromAMissingFile(t *testing.T) {
	server, grant, _ := newPublishedServer(t, Options{})

	bogus := get(t, server, Prefix+"deadbeef/index.html")
	missing := get(t, server, Prefix+grant.Token+"/nope.html")
	if bogus.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("expected 404/404, got %d/%d", bogus.Code, missing.Code)
	}
}

func TestPathEscapeIsRefused(t *testing.T) {
	server, grant, dir := newPublishedServer(t, Options{})
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(secret, []byte("token"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	for _, attempt := range []string{
		"../secret.txt",
		"..%2Fsecret.txt",
		"assets/../../secret.txt",
		"/etc/hostname",
	} {
		rec := get(t, server, Prefix+grant.Token+"/"+attempt)
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "token") {
			t.Fatalf("%q escaped the artifact directory", attempt)
		}
	}
}

func TestSymlinkOutOfTheArtifactIsRefused(t *testing.T) {
	server, grant, dir := newPublishedServer(t, Options{})
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(secret, []byte("token"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leak.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rec := get(t, server, Prefix+grant.Token+"/leak.txt")
	if rec.Code == http.StatusOK {
		t.Fatal("a symlink pointing out of the artifact directory must not be served")
	}
}

func TestSecurityHeadersLockTheDocumentDown(t *testing.T) {
	server, grant, _ := newPublishedServer(t, Options{})
	rec := get(t, server, Prefix+grant.Token+"/index.html")

	csp := rec.Header().Get("Content-Security-Policy")
	for _, required := range []string{"default-src 'self'", "connect-src 'self'", "frame-ancestors 'self'", "form-action 'none'"} {
		if !strings.Contains(csp, required) {
			t.Fatalf("CSP is missing %q: %s", required, csp)
		}
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff is required: previews serve user-authored files")
	}
	if cache := rec.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("a preview must never be cached, got %q", cache)
	}
}

func TestBridgeIsInjectedOnlyOnRequest(t *testing.T) {
	server, grant, _ := newPublishedServer(t, Options{Inject: []byte("/*stamp*/")})

	plain := get(t, server, Prefix+grant.Token+"/index.html")
	if strings.Contains(plain.Body.String(), BridgePath) {
		t.Fatal("a plain preview must stay the markup the agent wrote")
	}

	bridged := get(t, server, Prefix+grant.Token+"/index.html?bridge=1")
	body := bridged.Body.String()
	if !strings.Contains(body, BridgePath) {
		t.Fatalf("bridge tag missing: %s", body)
	}
	if !strings.Contains(body, "/*stamp*/") {
		t.Fatalf("the injected preamble is missing: %s", body)
	}
	if strings.Index(body, "/*stamp*/") > strings.Index(body, BridgePath) {
		t.Fatal("the stamping preamble must precede the bridge so ids exist when it runs")
	}
	if !strings.Contains(body, "Hello") || !strings.Contains(body, "<title>Landing</title>") {
		t.Fatal("injection must not disturb the document")
	}
	if idx := strings.Index(body, "</body>"); idx < 0 || strings.Index(body, BridgePath) > idx {
		t.Fatal("the bridge belongs before </body>")
	}
}

func TestBridgeScriptIsServedWithoutAToken(t *testing.T) {
	server, _, _ := newPublishedServer(t, Options{})
	rec := get(t, server, BridgePath)
	if rec.Code != http.StatusOK {
		t.Fatalf("bridge: got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "data-pando-id") {
		t.Fatal("the bridge must resolve elements by data-pando-id")
	}
}

func TestLiveEndpointReportsTheCurrentRevision(t *testing.T) {
	server, grant, _ := newPublishedServer(t, Options{})

	rec := get(t, server, Prefix+grant.Token+"/_live")
	if got := strings.TrimSpace(rec.Body.String()); got != "0" {
		t.Fatalf("initial live revision = %q, want 0", got)
	}

	server.Bump("dsg_1")
	rec = get(t, server, Prefix+grant.Token+"/_live")
	if got := strings.TrimSpace(rec.Body.String()); got != "1" {
		t.Fatalf("bumped live revision = %q, want 1", got)
	}
}

func TestGrantExpiryStopsResolving(t *testing.T) {
	server, grant, _ := newPublishedServer(t, Options{TTL: time.Millisecond})
	time.Sleep(5 * time.Millisecond)

	if rec := get(t, server, Prefix+grant.Token+"/index.html"); rec.Code != http.StatusNotFound {
		t.Fatalf("an expired grant must stop resolving, got %d", rec.Code)
	}
	if len(server.Grants()) != 0 {
		t.Fatal("an expired grant must be dropped")
	}
}

func TestRepublishKeepsTheTokenStable(t *testing.T) {
	server, grant, dir := newPublishedServer(t, Options{})
	again, err := server.Publish("dsg_1", "ses_1", dir, "index.html")
	if err != nil {
		t.Fatalf("republish: %v", err)
	}
	if again.Token != grant.Token {
		t.Fatal("an open preview must survive re-publishing: the token has to stay the same")
	}
}

func TestRevokeSessionDropsEveryGrantOfThatSession(t *testing.T) {
	dir := newArtifactDir(t)
	server := New(Options{})
	if _, err := server.Publish("dsg_1", "ses_1", dir, "index.html"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := server.Publish("dsg_2", "ses_2", dir, "index.html"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	server.RevokeSession("ses_1")

	grants := server.Grants()
	if len(grants) != 1 || grants[0].ArtifactID != "dsg_2" {
		t.Fatalf("only the other session's grant should survive, got %+v", grants)
	}
}

func TestAccessGuardRefusesPublishAndRequests(t *testing.T) {
	refused := errors.New("refused for the test")
	dir := newArtifactDir(t)

	// The guard is consulted on publish...
	blocked := New(Options{Access: func() error { return refused }})
	if _, err := blocked.Publish("dsg_1", "ses_1", dir, "index.html"); !errors.Is(err, refused) {
		t.Fatalf("publish must honour the guard, got %v", err)
	}

	// ...and on every request, so flipping the guard after a publish closes the
	// door on a URL that is already in someone's browser.
	var deny bool
	server := New(Options{Access: func() error {
		if deny {
			return refused
		}
		return nil
	}})
	grant, err := server.Publish("dsg_1", "ses_1", dir, "index.html")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if rec := get(t, server, Prefix+grant.Token+"/index.html"); rec.Code != http.StatusOK {
		t.Fatalf("expected the preview to be served, got %d", rec.Code)
	}
	deny = true
	rec := get(t, server, Prefix+grant.Token+"/index.html")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 once the guard refuses, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "refused for the test") {
		t.Fatalf("the refusal must explain itself, got %q", rec.Body.String())
	}
}

func TestURLShapeCarriesSlideAndBridge(t *testing.T) {
	server, grant, _ := newPublishedServer(t, Options{BaseURL: func() string { return "http://127.0.0.1:9000/" }})

	plain, err := server.URL("dsg_1", URLOptions{})
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	want := "http://127.0.0.1:9000" + Prefix + grant.Token + "/index.html"
	if plain != want {
		t.Fatalf("got %q, want %q", plain, want)
	}

	deck, err := server.URL("dsg_1", URLOptions{Slide: 3, Bridge: true})
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if !strings.HasSuffix(deck, "?bridge=1#slide-3") {
		t.Fatalf("deck URL lost its slide or its bridge: %q", deck)
	}

	if _, err := server.URL("dsg_missing", URLOptions{}); err == nil {
		t.Fatal("an unpublished artifact has no URL")
	}
}

func TestLoopbackServerAnswersOverHTTP(t *testing.T) {
	dir := newArtifactDir(t)
	server := New(Options{})
	if err := server.StartLoopback(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(server.Close)
	// Calling twice must not start a second listener.
	if err := server.StartLoopback(); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if !strings.HasPrefix(server.Addr(), "127.0.0.1:") {
		t.Fatalf("the fallback must stay on loopback, got %q", server.Addr())
	}

	if _, err := server.Publish("dsg_1", "ses_1", dir, "index.html"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	url, err := server.URL("dsg_1", URLOptions{})
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if !strings.HasPrefix(url, "http://"+server.Addr()) {
		t.Fatalf("URL %q does not point at the loopback listener %q", url, server.Addr())
	}

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestNonGetMethodsAreRejected(t *testing.T) {
	server, grant, _ := newPublishedServer(t, Options{})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, Prefix+grant.Token+"/index.html", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("a preview is read-only, got %d", rec.Code)
	}
}

func TestPublishRejectsAMissingDirectory(t *testing.T) {
	server := New(Options{})
	if _, err := server.Publish("dsg_1", "ses_1", filepath.Join(t.TempDir(), "gone"), "index.html"); err == nil {
		t.Fatal("publishing a directory that does not exist must fail")
	}
	if _, err := server.Publish("", "ses_1", t.TempDir(), "index.html"); err == nil {
		t.Fatal("publishing without an artifact id must fail")
	}
}

func TestInjectBridgeAppendsWhenThereIsNoBodyTag(t *testing.T) {
	out := string(injectBridge([]byte("<h1>bare</h1>"), nil))
	if !strings.HasPrefix(out, "<h1>bare</h1>") || !strings.Contains(out, BridgePath) {
		t.Fatalf("fragment handling wrong: %q", out)
	}
}

func TestFrameAncestorsIsConfigurable(t *testing.T) {
	// Default: the mounted deployment shares an origin with the UI framing it.
	server, grant, _ := newPublishedServer(t, Options{})
	rec := get(t, server, Prefix+grant.Token+"/index.html")
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatalf("default CSP wrong: %s", rec.Header().Get("Content-Security-Policy"))
	}

	// A shell running the UI on its own origin — the desktop app — has to be
	// able to widen it, or the canvas would be blocked from framing the preview.
	widened, wgrant, _ := newPublishedServer(t, Options{FrameAncestors: "'self' http://wails.localhost"})
	wrec := get(t, widened, Prefix+wgrant.Token+"/index.html")
	if !strings.Contains(wrec.Header().Get("Content-Security-Policy"), "frame-ancestors 'self' http://wails.localhost") {
		t.Fatalf("override ignored: %s", wrec.Header().Get("Content-Security-Policy"))
	}
}
