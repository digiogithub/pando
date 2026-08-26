// Package preview serves design artifacts over HTTP so every surface — the
// WebUI iframe, a system browser opened from the TUI, a Zed resource link —
// looks at the same running document instead of a file:// copy.
//
// The package deliberately knows nothing about the design model: it is handed a
// directory and an entry document and hands back a URL. That keeps it free of
// any dependency on internal/design, so internal/design can import it to mint
// URLs while internal/api mounts the very same handler on the main listener.
//
// Two deployments share one implementation:
//
//   - mounted: internal/api registers [Server.ServeHTTP] under [Prefix] on the
//     API listener, so the preview lives on the Pando origin and is reachable
//     remotely through the existing external-access toggle.
//   - loopback: processes without an API server (plain TUI, ACP, CLI) call
//     [Server.StartLoopback], which binds 127.0.0.1:0 and serves the same routes.
//
// Every artifact is addressed through an unguessable per-artifact token in the
// path. The token is the capability: it is bound to the session that created it
// and dies with the grant, so a stale URL in someone's browser history stops
// resolving instead of exposing whatever now sits in that directory.
package preview

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Prefix is the single route prefix the server owns. Everything below it is
// artifact content; nothing above it is ever touched.
const Prefix = "/preview/"

// BridgePath serves the selection bridge. It sits outside the token space
// on purpose: it is a static asset with no artifact content in it, and keeping
// it token-free means one cached copy serves every artifact.
const BridgePath = Prefix + "_bridge.js"

// DefaultTTL is how long a grant stays resolvable without being refreshed.
// Presenting an artifact refreshes it, so an artifact under active iteration
// never expires while the user is looking at it.
const DefaultTTL = 12 * time.Hour

// ErrForbidden is returned by an access guard that refuses to serve. The API
// server uses it to keep previews off a network-facing listener that has no
// authentication in front of it.
var ErrForbidden = errors.New("preview: refused")

// Grant is one artifact published for viewing.
type Grant struct {
	Token      string `json:"token"`
	ArtifactID string `json:"artifact_id"`
	SessionID  string `json:"session_id,omitempty"`
	// Dir is the absolute artifact directory. Nothing outside it is served.
	Dir string `json:"-"`
	// Entry is the directory-relative default document.
	Entry     string    `json:"entry"`
	ExpiresAt time.Time `json:"expires_at"`
	revision  *atomic.Uint64
}

// Options configures a server.
type Options struct {
	// BaseURL resolves the origin to build absolute URLs with. It is a function
	// because the API server's bind address changes at runtime when the
	// external-access toggle is flipped. When it is nil or returns an empty
	// string the server falls back to its own loopback listener, and to a
	// relative URL when it has none.
	BaseURL func() string
	// Access is consulted on every request and before every publish. A non-nil
	// error refuses the operation. It exists so the API server can enforce
	// "never on a non-loopback bind without basic auth".
	Access func() error
	// TTL overrides DefaultTTL.
	TTL time.Duration
	// FrameAncestors overrides the CSP frame-ancestors list. It defaults to
	// 'self', which is right whenever the preview and the UI framing it share
	// an origin — the mounted deployment. A shell that runs the UI on its own
	// origin (the Wails desktop app) has to widen it, and doing that here keeps
	// the decision in the caller that knows its own origin.
	FrameAncestors string
	// Inject is JavaScript spliced into a ?bridge=1 document ahead of the
	// bridge itself. internal/design supplies the renderer's own element
	// walker here, so the data-pando-id a user clicks in the browser is the
	// same id the stored node index holds. Keeping it an option is what lets
	// this package stay ignorant of the design model.
	Inject []byte
}

// Server is the grant registry and the HTTP handler over it.
type Server struct {
	opts Options

	mu         sync.RWMutex
	grants     map[string]*Grant // token -> grant
	byArtifact map[string]string // artifact id -> token

	// listener and httpSrv are set only by StartLoopback.
	listener net.Listener
	httpSrv  *http.Server
	ownURL   string
}

// New builds a server. It serves nothing until an artifact is published.
func New(opts Options) *Server {
	if opts.TTL <= 0 {
		opts.TTL = DefaultTTL
	}
	return &Server{
		opts:       opts,
		grants:     make(map[string]*Grant),
		byArtifact: make(map[string]string),
	}
}

// Publish registers (or refreshes) a grant for an artifact directory and
// returns it. The token is stable for the lifetime of the grant so reloading a
// preview keeps the same URL, which is what lets an iframe survive a re-render.
func (s *Server) Publish(artifactID, sessionID, absDir, entry string) (Grant, error) {
	if err := s.checkAccess(); err != nil {
		return Grant{}, err
	}
	if artifactID == "" || absDir == "" {
		return Grant{}, errors.New("preview: artifact id and directory are required")
	}
	dir, err := filepath.Abs(absDir)
	if err != nil {
		return Grant{}, err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Grant{}, fmt.Errorf("preview: %s is not a readable directory", absDir)
	}
	if entry == "" {
		entry = "index.html"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.byArtifact[artifactID]
	if !ok {
		if token, err = newToken(); err != nil {
			return Grant{}, err
		}
		s.byArtifact[artifactID] = token
	}
	revision := &atomic.Uint64{}
	if existing := s.grants[token]; existing != nil && existing.revision != nil {
		revision = existing.revision
	}
	grant := &Grant{
		Token:      token,
		ArtifactID: artifactID,
		SessionID:  sessionID,
		Dir:        dir,
		Entry:      filepath.ToSlash(entry),
		ExpiresAt:  time.Now().Add(s.opts.TTL),
		revision:   revision,
	}
	s.grants[token] = grant
	s.sweepLocked()
	return *grant, nil
}

// URLOptions tunes the address Publish hands out.
type URLOptions struct {
	// Slide adds a #slide-N fragment (decks).
	Slide int
	// Bridge asks for the selection bridge to be injected. Only the Pando UI
	// sets it; a URL opened in a plain browser stays untouched markup.
	Bridge bool
	// Doc overrides the entry document with another directory-relative file.
	Doc string
}

// URL returns the address of a published artifact.
func (s *Server) URL(artifactID string, opts URLOptions) (string, error) {
	s.mu.RLock()
	token, ok := s.byArtifact[artifactID]
	grant := s.grants[token]
	s.mu.RUnlock()
	if !ok || grant == nil {
		return "", fmt.Errorf("preview: artifact %s is not published", artifactID)
	}

	doc := opts.Doc
	if doc == "" {
		doc = grant.Entry
	}
	u := &url.URL{Path: Prefix + token + "/" + strings.TrimPrefix(filepath.ToSlash(doc), "/")}
	if opts.Bridge {
		u.RawQuery = "bridge=1"
	}
	if opts.Slide > 0 {
		u.Fragment = fmt.Sprintf("slide-%d", opts.Slide)
	}
	return s.base() + u.String(), nil
}

// Revoke drops the grant of one artifact.
func (s *Server) Revoke(artifactID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token, ok := s.byArtifact[artifactID]; ok {
		delete(s.grants, token)
		delete(s.byArtifact, artifactID)
	}
}

// Bump increments the live-reload revision of a published artifact. It is a
// no-op when the artifact has no active grant on this server.
func (s *Server) Bump(artifactID string) {
	s.mu.RLock()
	token, ok := s.byArtifact[artifactID]
	grant := s.grants[token]
	s.mu.RUnlock()
	if !ok || grant == nil || grant.revision == nil {
		return
	}
	grant.revision.Add(1)
}

// RevokeSession drops every grant a session published. Sessions end; their
// preview URLs must stop resolving with them.
func (s *Server) RevokeSession(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, grant := range s.grants {
		if grant.SessionID == sessionID {
			delete(s.grants, token)
			delete(s.byArtifact, grant.ArtifactID)
		}
	}
}

// Grants lists the live grants, newest expiry first is not guaranteed; the
// order is unspecified. It exists for diagnostics and tests.
func (s *Server) Grants() []Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Grant, 0, len(s.grants))
	for _, g := range s.grants {
		out = append(out, *g)
	}
	return out
}

// base returns the origin absolute URLs are built on.
func (s *Server) base() string {
	if s.opts.BaseURL != nil {
		if b := strings.TrimSuffix(s.opts.BaseURL(), "/"); b != "" {
			return b
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ownURL
}

func (s *Server) checkAccess() error {
	if s.opts.Access == nil {
		return nil
	}
	return s.opts.Access()
}

// sweepLocked removes expired grants. It runs on publish rather than on a timer
// so an idle process keeps no goroutine alive for it.
func (s *Server) sweepLocked() {
	now := time.Now()
	for token, grant := range s.grants {
		if now.After(grant.ExpiresAt) {
			delete(s.grants, token)
			delete(s.byArtifact, grant.ArtifactID)
		}
	}
}

func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("preview: token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// StartLoopback binds 127.0.0.1:0 and serves the preview routes there. It is
// the fallback for processes with no API server; calling it twice is a no-op.
func (s *Server) StartLoopback() error {
	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		return nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("preview: listen: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle(Prefix, s)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.listener, s.httpSrv = listener, srv
	s.ownURL = "http://" + listener.Addr().String()
	s.mu.Unlock()

	go func() { _ = srv.Serve(listener) }()
	return nil
}

// Addr reports the loopback listener address, empty when there is none.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Close stops the loopback listener and drops every grant.
func (s *Server) Close() {
	s.mu.Lock()
	srv := s.httpSrv
	s.httpSrv, s.listener, s.ownURL = nil, nil, ""
	s.grants = make(map[string]*Grant)
	s.byArtifact = make(map[string]string)
	s.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// resolve looks a token up, rejecting expired grants.
func (s *Server) resolve(token string) (*Grant, bool) {
	s.mu.RLock()
	grant, ok := s.grants[token]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(grant.ExpiresAt) {
		s.Revoke(grant.ArtifactID)
		return nil, false
	}
	return grant, true
}

// ServeHTTP serves artifact files under Prefix.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.checkAccess(); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.URL.Path == BridgePath {
		s.serveBridge(w, r)
		return
	}

	token, rest, ok := splitPreviewPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	grant, ok := s.resolve(token)
	if !ok {
		// Deliberately indistinguishable from a missing file: a wrong token must
		// not confirm that some other artifact exists.
		http.NotFound(w, r)
		return
	}
	if rest == "" {
		rest = grant.Entry
	}
	if rest == "_live" {
		s.serveLive(w, r, grant)
		return
	}

	target, err := safeJoin(grant.Dir, rest)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	s.writeSecurityHeaders(w)
	ext := strings.ToLower(path.Ext(rest))
	if ctype := mime.TypeByExtension(ext); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	// The document is edited underneath the browser on every iteration, so it
	// must never be cached: a stale preview would make the agent look broken.
	w.Header().Set("Cache-Control", "no-store, must-revalidate")

	if ext == ".html" || ext == ".htm" {
		body, err := os.ReadFile(target)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		body = injectLiveReload(body, Prefix+grant.Token+"/_live")
		if r.URL.Query().Get("bridge") == "1" {
			body = injectBridge(body, s.opts.Inject)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, filepath.Base(target), info.ModTime(), bytes.NewReader(body))
		return
	}

	// ServeContent rather than ServeFile: ServeFile redirects any path ending
	// in /index.html to ./, which would rewrite the very URL the grant token
	// makes stable.
	file, err := os.Open(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	http.ServeContent(w, r, filepath.Base(target), info.ModTime(), file)
}

// writeSecurityHeaders locks a preview response down. The artifact is untrusted
// generated markup running on the Pando origin, so it must not be able to reach
// the API, phone home, or be framed by a third-party page.
//
// Inline script and style stay allowed: design artifacts are single-file HTML
// with inline <style> and <script> by construction, and forbidding them would
// break the very documents this serves. connect-src is limited to 'self' so the
// injected live-reload poll can reach only the token-scoped _live endpoint on
// the same origin, not arbitrary hosts or the wider API surface.
func (s *Server) writeSecurityHeaders(w http.ResponseWriter) {
	frameAncestors := s.opts.FrameAncestors
	if frameAncestors == "" {
		frameAncestors = "'self'"
	}
	w.Header().Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self' data: blob:",
		"img-src 'self' data: blob:",
		"media-src 'self' data: blob:",
		"font-src 'self' data:",
		"style-src 'self' 'unsafe-inline'",
		"script-src 'self' 'unsafe-inline'",
		"connect-src 'self'",
		"frame-ancestors " + frameAncestors,
		"base-uri 'none'",
		"form-action 'none'",
	}, "; "))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// splitPreviewPath breaks /preview/<token>/<rest> apart.
func splitPreviewPath(p string) (token, rest string, ok bool) {
	if !strings.HasPrefix(p, Prefix) {
		return "", "", false
	}
	trimmed := strings.TrimPrefix(p, Prefix)
	token, rest, _ = strings.Cut(trimmed, "/")
	if token == "" {
		return "", "", false
	}
	return token, rest, true
}

// safeJoin resolves a request path inside a directory, refusing anything that
// climbs out of it — including through a symlink, which is why the resolved
// path is compared after EvalSymlinks.
func safeJoin(dir, rel string) (string, error) {
	cleaned := path.Clean("/" + rel)
	target := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(cleaned, "/")))

	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		root = dir
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		resolved = target
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("preview: %q escapes the artifact directory", rel)
	}
	return target, nil
}

func (s *Server) serveLive(w http.ResponseWriter, r *http.Request, grant *Grant) {
	s.writeSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	revision := uint64(0)
	if grant != nil && grant.revision != nil {
		revision = grant.revision.Load()
	}
	_, _ = fmt.Fprintf(w, "%d", revision)
}
