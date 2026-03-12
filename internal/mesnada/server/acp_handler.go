package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/digiogithub/pando/internal/mesnada/acp"
)

// ACPHandler integrates ACP transport with the Mesnada server.
type ACPHandler struct {
	transport *acp.HTTPTransport
	agent     acp.ACPAgent
	logger    *log.Logger
}

// ACPHandlerConfig holds configuration for the ACP handler.
type ACPHandlerConfig struct {
	Agent     acp.ACPAgent
	Logger    *log.Logger
	Transport *acp.HTTPTransport
}

// NewACPHandler creates a new ACP handler for the Mesnada server.
func NewACPHandler(cfg ACPHandlerConfig) *ACPHandler {
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}

	// Create transport if not provided
	if cfg.Transport == nil {
		transportCfg := acp.DefaultHTTPTransportConfig()
		cfg.Transport = acp.NewHTTPTransport(cfg.Agent, cfg.Logger, transportCfg)
	}

	return &ACPHandler{
		transport: cfg.Transport,
		agent:     cfg.Agent,
		logger:    cfg.Logger,
	}
}

// RegisterRoutes registers ACP endpoints on the HTTP mux.
func (h *ACPHandler) RegisterRoutes(mux *http.ServeMux) {
	h.logger.Printf("[ACP HANDLER] Registering ACP routes")

	mux.HandleFunc("/mesnada/acp", h.handleRequest)
	mux.HandleFunc("/mesnada/acp/events", h.handleSSE)
	mux.HandleFunc("/mesnada/acp/health", h.handleHealth)

	h.logger.Printf("[ACP HANDLER] Routes registered:")
	h.logger.Printf("  POST /mesnada/acp         - ACP JSON-RPC requests")
	h.logger.Printf("  GET  /mesnada/acp/events  - SSE event stream")
	h.logger.Printf("  GET  /mesnada/acp/health  - Health check")
}

// handleRequest proxies to the transport's HandleRequest.
func (h *ACPHandler) handleRequest(w http.ResponseWriter, r *http.Request) {
	h.transport.HandleRequest(w, r)
}

// handleSSE proxies to the transport's HandleSSE.
func (h *ACPHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	h.transport.HandleSSE(w, r)
}

// handleHealth provides detailed health information about ACP.
func (h *ACPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	activeSessions := h.transport.ActiveSessions()
	capabilities := h.agent.GetCapabilities()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "healthy",
		"protocol": "ACP",
		"transport": map[string]interface{}{
			"type":            "http+sse",
			"active_sessions": activeSessions,
		},
		"agent": map[string]interface{}{
			"name":         "pando",
			"version":      h.agent.GetVersion(),
			"capabilities": capabilities,
		},
	})
}

// StartCleanup starts the background cleanup goroutine.
func (h *ACPHandler) StartCleanup(ctx context.Context) {
	go h.transport.Cleanup(ctx)
}

// GetTransport returns the underlying HTTP transport.
func (h *ACPHandler) GetTransport() *acp.HTTPTransport {
	return h.transport
}

// GetAgent returns the underlying ACP agent.
func (h *ACPHandler) GetAgent() acp.ACPAgent {
	return h.agent
}
