package llmproxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/digiogithub/pando/internal/sandbox/portguard"
)

// LLMProxyServer is an OpenAI-compatible HTTP proxy server.
type LLMProxyServer struct {
	httpServer *http.Server
	config     ProxyConfig
}

// NewServer creates a new LLMProxyServer with the given configuration.
func NewServer(cfg ProxyConfig) *LLMProxyServer {
	s := &LLMProxyServer{
		config: cfg,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Build middleware chain: CORS -> optional auth -> mux
	var handler http.Handler = mux
	handler = authMiddleware(cfg.APIKey, handler)
	handler = corsMiddleware(handler)

	s.httpServer = &http.Server{
		Addr:        fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:     handler,
		ReadTimeout: 30 * time.Second,
		// WriteTimeout is 0 to allow streaming responses
		WriteTimeout: 0,
	}

	return s
}

// Start begins listening and serving HTTP requests.
func (s *LLMProxyServer) Start() error {
	addr := s.httpServer.Addr
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	// Sandboxed commands must not reach the proxy (it spends the user's
	// provider credentials).
	return s.httpServer.Serve(portguard.Guard(ln, "llm-proxy"))
}

// Shutdown gracefully shuts down the server without interrupting active connections.
func (s *LLMProxyServer) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
