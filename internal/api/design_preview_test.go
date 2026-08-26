package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/design/preview"
)

// previewServerAt builds a Server bound to host with a mounted preview server
// holding one published artifact. No app and no database are involved: the
// guard under test depends only on the bind address and the basic-auth config.
func previewServerAt(t *testing.T, host string) (*Server, preview.Grant) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><body>secret design</body></html>"), 0o644); err != nil {
		t.Fatalf("write entry: %v", err)
	}

	s := &Server{
		config:      ServerConfig{Host: host, Port: 41234, StartupMode: "app"},
		token:       "test-token",
		bindHost:    host,
		initialHost: host,
	}
	// Publishing happens while the server is on loopback, so the grant exists
	// before the bind moves: that is exactly the situation the guard must
	// survive — a URL handed out locally, then external access switched on.
	previewSrv := preview.New(preview.Options{BaseURL: s.previewBaseURL, Access: s.previewAccess})
	s.bindHost = "127.0.0.1"
	grant, err := previewSrv.Publish("dsg_1", "ses_1", dir, "index.html")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	s.bindHost = host
	s.preview = previewSrv
	return s, grant
}

// withPreviewAuth installs a basic-auth configuration for the duration of a
// test. It is separate from withBasicAuth in basicauth_test.go because the
// preview guard reads the live bind host from the Server, not from the config.
func withPreviewAuth(t *testing.T, enabled bool) {
	t.Helper()
	previous := config.Get()
	cfg := &config.Config{
		Server: config.APIServerConfig{
			Enabled:   true,
			BasicAuth: config.BasicAuthConfig{Enabled: enabled},
		},
	}
	if enabled {
		cfg.Server.BasicAuth.Users = []config.BasicAuthUser{{Username: "u", Password: "p"}}
	}
	config.SetForTests(cfg)
	t.Cleanup(func() { config.SetForTests(previous) })
}

func TestPreviewIsRefusedOnAnExposedBindWithoutBasicAuth(t *testing.T) {
	withPreviewAuth(t, false)
	s, grant := previewServerAt(t, "0.0.0.0")

	if err := s.previewAccess(); err == nil {
		t.Fatal("a wildcard bind with no basic auth must not serve previews")
	} else if !errors.Is(err, preview.ErrForbidden) {
		t.Fatalf("the refusal must be a preview.ErrForbidden, got %v", err)
	}

	rec := httptest.NewRecorder()
	s.preview.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, preview.Prefix+grant.Token+"/index.html", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret design") {
		t.Fatal("artifact content leaked past the guard")
	}
}

func TestPreviewIsServedOnAnExposedBindOnceBasicAuthIsOn(t *testing.T) {
	withPreviewAuth(t, true)
	s, grant := previewServerAt(t, "0.0.0.0")

	if err := s.previewAccess(); err != nil {
		t.Fatalf("basic auth is configured, the preview must be allowed: %v", err)
	}
	rec := httptest.NewRecorder()
	s.preview.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, preview.Prefix+grant.Token+"/index.html", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestPreviewIsServedOnLoopbackWithoutAnyAuth(t *testing.T) {
	withPreviewAuth(t, false)
	s, grant := previewServerAt(t, "127.0.0.1")

	if err := s.previewAccess(); err != nil {
		t.Fatalf("a loopback bind needs no credentials: %v", err)
	}
	rec := httptest.NewRecorder()
	s.preview.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, preview.Prefix+grant.Token+"/index.html", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestBasicAuthCoversThePreviewSurface(t *testing.T) {
	withPreviewAuth(t, true)
	s, _ := previewServerAt(t, "0.0.0.0")

	cases := map[string]bool{
		preview.Prefix + "abc/index.html": true,
		"/api/v1/sessions":                true,
		"/health":                         false,
		"/index.html":                     false,
		"/assets/app.js":                  false,
	}
	for path, want := range cases {
		got := s.basicAuthEnforced(httptest.NewRequest(http.MethodGet, path, nil))
		if got != want {
			t.Fatalf("basicAuthEnforced(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestPreviewBaseURLResolvesAWildcardBindToLoopback(t *testing.T) {
	s, _ := previewServerAt(t, "0.0.0.0")
	if got := s.previewBaseURL(); got != "http://127.0.0.1:41234" {
		t.Fatalf("got %q", got)
	}

	s.bindHost = "127.0.0.1"
	if got := s.previewBaseURL(); got != "http://127.0.0.1:41234" {
		t.Fatalf("got %q", got)
	}

	s.config.TLSCertFile, s.config.TLSKeyFile = "cert.pem", "key.pem"
	if got := s.previewBaseURL(); !strings.HasPrefix(got, "https://") {
		t.Fatalf("a TLS listener must produce https URLs, got %q", got)
	}
}

// fakeStaticFS is a one-file SPA bundle, enough to make uiHandler take over
// every non-API path the way the real embedded Web UI does.
type fakeStaticFS struct{ files map[string]string }

func (f fakeStaticFS) Open(name string) (fs.File, error) {
	content, ok := f.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &fakeStaticFile{Reader: strings.NewReader(content), name: name, size: int64(len(content))}, nil
}

type fakeStaticFile struct {
	*strings.Reader
	name string
	size int64
}

func (f *fakeStaticFile) Stat() (fs.FileInfo, error) { return fakeStaticInfo{f.name, f.size}, nil }
func (f *fakeStaticFile) Close() error               { return nil }

type fakeStaticInfo struct {
	name string
	size int64
}

func (i fakeStaticInfo) Name() string       { return i.name }
func (i fakeStaticInfo) Size() int64        { return i.size }
func (i fakeStaticInfo) Mode() fs.FileMode  { return 0o444 }
func (i fakeStaticInfo) ModTime() time.Time { return time.Time{} }
func (i fakeStaticInfo) IsDir() bool        { return false }
func (i fakeStaticInfo) Sys() any           { return nil }

// TestPreviewSurvivesTheWholeMiddlewareChain is the regression test for the two
// ways /preview could silently break: the SPA fallback answering it with
// index.html, and the API token middleware rejecting a browser that has no
// token. Both would leave the user staring at a blank frame.
func TestPreviewSurvivesTheWholeMiddlewareChain(t *testing.T) {
	withPreviewAuth(t, false)
	s, grant := previewServerAt(t, "127.0.0.1")
	s.staticFS = fakeStaticFS{files: map[string]string{"index.html": "<html>SPA</html>"}}
	s.staticHandler = http.FileServer(http.FS(s.staticFS))

	mux := http.NewServeMux()
	mux.Handle(preview.Prefix, s.preview)
	handler := s.corsMiddleware(s.uiHandler(s.basicAuthMiddleware(s.authMiddleware(mux))))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, preview.Prefix+grant.Token+"/index.html", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected the preview to be served, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "SPA") {
		t.Fatal("the SPA fallback swallowed the preview route")
	}
	if !strings.Contains(body, "secret design") {
		t.Fatalf("artifact content missing: %q", body)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("the preview CSP must survive the middleware chain")
	}

	// A path the preview server does not own still reaches the SPA.
	spa := httptest.NewRecorder()
	handler.ServeHTTP(spa, httptest.NewRequest(http.MethodGet, "/design", nil))
	if !strings.Contains(spa.Body.String(), "SPA") {
		t.Fatalf("the SPA must still answer its own routes, got %q", spa.Body.String())
	}
}

func TestExportDownloadStaysInsideTheArtifactDirectory(t *testing.T) {
	root := t.TempDir()
	artifactDir := filepath.Join(root, "designer", "deck")
	if err := os.MkdirAll(filepath.Join(artifactDir, "exports"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	allowed := []string{
		filepath.Join(artifactDir, "exports", "deck.pdf"),
		"designer/deck/exports/deck.pdf",
	}
	for _, rel := range allowed {
		if _, err := resolveArtifactExport(artifactDir, root, rel); err != nil {
			t.Fatalf("%q should resolve inside the artifact: %v", rel, err)
		}
	}

	refused := []string{
		"../../secrets.env",
		"designer/other/exports/deck.pdf",
		"/etc/passwd",
		filepath.Join(root, "designer", "deck-evil", "x.pdf"),
	}
	for _, rel := range refused {
		if _, err := resolveArtifactExport(artifactDir, root, rel); err == nil {
			t.Fatalf("%q must not be downloadable", rel)
		}
	}
}

func TestDesignStatusReportsWhyThePreviewIsUnavailable(t *testing.T) {
	withPreviewAuth(t, false)
	cfg := config.Get()
	config.SetForTests(cfg)

	s := &Server{config: ServerConfig{Host: "127.0.0.1"}, bindHost: "127.0.0.1", initialHost: "127.0.0.1"}
	rec := httptest.NewRecorder()
	s.handleDesignStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/design/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d", rec.Code)
	}
	var off DesignStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &off); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !off.Enabled {
		t.Fatalf("design status must stay enabled for client compatibility, got %+v", off)
	}
	if off.Preview {
		t.Fatalf("a server with no preview mounted must not report preview availability, got %+v", off)
	}
	if len(off.Kinds) != 2 {
		t.Fatalf("the kinds a client may create are always reported, got %v", off.Kinds)
	}

	// Exposed without credentials: preview is false *with a reason*, which is
	// what the canvas renders instead of a blank frame.
	exposed, _ := previewServerAt(t, "0.0.0.0")
	rec = httptest.NewRecorder()
	exposed.handleDesignStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/design/status", nil))
	var guarded DesignStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &guarded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !guarded.Enabled {
		t.Fatal("the subsystem is on and must be reported as such")
	}
	if guarded.Preview {
		t.Fatal("an exposed listener with no basic auth must not report a usable preview")
	}
	if !strings.Contains(guarded.PreviewReason, "basic auth") {
		t.Fatalf("the reason must be actionable, got %q", guarded.PreviewReason)
	}
}
