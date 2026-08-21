package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/agui"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/extensions"
	"github.com/digiogithub/pando/internal/logging"
)

type ServerConfig struct {
	Host    string
	Port    int
	Version string
	DB      *sql.DB
	// Querier overrides the db.Querier passed to app.New. When non-nil it is
	// used instead of db.New(cfg.DB). Secondary instances supply a DBProxy here
	// so all writes are forwarded to the primary via ZMQ RPC.
	Querier     db.Querier
	CWD         string
	StaticFS    fs.FS
	OpenUI      bool
	UIBaseURL   string
	TLSCertFile string
	TLSKeyFile  string
	StartupMode string

	// IPC identity fields populated by Bootstrap so the /api/ipc/status
	// endpoint can report them without re-reading the lock file.
	InstanceID string
	Role       string
	PubPort    int
	RPCPort    int
}

type Server struct {
	httpServer    *http.Server
	app           *app.App
	config        ServerConfig
	token         string
	staticFS      fs.FS
	staticHandler http.Handler
	bgRunner      *BackgroundSessionManager
	// agui is the AG-UI protocol adapter (CopilotKit and other Generative-UI
	// frontends). It is nil unless [AGUI] Enabled is set. It runs its own agent
	// instances and its own permission service, so it shares nothing with the
	// handlers above beyond the process — see internal/agui/doc.go.
	agui *agui.Runtime
	// aguiPath is the route prefix owned by that adapter, excluded from the
	// server-wide CORS and token middleware so the adapter can enforce its own
	// stricter policy. It is empty when the adapter runs on its own listener,
	// because then it owns no path on this server at all.
	aguiPath string
	// aguiListener is set instead of aguiPath when [AGUI] Port is configured:
	// the adapter then serves itself, and this server merely owns its lifetime.
	aguiListener *agui.Listener
	// seededSessions tracks which sessions have already had the auto-approve
	// config default applied, so a user toggle is not overridden on later prompts.
	seededSessions sync.Map

	// bindMu guards the live bind host and the rebind handshake below. The bind
	// host changes at runtime when the external-access toggle is flipped, and it
	// is read by the basic-auth gate on every request.
	bindMu sync.RWMutex
	// bindHost is the address the active listener is bound to.
	bindHost string
	// initialHost is the host the process was started with. Turning external
	// access off returns to it.
	initialHost string
	// rebinding is set while SetExternalAccess is swapping listeners, so the
	// Serve loop in Start knows a closed listener is not a shutdown.
	rebinding bool
	// rebindCh hands the freshly bound listener to the Serve loop. A nil value
	// means the rebind failed and the previous listener could not be restored.
	rebindCh chan net.Listener
	// activeListener is the listener Serve is currently accepting on. Rebind
	// closes it directly: http.Server.Close would also mark the server as shut
	// down, which would end the Serve loop for good.
	activeListener net.Listener
}

func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	appOpts := app.AppOptions{DBQuerier: cfg.Querier, StartupMode: cfg.StartupMode}
	application, err := app.New(ctx, cfg.DB, appOpts)
	if err != nil {
		return nil, err
	}

	// Tool permissions are governed per-session ("auto mode" toggle). The default
	// state is seeded from Permissions.AutoApproveTools when a session is used
	// (see getOrCreateSession); we no longer auto-approve globally so the Web UI
	// can prompt for permission just like the TUI.

	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	s := &Server{
		app:         application,
		config:      cfg,
		token:       token,
		staticFS:    cfg.StaticFS,
		bgRunner:    NewBackgroundSessionManager(),
		bindHost:    cfg.Host,
		initialHost: cfg.Host,
		rebindCh:    make(chan net.Listener, 1),
	}

	if s.staticFS != nil {
		s.staticHandler = http.FileServer(http.FS(s.staticFS))
	}

	s.setupAGUI()

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Extension middleware wraps the routes but sits *inside* core's own auth
	// and CORS: an extension must be able to add SSO or audit logging, never to
	// weaken the token check that protects the API.
	var routed http.Handler = mux
	if s.app != nil {
		routed = extensions.WrapHTTP(s.app.Extensions, routed)
	}

	handler := s.corsMiddleware(s.basicAuthMiddleware(s.authMiddleware(routed)))
	if s.staticHandler != nil {
		handler = s.corsMiddleware(s.uiHandler(s.basicAuthMiddleware(s.authMiddleware(routed))))
	}

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
	}

	return s, nil
}

// setupAGUI builds the AG-UI adapter when it is enabled in config. It mirrors
// the dependency set app.go passes to the coder agent, minus the permission and
// user-input services: the adapter creates its own so a browser run can never
// raise a prompt on a desktop surface.
//
// A failure here is never fatal — the rest of the API server must keep working.
func (s *Server) setupAGUI() {
	cfg := config.Get()
	if cfg == nil || !cfg.AGUI.Enabled {
		return
	}

	aguiCfg := agui.ConfigFromApp(cfg.AGUI)

	runtime, err := agui.New(agui.Deps{
		Sessions:     s.app.Sessions,
		Messages:     s.app.Messages,
		History:      s.app.History,
		Skills:       s.app.SkillManager,
		Gateway:      s.app.MCPGateway,
		Orchestrator: s.app.MesnadaOrchestrator,
		Remembrances: s.app.Remembrances,
		LSP:          s.app,
		DB:           s.config.DB,
		Token:        s.token,
	}, aguiCfg)
	if err != nil {
		logging.Error("Failed to start the AG-UI adapter", err)
		return
	}

	s.agui = runtime

	// A configured port moves the adapter off this server entirely, so a browser
	// origin allowed to reach AG-UI cannot reach the Web-UI API. The routes are
	// then NOT registered on the main mux, and aguiPath stays empty so the
	// middleware exclusions do not open a hole for a path nothing serves.
	if aguiCfg.Port > 0 {
		listener, lerr := runtime.StartListener(agui.ListenerOptions{
			Host:     cfg.AGUI.Host,
			CertFile: s.config.TLSCertFile,
			KeyFile:  s.config.TLSKeyFile,
		})
		if lerr != nil {
			// Falling back to the shared mux would silently widen the surface the
			// operator asked to isolate, so the adapter is dropped instead.
			logging.Error("AG-UI dedicated listener failed to start; the adapter is disabled", lerr)
			runtime.Close()
			s.agui = nil
			return
		}
		s.aguiListener = listener
		return
	}

	s.aguiPath = strings.TrimSuffix(aguiCfg.Path, "/")
}

// isAGUIPath reports whether a request belongs to the AG-UI adapter, which
// enforces its own authentication and CORS policy.
func (s *Server) isAGUIPath(urlPath string) bool {
	if s.agui == nil || s.aguiPath == "" {
		return false
	}
	return urlPath == s.aguiPath || strings.HasPrefix(urlPath, s.aguiPath+"/")
}

// Start binds the listener and serves until shutdown. The listener can be
// swapped underneath the loop by SetExternalAccess, which closes the current one
// and hands over a replacement bound to a different host; Serve returning
// because of that swap is not an error.
func (s *Server) Start() error {
	listener, err := s.newListener(s.BindHost())
	if err != nil {
		return err
	}

	for {
		s.bindMu.Lock()
		s.activeListener = listener
		s.bindMu.Unlock()

		err = s.httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return err
		}

		s.bindMu.RLock()
		swapping := s.rebinding
		s.bindMu.RUnlock()
		if !swapping {
			return err
		}

		next := <-s.rebindCh
		if next == nil {
			return err
		}
		listener = next
	}
}

// newListener binds host:port, wrapped in TLS when the server is configured for
// it. ServeTLS is not used because the Serve loop above owns the listener.
func (s *Server) newListener(host string) (net.Listener, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(s.config.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if !s.IsTLS() {
		return listener, nil
	}

	cert, err := tls.LoadX509KeyPair(s.config.TLSCertFile, s.config.TLSKeyFile)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to load TLS key pair: %w", err)
	}
	return tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{cert}}), nil
}

// BindHost returns the host the server is currently listening on. It falls back
// to the configured host so a Server built without NewServer (tests) still
// reports the right bind.
func (s *Server) BindHost() string {
	s.bindMu.RLock()
	defer s.bindMu.RUnlock()
	if s.bindHost == "" {
		return s.config.Host
	}
	return s.bindHost
}

// InitialHost returns the host the process was started with.
func (s *Server) InitialHost() string {
	s.bindMu.RLock()
	defer s.bindMu.RUnlock()
	if s.initialHost == "" {
		return s.config.Host
	}
	return s.initialHost
}

// Rebind moves the listener to another host without dropping the process. The
// old listener must be closed before the new one is bound: 0.0.0.0 and
// 127.0.0.1 collide on the same port. On failure the previous host is restored
// when it can still be bound, and the error is returned either way.
func (s *Server) Rebind(host string) error {
	s.bindMu.Lock()
	current := s.bindHost
	if current == host {
		s.bindMu.Unlock()
		return nil
	}
	s.rebinding = true
	s.bindMu.Unlock()

	defer func() {
		s.bindMu.Lock()
		s.rebinding = false
		s.bindMu.Unlock()
	}()

	// Closing the listener makes Serve return; the loop in Start then waits on
	// rebindCh, which every path below must feed exactly once.
	s.bindMu.RLock()
	active := s.activeListener
	s.bindMu.RUnlock()
	if active == nil {
		return fmt.Errorf("server is not listening yet")
	}
	if err := active.Close(); err != nil {
		return fmt.Errorf("failed to release %s: %w", current, err)
	}

	listener, err := s.newListener(host)
	if err != nil {
		restored, restoreErr := s.newListener(current)
		if restoreErr != nil {
			s.rebindCh <- nil
			return fmt.Errorf("failed to bind %s (%w) and failed to restore %s: %v", host, err, current, restoreErr)
		}
		s.rebindCh <- restored
		return fmt.Errorf("failed to bind %s: %w", host, err)
	}

	s.bindMu.Lock()
	s.bindHost = host
	s.bindMu.Unlock()
	s.rebindCh <- listener
	return nil
}

// IsTLS reports whether the server is configured to use TLS.
func (s *Server) IsTLS() bool {
	return s.config.TLSCertFile != "" && s.config.TLSKeyFile != ""
}

// PandoApp returns the underlying app.App instance.
// This is used by serve/app/desktop commands to set up the IPC bus after
// the server is created.
func (s *Server) PandoApp() *app.App {
	return s.app
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.aguiListener != nil {
		if err := s.aguiListener.Shutdown(ctx); err != nil {
			logging.Debug("AG-UI listener shutdown", "error", err)
		}
	}
	if s.agui != nil {
		s.agui.Close()
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		s.app.Shutdown()
	}()

	serverErr := s.httpServer.Shutdown(ctx)
	select {
	case <-shutdownDone:
	case <-ctx.Done():
		if serverErr == nil {
			serverErr = ctx.Err()
		}
	}

	return serverErr
}

func (s *Server) GetToken() string {
	return s.token
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate api token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The AG-UI adapter drives a code-executing agent from a browser, so it
		// answers a pinned origin allow-list instead of the wildcard used by the
		// Web-UI routes. Let it write its own headers.
		if s.isAGUIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Pando-Token, X-Pando-Client, Authorization")
		w.Header().Set("Access-Control-Expose-Headers", "X-Pando-Token")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// hasValidToken reports whether the request carries this server's API token,
// either in the X-Pando-Token header or as a ?token= query parameter (the only
// option for EventSource streams).
func (s *Server) hasValidToken(r *http.Request) bool {
	token := r.Header.Get("X-Pando-Token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/v1/token" {
			next.ServeHTTP(w, r)
			return
		}

		// AG-UI clients authenticate with `Authorization: Bearer <token>`, which
		// this middleware does not understand; the adapter validates the same
		// token itself (see agui.Runtime.authorize).
		if s.isAGUIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		if !s.hasValidToken(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) uiHandler(apiHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			apiHandler.ServeHTTP(w, r)
			return
		}

		if s.serveStaticAsset(w, r) {
			return
		}

		route := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if route == "." {
			route = ""
		}
		if route != "" {
			if _, err := fs.Stat(s.staticFS, route); err == nil {
				s.staticHandler.ServeHTTP(w, r)
				return
			}
		}

		s.serveIndexHTML(w, r)
	})
}

func (s *Server) serveStaticAsset(w http.ResponseWriter, r *http.Request) bool {
	if s.staticFS == nil {
		return false
	}

	cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if cleanPath == "." || cleanPath == "" {
		return false
	}

	encoding, assetPath := s.resolveEncodedAsset(cleanPath, r.Header.Get("Accept-Encoding"))
	if assetPath == "" {
		return false
	}

	content, err := fs.ReadFile(s.staticFS, assetPath)
	if err != nil {
		return false
	}

	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
		w.Header().Set("Vary", "Accept-Encoding")
	}
	if contentType := mime.TypeByExtension(path.Ext(cleanPath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	// Vite emits content-hashed bundles under assets/ — their filename changes
	// whenever their content does, so they are safe to cache forever. Everything
	// else (service worker scripts, manifest, icons) must stay revalidated so a
	// freshly updated binary is never shadowed by a stale WebView HTTP cache,
	// which would prevent the service worker from picking up the new version.
	if strings.HasPrefix(cleanPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	http.ServeContent(w, r, cleanPath, time.Time{}, bytes.NewReader(content))
	return true
}

func (s *Server) serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	encoding, assetPath := s.resolveEncodedAsset("index.html", r.Header.Get("Accept-Encoding"))
	if assetPath == "" {
		http.NotFound(w, r)
		return
	}

	content, err := fs.ReadFile(s.staticFS, assetPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if encoding == "" {
		content = s.InjectRuntimeConfig(content)
	}
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
		w.Header().Set("Vary", "Accept-Encoding")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// index.html is the SPA/PWA entry point and references the current hashed
	// bundles; it must always be revalidated so an updated binary is picked up
	// immediately instead of serving a cached document that points at old assets.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(content))
}

func (s *Server) resolveEncodedAsset(name, acceptEncoding string) (string, string) {
	if s.staticFS == nil {
		return "", ""
	}

	if strings.Contains(acceptEncoding, "br") {
		if _, err := fs.Stat(s.staticFS, name+".br"); err == nil {
			return "br", name + ".br"
		}
	}
	if strings.Contains(acceptEncoding, "gzip") {
		if _, err := fs.Stat(s.staticFS, name+".gz"); err == nil {
			return "gzip", name + ".gz"
		}
	}
	if _, err := fs.Stat(s.staticFS, name); err == nil {
		return "", name
	}
	return "", ""
}

func (s *Server) InjectRuntimeConfig(html []byte) []byte {
	if s.config.UIBaseURL == "" {
		return html
	}

	injection := fmt.Sprintf(`<script>window.__PANDO_API_BASE__=%q;</script>`, s.config.UIBaseURL)
	content := string(html)
	if strings.Contains(content, "</head>") {
		return []byte(strings.Replace(content, "</head>", injection+"</head>", 1))
	}
	return append([]byte(injection), html...)
}
