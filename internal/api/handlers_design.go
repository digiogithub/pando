package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/design/preview"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/skills"
	"github.com/digiogithub/pando/internal/skills/catalog"
)

// designService resolves a session-bound design service, reporting the same
// "not available" condition the tools report when this process has no design
// provider wired.
func (s *Server) designService(r *http.Request) (*design.Service, error) {
	return design.ServiceFor(r.URL.Query().Get("session_id"))
}

// writeDesignError maps a design error onto a status code. A missing provider is
// 503 rather than 500: nothing is broken, this process just cannot serve design
// requests.
func writeDesignError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, design.ErrNoProvider):
		writeError(w, http.StatusServiceUnavailable, "design subsystem is not available in this process")
	case errors.Is(err, design.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, design.ErrBundleInstalled):
		// Not a bad request: the caller asked for something reasonable and the
		// answer is that a copy is already there, which force can override.
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, design.ErrNoBrowser):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// DesignArtifactResponse is one artifact plus everything a surface needs to
// show it without a second round trip.
type DesignArtifactResponse struct {
	design.Artifact
	// URL, BridgeURL and FileURL come from the presentation; they are empty
	// when the entry document is missing (a half-deleted artifact directory).
	URL       string `json:"url,omitempty"`
	BridgeURL string `json:"bridge_url,omitempty"`
	FileURL   string `json:"file_url,omitempty"`
	Slides    int    `json:"slides,omitempty"`
	Entry     string `json:"entry,omitempty"`
}

// DesignStatusResponse tells a client whether the Design Studio is usable in
// this process, and with what.
type DesignStatusResponse struct {
	Enabled bool `json:"enabled"`
	// Preview reports that artifacts can be served over HTTP. It is false when
	// the guard refuses (an exposed listener with no basic auth), which is a
	// different failure from the subsystem being off.
	Preview bool `json:"preview"`
	// PreviewReason explains a false Preview so the UI can say why instead of
	// showing an empty frame.
	PreviewReason string `json:"preview_reason,omitempty"`
	// Renderer reports that a headless browser was found, without which render,
	// screenshot and export do nothing.
	Renderer  bool     `json:"renderer"`
	Kinds     []string `json:"kinds"`
	OutputDir string   `json:"output_dir,omitempty"`
}

// handleDesignStatus reports the availability of the Design Studio. It is the
// one design route registered unconditionally: the Web UI has to be able to ask
// whether to show the section at all, and a 404 is not an answer it can tell
// apart from an old server.
//
// GET /api/v1/design/status
func (s *Server) handleDesignStatus(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	status := DesignStatusResponse{
		Enabled: true,
		Kinds:   []string{string(design.KindWeb), string(design.KindDeck)},
	}
	if cfg != nil {
		status.OutputDir = cfg.Design.OutputDir
	}

	if err := s.previewAccess(); err != nil {
		status.PreviewReason = err.Error()
	} else {
		status.Preview = s.preview != nil
	}
	if svc, err := design.ServiceFor(""); err == nil {
		if renderer := svc.Renderer(); renderer != nil {
			status.Renderer = renderer.Available()
		}
	}
	writeJSON(w, http.StatusOK, status)
}

// DesignCanvasResponse is the address of the read-only design canvas.
type DesignCanvasResponse struct {
	URL string `json:"url"`
	// Artboards is how many artifacts the canvas currently holds, so a caller
	// can tell an empty canvas from a full one before opening a window.
	Artboards int `json:"artboards"`
}

// handleDesignCanvas publishes the canvas of a session and returns its address.
// The UI opens it in a separate window rather than framing it: the canvas is
// the whole surface, and it frames the artifacts itself.
//
// GET /api/v1/design/canvas[?session_id=…]
func (s *Server) handleDesignCanvas(w http.ResponseWriter, r *http.Request) {
	if err := s.previewAccess(); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	url, err := design.CanvasPresentation(sessionID)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	response := DesignCanvasResponse{URL: url}
	if boards, err := design.CanvasArtboards(sessionID); err == nil {
		response.Artboards = len(boards)
	}
	writeJSON(w, http.StatusOK, response)
}

// handleDesignArtifacts lists artifacts.
//
// GET /api/v1/design/artifacts[?session_only=1]
func (s *Server) handleDesignArtifacts(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	artifacts, err := svc.List(r.Context(), r.URL.Query().Get("session_only") == "1")
	if err != nil {
		writeDesignError(w, err)
		return
	}
	out := make([]DesignArtifactResponse, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, s.decorateArtifact(r, svc, artifact))
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": out})
}

// decorateArtifact attaches presentation data, tolerating a broken artifact
// directory: a listing must never fail because one entry document was deleted.
func (s *Server) decorateArtifact(r *http.Request, svc *design.Service, artifact design.Artifact) DesignArtifactResponse {
	out := DesignArtifactResponse{Artifact: artifact}
	presentation, err := svc.Presentation(r.Context(), artifact.ID, 0, "")
	if err != nil {
		return out
	}
	out.URL, out.BridgeURL, out.FileURL = presentation.URL, presentation.BridgeURL, presentation.FileURL
	out.Slides, out.Entry = presentation.Slides, presentation.Entry
	return out
}

// handleDesignArtifact returns one artifact.
//
// GET /api/v1/design/artifacts/{id}
func (s *Server) handleDesignArtifact(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	artifact, err := svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.decorateArtifact(r, svc, artifact))
}

// handleDesignVersions lists the version history of an artifact.
//
// GET /api/v1/design/artifacts/{id}/versions
func (s *Server) handleDesignVersions(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	versions, err := svc.Versions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// handleDesignCheckout restores an artifact directory to a previous version.
//
// POST /api/v1/design/artifacts/{id}/checkout {"version": 2}
func (s *Server) handleDesignCheckout(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	var body struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Version < 1 {
		writeError(w, http.StatusBadRequest, "version must be 1 or greater")
		return
	}
	if err := svc.Checkout(r.Context(), r.PathValue("id"), body.Version); err != nil {
		writeDesignError(w, err)
		return
	}
	artifact, err := svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.decorateArtifact(r, svc, artifact))
}

// handleDesignNodes returns a page of the stored structure index.
//
// GET /api/v1/design/artifacts/{id}/nodes?version=&page=&per_page=&styles=1
func (s *Server) handleDesignNodes(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	query := r.URL.Query()
	opts := design.InspectOptions{
		NodeID:        query.Get("node_id"),
		Selector:      query.Get("selector"),
		Text:          query.Get("text"),
		Slide:         atoiDefault(query.Get("slide"), -1),
		Depth:         atoiDefault(query.Get("depth"), 0),
		Offset:        atoiDefault(query.Get("offset"), 0),
		Limit:         atoiDefault(query.Get("limit"), 0),
		IncludeStyles: query.Get("styles") == "1",
	}
	result, err := svc.Inspect(r.Context(), r.PathValue("id"), atoiDefault(query.Get("version"), 0), opts)
	if errors.Is(err, design.ErrNoIndex) {
		// Not an error for a UI: an artifact that has never been rendered has
		// an empty index, and the inspector says so itself.
		writeJSON(w, http.StatusOK, design.InspectResult{ArtifactID: r.PathValue("id"), NextOffset: -1})
		return
	}
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleDesignRender re-renders an artifact and refreshes its index. The Studio
// calls it after an edit; the SSE stream then tells every other surface.
//
// POST /api/v1/design/artifacts/{id}/render
func (s *Server) handleDesignRender(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	var body struct {
		Width  int  `json:"width"`
		Height int  `json:"height"`
		Reload bool `json:"reload"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	opts := design.RenderOptions{}
	if body.Width > 0 && body.Height > 0 {
		opts.Viewport = design.Viewport{W: body.Width, H: body.Height}
	}
	result, err := svc.Render(r.Context(), r.PathValue("id"), opts)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	// The node array is the expensive part and the Studio pages it from
	// /nodes; sending it here would duplicate it on every render.
	result.Nodes = nil
	writeJSON(w, http.StatusOK, result)
}

// handleDesignScreenshot returns a PNG of the artifact.
//
// GET /api/v1/design/artifacts/{id}/screenshot?slide=&full=1
func (s *Server) handleDesignScreenshot(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	artifact, err := svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDesignError(w, err)
		return
	}
	renderer := svc.Renderer()
	if renderer == nil {
		writeDesignError(w, design.ErrNoBrowser)
		return
	}
	query := r.URL.Query()
	png, err := renderer.Screenshot(r.Context(), artifact, design.ScreenshotOptions{
		RenderOptions: design.RenderOptions{},
		FullPage:      query.Get("full") == "1",
		Slide:         atoiDefault(query.Get("slide"), 0),
	})
	if err != nil {
		writeDesignError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// handleDesignExport writes an export and returns where it landed. The bytes
// themselves are fetched from the download endpoint, so a large PDF never
// travels through a JSON body.
//
// POST /api/v1/design/artifacts/{id}/export {"format":"pdf"}
func (s *Server) handleDesignExport(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	var body struct {
		Format    string `json:"format"`
		Slide     int    `json:"slide"`
		FullPage  bool   `json:"full_page"`
		Landscape bool   `json:"landscape"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Format == "" {
		body.Format = design.ExportHTML
	}
	result, err := svc.Export(r.Context(), r.PathValue("id"), design.ExportOptions{
		Format:    body.Format,
		Slide:     body.Slide,
		FullPage:  body.FullPage,
		Landscape: body.Landscape,
	})
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"export":       result,
		"download_url": fmt.Sprintf("/api/v1/design/artifacts/%s/download?path=%s", r.PathValue("id"), url.QueryEscape(result.Path)),
	})
}

// handleDesignDownload streams a previously exported file. The path is checked
// against the artifact's own directory, so this endpoint can never be turned
// into a file-read primitive for the rest of the project.
//
// GET /api/v1/design/artifacts/{id}/download?path=designer/deck/exports/deck.pdf
func (s *Server) handleDesignDownload(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	artifact, err := svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDesignError(w, err)
		return
	}
	absDir, err := svc.AbsDir(artifact)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	workingDir := ""
	if cfg := config.Get(); cfg != nil {
		workingDir = cfg.WorkingDir
	}
	target, err := resolveArtifactExport(absDir, workingDir, rel)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "export not found")
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(target)+"\"")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, target)
}

// resolveArtifactExport turns a requested export path into an absolute file
// inside the artifact directory, or refuses it. Downloads are the one design
// endpoint that names a file, so this is the check that keeps it from becoming
// a way to read the rest of the project.
func resolveArtifactExport(absDir, workingDir, rel string) (string, error) {
	target := filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsAbs(target) && workingDir != "" {
		target = filepath.Join(workingDir, target)
	}
	root, err := filepath.Abs(absDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the artifact directory")
	}
	resolved, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q", rel)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside the artifact directory")
	}
	return resolved, nil
}

// handleDesignEvents streams design lifecycle events.
//
// GET /api/v1/design/events
func (s *Server) handleDesignEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	events := design.Events().Subscribe(ctx)

	fmt.Fprintf(w, "event: connected\ndata: {\"ts\":%d}\n\n", time.Now().UnixMilli())
	flusher.Flush()

	// A design session can sit idle for a long time while the agent thinks;
	// a periodic comment keeps proxies from closing the stream.
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			payload, err := json.Marshal(event.Payload)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Payload.Kind, payload)
			flusher.Flush()
		}
	}
}

// setupPreview installs the design preview server on this listener. Previews
// are served from the Pando origin so they inherit its bind address and its
// basic-auth gate; see previewAccess for the rule that keeps them off an
// unauthenticated network-facing listener.
func (s *Server) setupPreview() {
	server := preview.New(design.PreviewOptions(s.previewBaseURL, s.previewAccess))
	s.preview = server
	design.SetPreviewServer(server)
	logging.Debug("design: preview mounted on the API listener", "prefix", preview.Prefix)
}

// previewBaseURL is the origin absolute preview URLs are built on. A wildcard
// bind has no address of its own, so it resolves to loopback: the absolute URL
// exists for opening a browser on this machine, while remote clients use the
// path against their own origin.
func (s *Server) previewBaseURL() string {
	scheme := "http"
	if s.config.TLSCertFile != "" && s.config.TLSKeyFile != "" {
		scheme = "https"
	}
	host := s.BindHost()
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, s.config.Port)
}

// previewAccess refuses to serve previews from a listener that is reachable
// from the network without credentials in front of it.
//
// The API surface is already protected by basicAuthMiddleware, but /preview is
// not under /api: without this guard, turning external access on would publish
// every design artifact — and every file in its directory — to the whole
// network. The rule is deliberately the strict one: a non-loopback bind
// requires configured, enabled basic auth, whatever the request looks like.
func (s *Server) previewAccess() error {
	if isLoopbackHost(s.BindHost()) {
		return nil
	}
	cfg := config.Get()
	if cfg != nil && cfg.Server.BasicAuth.Enabled && len(cfg.Server.BasicAuth.Users) > 0 {
		return nil
	}
	return fmt.Errorf("%w: design previews are disabled while this server is bound to %s with no basic auth configured; enable basic auth in Settings > Access, or bind to loopback",
		preview.ErrForbidden, s.BindHost())
}

// DesignSystemResponse is the design system plus everything a settings screen
// needs to explain it: whether it was ever committed, and where its files live.
type DesignSystemResponse struct {
	System design.DesignSystem `json:"system"`
	// Exists is false when the project has never committed a system and the
	// values shown are the defaults.
	Exists     bool   `json:"exists"`
	Tokens     string `json:"tokens_path"`
	Stylesheet string `json:"stylesheet_path"`
	Contract   string `json:"contract_path"`
}

// designSystemResponse builds the payload shared by every system route.
func designSystemResponse(svc *design.Service, ds design.DesignSystem, exists bool) DesignSystemResponse {
	return DesignSystemResponse{
		System:     ds,
		Exists:     exists,
		Tokens:     svc.SystemRelPath(design.SystemTokensFile),
		Stylesheet: svc.SystemRelPath(design.SystemStylesheet),
		Contract:   svc.SystemRelPath(design.SystemContractFile),
	}
}

// handleDesignSystem returns the design system shared by this project.
//
// GET /api/v1/design/system
func (s *Server) handleDesignSystem(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	ds, exists, err := svc.LoadSystem()
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, designSystemResponse(svc, ds, exists))
}

// handleDesignSystemUpdate merges tokens into the design system.
//
// PUT /api/v1/design/system {"name": "...", "tokens": {...}, "fonts": [...]}
func (s *Server) handleDesignSystemUpdate(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	var body struct {
		Name   string                       `json:"name"`
		Tokens map[string]map[string]string `json:"tokens"`
		Fonts  []string                     `json:"fonts"`
		// ReplaceFonts distinguishes "leave the fonts alone" from "clear them":
		// an omitted array and an empty array are the same value in JSON.
		ReplaceFonts bool `json:"replace_fonts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.ReplaceFonts {
		ds, _, err := svc.LoadSystem()
		if err != nil {
			writeDesignError(w, err)
			return
		}
		ds.Fonts = body.Fonts
		if _, _, err := svc.SaveSystem(ds); err != nil {
			writeDesignError(w, err)
			return
		}
	}
	updated, err := svc.SetSystemTokens(body.Name, body.Tokens)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, designSystemResponse(svc, updated, true))
}

// handleDesignSystemExamples lists the bundled style guides.
//
// GET /api/v1/design/system/examples
func (s *Server) handleDesignSystemExamples(w http.ResponseWriter, r *http.Request) {
	names := design.ExampleSystemNames()
	out := make([]map[string]string, 0, len(names))
	for _, name := range names {
		out = append(out, map[string]string{"name": name, "title": design.ExampleSystemTitle(name)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"examples": out})
}

// handleDesignSystemExtract builds the design system from a source and writes
// it, unless the caller asked for a dry run.
//
// POST /api/v1/design/system/extract {"source": "url", "target": "https://…"}
func (s *Server) handleDesignSystemExtract(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	var body struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Name   string `json:"name"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := svc.ExtractSystem(r.Context(), design.ExtractOptions{
		Source: design.ExtractSource(strings.ToLower(strings.TrimSpace(body.Source))),
		Target: body.Target,
		Name:   body.Name,
	})
	if err != nil {
		writeDesignError(w, err)
		return
	}
	payload := map[string]any{"result": result, "saved": false}
	if body.DryRun {
		writeJSON(w, http.StatusOK, payload)
		return
	}
	if _, _, err := svc.SaveSystem(result.System); err != nil {
		writeDesignError(w, err)
		return
	}
	payload["saved"] = true
	payload["system"] = designSystemResponse(svc, result.System, true)
	if mirrored, mirrorErr := svc.MirrorSystem(r.Context(), result.System, result.Source, result.Target); mirrorErr != nil {
		// The mirror is a convenience; reporting the failure is enough.
		logging.Warn("design: mirror design system", "error", mirrorErr)
		payload["mirror_error"] = mirrorErr.Error()
	} else if mirrored != "" {
		payload["mirrored"] = mirrored
	}
	writeJSON(w, http.StatusOK, payload)
}

// handleDesignSystemApply links the stylesheet into an artifact and audits it.
//
// POST /api/v1/design/artifacts/{id}/apply-system
func (s *Server) handleDesignSystemApply(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	result, err := svc.ApplySystem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleDesignCritique runs a quality pass over an artifact and returns the
// findings, the score and the gate decision. It never edits the artifact, so a
// panel may call it as freely as it renders.
//
// POST /api/v1/design/artifacts/{id}/critique
func (s *Server) handleDesignCritique(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}

	body := struct {
		Version    int            `json:"version"`
		Width      int            `json:"width"`
		Height     int            `json:"height"`
		Policy     string         `json:"policy"`
		Round      int            `json:"round"`
		SkipRender bool           `json:"skip_render"`
		Record     *bool          `json:"record"`
		Score      float64        `json:"score"`
		Summary    string         `json:"summary"`
		Issues     []design.Issue `json:"issues"`
	}{}
	if r.Body != nil {
		// An empty body is the ordinary case: critique the current version
		// with the artifact's own policy.
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	record := true
	if body.Record != nil {
		record = *body.Record
	}

	report, err := svc.Critique(r.Context(), r.PathValue("id"), design.CritiqueOptions{
		Version:    body.Version,
		Render:     design.RenderOptions{Viewport: design.Viewport{W: body.Width, H: body.Height}},
		SkipRender: body.SkipRender,
		Round:      body.Round,
		Policy:     body.Policy,
		Score:      body.Score,
		Summary:    body.Summary,
		Issues:     body.Issues,
		Record:     record,
	})
	if err != nil {
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleDesignLatestCritique reads back the last recorded pass over a version,
// which is what the inspector panel shows before anyone asks for a new one.
//
// GET /api/v1/design/artifacts/{id}/critique?version=N
func (s *Server) handleDesignLatestCritique(w http.ResponseWriter, r *http.Request) {
	svc, err := s.designService(r)
	if err != nil {
		writeDesignError(w, err)
		return
	}
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))

	artifact, err := svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDesignError(w, err)
		return
	}
	settings := svc.CritiqueSettingsFor(artifact.SkillID)

	critique, err := svc.LatestCritique(r.Context(), artifact.ID, version)
	if err != nil {
		if errors.Is(err, design.ErrNotFound) {
			// Never critiqued is not an error: the panel shows an empty state
			// and an invitation to run one.
			writeJSON(w, http.StatusOK, map[string]any{
				"exists":   false,
				"settings": settings,
			})
			return
		}
		writeDesignError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exists":   true,
		"critique": critique,
		"settings": settings,
		"decision": settings.Gate(critique, critique.Version),
	})
}

// designGallery merges the bundled design bundles with the ones already present
// in the skill discovery roots, using the same discovery the agent's skill
// loader uses so the gallery never lists something the agent cannot read.
func designGallery() ([]design.Template, error) {
	workDir := config.WorkingDirectory()
	var extraPaths []string
	if cfg := config.Get(); cfg != nil {
		extraPaths = cfg.Skills.Paths
	}
	discovered, err := skills.DiscoverSkills(skills.ConfiguredDiscoveryPaths(workDir, extraPaths))
	if err != nil {
		return nil, err
	}

	installed := make([]design.Template, 0, len(discovered))
	for _, sk := range discovered {
		t, ok := design.TemplateFromSkill(sk.Metadata.Name, sk.Metadata.Description, sk.Metadata.OD)
		if !ok {
			continue
		}
		t.SourcePath = sk.SourcePath
		installed = append(installed, t)
	}
	return design.Gallery(installed), nil
}

// handleDesignSkills lists the design gallery: every bundle a user can start an
// artifact from, plus the craft references and workflows that are not startable
// but are part of the same library.
//
// GET /api/v1/design/skills
func (s *Server) handleDesignSkills(w http.ResponseWriter, r *http.Request) {
	gallery, err := designGallery()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to discover design skills: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skills": gallery,
		"craft":  design.CraftReferenceNames(),
	})
}

// handleDesignSkillInstall copies a bundled design skill into a skills root so
// the agent's skill loader picks it up. The gallery works without installing;
// installing is what makes the bundle available to the model.
//
// POST /api/v1/design/skills/{name}/install {"scope": "project"|"global"}
func (s *Server) handleDesignSkillInstall(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var body struct {
		Scope string `json:"scope"`
		Force bool   `json:"force"`
	}
	if r.Body != nil {
		// An empty body is a valid request: the default scope is the project,
		// because a design bundle belongs to the project it designs.
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	projectLocal := !strings.EqualFold(strings.TrimSpace(body.Scope), "global")

	targetDir := catalog.ResolveSkillsDir(projectLocal)
	if !filepath.IsAbs(targetDir) {
		targetDir = filepath.Join(config.WorkingDirectory(), targetDir)
	}
	targetDir = filepath.Join(targetDir, "design")

	written, err := design.InstallBundle(name, targetDir, body.Force)
	if err != nil {
		writeDesignError(w, err)
		return
	}

	scope := "project"
	if !projectLocal {
		scope = "global"
	}
	relative := make([]string, 0, len(written))
	for _, p := range written {
		if rel, relErr := filepath.Rel(config.WorkingDirectory(), p); relErr == nil && !strings.HasPrefix(rel, "..") {
			relative = append(relative, filepath.ToSlash(rel))
			continue
		}
		relative = append(relative, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":  name,
		"scope": scope,
		"dir":   targetDir,
		"files": relative,
	})
}
