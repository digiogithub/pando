package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/design/preview"
)

const studioFixture = `<!doctype html>
<html><head><title>Studio Fixture</title></head>
<body>
  <section id="hero"><h1>Ship design faster</h1></section>
</body>
</html>
`

// designStudioServer stands up the real handlers over a real design provider in
// a temporary project, and returns the server plus the artifact the Studio will
// drive. Nothing is mocked: these are the endpoints the Web UI calls.
func designStudioServer(t *testing.T) (*Server, design.Artifact) {
	t.Helper()

	project := t.TempDir()
	dataDir := filepath.Join(project, ".pando", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	previousCfg := config.Get()
	cfg := &config.Config{WorkingDir: project}
	cfg.Data.Directory = dataDir
	cfg.Design.OutputDir = "designer"
	cfg.Design.SystemDir = "_system"
	config.SetForTests(cfg)
	t.Cleanup(func() { config.SetForTests(previousCfg) })

	conn, err := sql.Open("sqlite3", filepath.Join(dataDir, "pando.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	goose.SetBaseFS(db.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	provider, err := design.NewProvider(conn)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	previousProvider := design.DefaultProvider()
	design.SetDefaultProvider(provider)
	t.Cleanup(func() {
		provider.Close()
		design.SetDefaultProvider(previousProvider)
		design.ClosePreviewServer()
	})

	s := &Server{
		config:      ServerConfig{Host: "127.0.0.1", Port: 45231, StartupMode: "app"},
		token:       "test-token",
		bindHost:    "127.0.0.1",
		initialHost: "127.0.0.1",
	}
	s.setupPreview()
	if s.preview == nil {
		t.Fatal("the preview server must be mounted for the Design Studio")
	}

	// The agent creates artifacts through the tools; the Studio only ever reads
	// and iterates, so the fixture is seeded through the service directly.
	svc := provider.Service("ses_studio")
	artifact, err := svc.Create(t.Context(), design.CreateParams{
		Title: "Studio Landing",
		Kind:  design.KindWeb,
		Files: map[string]string{"index.html": studioFixture},
	})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	return s, artifact
}

// call runs one request through the design routes and decodes the JSON body.
func call[T any](t *testing.T, s *Server, method, path string, body string, out *T) int {
	t.Helper()
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("{}")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("X-Pando-Token", s.token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec.Code
}

// The loop P4 exists to make possible, proven at the HTTP contract the Studio
// is built on: list, open, look at the served canvas, read the structure index,
// walk the version timeline and export. The Web UI adds pixels on top of
// exactly these calls.
func TestDesignStudioHTTPLoop(t *testing.T) {
	s, artifact := designStudioServer(t)

	// The section announces itself.
	var status DesignStatusResponse
	if code := call(t, s, http.MethodGet, "/api/v1/design/status", "", &status); code != http.StatusOK {
		t.Fatalf("status: %d", code)
	}
	if !status.Enabled || !status.Preview {
		t.Fatalf("a loopback server with design on must offer previews: %+v", status)
	}

	// The gallery.
	var list struct {
		Artifacts []DesignArtifactResponse `json:"artifacts"`
	}
	if code := call(t, s, http.MethodGet, "/api/v1/design/artifacts", "", &list); code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	if len(list.Artifacts) != 1 || list.Artifacts[0].ID != artifact.ID {
		t.Fatalf("the gallery must show the artifact, got %+v", list.Artifacts)
	}

	// Opening one hands the canvas a bridged URL and the "open externally" link.
	var opened DesignArtifactResponse
	if code := call(t, s, http.MethodGet, "/api/v1/design/artifacts/"+artifact.ID, "", &opened); code != http.StatusOK {
		t.Fatalf("get: %d", code)
	}
	if !strings.HasPrefix(opened.URL, "http://127.0.0.1:45231"+preview.Prefix) {
		t.Fatalf("the canvas needs a served URL, got %q", opened.URL)
	}
	if !strings.Contains(opened.BridgeURL, "bridge=1") {
		t.Fatalf("the canvas needs the bridge, got %q", opened.BridgeURL)
	}
	if opened.Entry == "" {
		t.Fatal("the entry document must be reported")
	}

	// That URL actually serves the artifact through the mounted route.
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	rec := httptest.NewRecorder()
	previewPath := strings.TrimPrefix(opened.BridgeURL, "http://127.0.0.1:45231")
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, previewPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("the canvas URL answered %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Ship design faster") {
		t.Fatal("the served canvas is not the artifact")
	}
	if !strings.Contains(rec.Body.String(), preview.BridgePath) {
		t.Fatal("the bridged canvas must carry the selection bridge")
	}

	// The version timeline, and a checkout that comes back to version 1.
	var versions struct {
		Versions []design.Version `json:"versions"`
	}
	if code := call(t, s, http.MethodGet, "/api/v1/design/artifacts/"+artifact.ID+"/versions", "", &versions); code != http.StatusOK {
		t.Fatalf("versions: %d", code)
	}
	if len(versions.Versions) != 1 || versions.Versions[0].Number != 1 {
		t.Fatalf("expected one version, got %+v", versions.Versions)
	}
	var checkedOut DesignArtifactResponse
	if code := call(t, s, http.MethodPost, "/api/v1/design/artifacts/"+artifact.ID+"/checkout", `{"version":1}`, &checkedOut); code != http.StatusOK {
		t.Fatalf("checkout: %d", code)
	}
	if checkedOut.CurrentVersion != 1 {
		t.Fatalf("checkout reported version %d", checkedOut.CurrentVersion)
	}

	// A bad version is refused rather than silently ignored.
	if code := call[struct{}](t, s, http.MethodPost, "/api/v1/design/artifacts/"+artifact.ID+"/checkout", `{"version":0}`, nil); code != http.StatusBadRequest {
		t.Fatalf("version 0 must be refused, got %d", code)
	}

	// The inspector reads the stored index. It is legitimately empty before a
	// render, and the panel says so rather than pretending to have failed.
	var nodes design.InspectResult
	if code := call(t, s, http.MethodGet, "/api/v1/design/artifacts/"+artifact.ID+"/nodes", "", &nodes); code != http.StatusOK {
		t.Fatalf("nodes: %d", code)
	}
	if nodes.Total != 0 {
		t.Fatalf("nothing has been rendered yet, got %d nodes", nodes.Total)
	}

	// The export menu writes a self-contained HTML file and hands back a link
	// that resolves through the download route.
	var exported struct {
		Export      design.ExportResult `json:"export"`
		DownloadURL string              `json:"download_url"`
	}
	if code := call(t, s, http.MethodPost, "/api/v1/design/artifacts/"+artifact.ID+"/export", `{"format":"html"}`, &exported); code != http.StatusOK {
		t.Fatalf("export: %d", code)
	}
	if exported.Export.Bytes == 0 || exported.DownloadURL == "" {
		t.Fatalf("export produced nothing usable: %+v", exported)
	}

	download := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, exported.DownloadURL, nil)
	req.Header.Set("X-Pando-Token", s.token)
	mux.ServeHTTP(download, req)
	if download.Code != http.StatusOK {
		t.Fatalf("download answered %d (%s)", download.Code, download.Body.String())
	}
	if !strings.Contains(download.Body.String(), "Ship design faster") {
		t.Fatal("the downloaded export is not the artifact")
	}
	if disposition := download.Header().Get("Content-Disposition"); !strings.Contains(disposition, "attachment") {
		t.Fatalf("an export must download, not render: %q", disposition)
	}
}

// A design route must refuse an artifact that does not exist rather than
// answering with an empty Studio.
func TestDesignRoutesRejectUnknownArtifacts(t *testing.T) {
	s, _ := designStudioServer(t)
	for _, path := range []string{
		"/api/v1/design/artifacts/dsg_missing",
		"/api/v1/design/artifacts/dsg_missing/versions",
		"/api/v1/design/artifacts/dsg_missing/nodes",
	} {
		if code := call[struct{}](t, s, http.MethodGet, path, "", nil); code != http.StatusNotFound {
			t.Fatalf("%s answered %d, want 404", path, code)
		}
	}
}

const studioDeckFixture = `<!doctype html>
<html><head><title>Studio Deck</title><style>
  body { margin: 0; }
  .slide { width: 1280px; height: 720px; }
  @page { size: 1280px 720px; margin: 0; }
  @media print { .slide { break-after: page; } .slide:last-child { break-after: auto; } }
</style></head>
<body>
  <section class="slide"><h1 id="t0">Title slide</h1></section>
  <section class="slide"><h2 id="t1">Second slide</h2></section>
  <section class="slide"><h2 id="t2">Third slide</h2></section>
</body></html>`

// Deck mode over the same HTTP contract: render fills the slide count the slide
// strip is built from, the inspector index carries slide attribution, and the
// export menu produces a real PDF. Skipped where no headless browser exists —
// every step here needs one.
func TestDesignStudioDeckLoop(t *testing.T) {
	s, _ := designStudioServer(t)

	svc, err := design.ServiceFor("ses_studio")
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if renderer := svc.Renderer(); renderer == nil || !renderer.Available() {
		t.Skip("no Chromium-family browser available")
	}
	deck, err := svc.Create(t.Context(), design.CreateParams{
		Title: "Studio Deck",
		Kind:  design.KindDeck,
		Files: map[string]string{"index.html": studioDeckFixture},
	})
	if err != nil {
		t.Fatalf("create deck: %v", err)
	}

	var rendered map[string]any
	if code := call(t, s, http.MethodPost, "/api/v1/design/artifacts/"+deck.ID+"/render", "{}", &rendered); code != http.StatusOK {
		t.Fatalf("render: %d", code)
	}
	if slides, _ := rendered["slides"].(float64); slides < 3 {
		t.Fatalf("the slide strip needs a slide count, got %v", rendered["slides"])
	}
	if _, hasNodes := rendered["nodes"]; hasNodes && rendered["nodes"] != nil {
		t.Fatal("the render response must not repeat the node array the inspector pages separately")
	}

	// The artifact response now carries the slide count the strip renders.
	var opened DesignArtifactResponse
	if code := call(t, s, http.MethodGet, "/api/v1/design/artifacts/"+deck.ID, "", &opened); code != http.StatusOK {
		t.Fatalf("get: %d", code)
	}
	if opened.Slides < 3 {
		t.Fatalf("expected 3 slides, got %d", opened.Slides)
	}

	// The inspector index exists and attributes nodes to slides.
	var nodes design.InspectResult
	if code := call(t, s, http.MethodGet, "/api/v1/design/artifacts/"+deck.ID+"/nodes?limit=500", "", &nodes); code != http.StatusOK {
		t.Fatalf("nodes: %d", code)
	}
	if nodes.Total == 0 {
		t.Fatal("a rendered deck must have an index")
	}
	maxSlide := 0
	for _, node := range nodes.Nodes {
		if node.Slide > maxSlide {
			maxSlide = node.Slide
		}
	}
	if maxSlide == 0 {
		t.Fatal("deck nodes must carry slide attribution, or the strip cannot filter them")
	}

	// Export the deck as a PDF and pull it through the download route.
	var exported struct {
		Export      design.ExportResult `json:"export"`
		DownloadURL string              `json:"download_url"`
	}
	if code := call(t, s, http.MethodPost, "/api/v1/design/artifacts/"+deck.ID+"/export", `{"format":"pdf"}`, &exported); code != http.StatusOK {
		t.Fatalf("export: %d", code)
	}
	if exported.Export.Note != "" {
		t.Fatalf("this deck has print breaks, so it must export without a warning: %q", exported.Export.Note)
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	download := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, exported.DownloadURL, nil)
	req.Header.Set("X-Pando-Token", s.token)
	mux.ServeHTTP(download, req)
	if download.Code != http.StatusOK {
		t.Fatalf("download answered %d", download.Code)
	}
	if !strings.HasPrefix(download.Body.String(), "%PDF") {
		t.Fatal("the downloaded deck export is not a PDF")
	}
}
