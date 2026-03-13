package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/digiogithub/pando/internal/app"
)

type ServerConfig struct {
	Host    string
	Port    int
	Version string
	DB      *sql.DB
	CWD     string
}

type Server struct {
	httpServer *http.Server
	app        *app.App
	config     ServerConfig
	token      string
}

func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	application, err := app.New(ctx, cfg.DB)
	if err != nil {
		return nil, err
	}

	s := &Server{
		app:    application,
		config: cfg,
		token:  generateToken(),
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      s.corsMiddleware(s.authMiddleware(mux)),
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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
		if r.URL.Path == "/health" || r.URL.Path == "/api/v1/token" {
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
