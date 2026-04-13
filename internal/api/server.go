package api

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/app"
)

type ServerConfig struct {
	Host      string
	Port      int
	Version   string
	DB        *sql.DB
	CWD       string
	StaticFS  fs.FS
	OpenUI    bool
	UIBaseURL string
}

type Server struct {
	httpServer    *http.Server
	app           *app.App
	config        ServerConfig
	token         string
	staticFS      fs.FS
	staticHandler http.Handler
}

func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	application, err := app.New(ctx, cfg.DB)
	if err != nil {
		return nil, err
	}

	application.Permissions.SetGlobalAutoApprove(true)

	s := &Server{
		app:      application,
		config:   cfg,
		token:    generateToken(),
		staticFS: cfg.StaticFS,
	}

	if s.staticFS != nil {
		s.staticHandler = http.FileServer(http.FS(s.staticFS))
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	handler := s.corsMiddleware(s.authMiddleware(mux))
	if s.staticHandler != nil {
		handler = s.corsMiddleware(s.uiHandler(s.authMiddleware(mux)))
	}

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
	}

	return s, nil
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.app.Shutdown()
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) GetToken() string {
	return s.token
}

func generateToken() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 32)
	for i := range b {
		b[i] = chars[i%len(chars)]
	}
	return string(b)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Pando-Token")
		w.Header().Set("Access-Control-Expose-Headers", "X-Pando-Token")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/v1/token" {
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get("X-Pando-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if token != s.token {
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
