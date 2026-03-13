// Package server implements the MCP server with HTTP Streamable and stdio transports.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	config "github.com/digiogithub/pando/internal/mesnada/config"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	rag "github.com/digiogithub/pando/internal/rag"
)

const (
	jsonRPCVersion = "2.0"
	mcpVersion     = "2024-11-05"
)

// Server is the MCP HTTP Streamable and stdio server.
type Server struct {
	orchestrator *orchestrator.Orchestrator
	addr         string
	version      string
	commit       string
	httpServer   *http.Server
	sessions     map[string]*Session
	sessionMu    sync.RWMutex
	tools        map[string]ToolHandler
	useStdio     bool
	config       *config.Config

	uiOnce   sync.Once
	uiTpl    *template.Template
	uiTplErr error

	// ACP support
	acpHandler *ACPHandler

	// Remembrances support (optional)
	remembrances *rag.RemembrancesService
}

// Session represents an MCP session.
type Session struct {
	ID        string
	CreatedAt time.Time
	events    chan []byte
	closed    bool
	mu        sync.Mutex
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ToolHandler handles a tool call.
type ToolHandler func(ctx context.Context, params json.RawMessage) (interface{}, error)

// Config holds server configuration.
type Config struct {
	Addr         string
	Orchestrator *orchestrator.Orchestrator
	Version      string
	Commit       string
	UseStdio     bool
	AppConfig    *config.Config
	ACPHandler   *ACPHandler                // Optional ACP handler for remote connections
	Remembrances *rag.RemembrancesService   // Optional remembrances service
}

// New creates a new MCP server.
func New(cfg Config) *Server {
	s := &Server{
		orchestrator: cfg.Orchestrator,
		addr:         cfg.Addr,
		version:      cfg.Version,
		commit:       cfg.Commit,
		sessions:     make(map[string]*Session),
		tools:        make(map[string]ToolHandler),
		useStdio:     cfg.UseStdio,
		config:       cfg.AppConfig,
		acpHandler:   cfg.ACPHandler,
		remembrances: cfg.Remembrances,
	}

	s.registerTools()

	if !cfg.UseStdio {
		mux := http.NewServeMux()
		mux.HandleFunc("/mcp", s.handleMCP)
		mux.HandleFunc("/mcp/sse", s.handleSSE)
		mux.HandleFunc("/health", s.handleHealth)
		mux.Handle("/", s.newGinEngine())

		// Register ACP endpoints if handler is provided
		if s.acpHandler != nil {
			s.acpHandler.RegisterRoutes(mux)
		}

		s.httpServer = &http.Server{
			Addr:         cfg.Addr,
			Handler:      s.corsMiddleware(mux),
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 0,
		}
	}

	return s
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id, ACP-Session-Id")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id, ACP-Session-Id")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Start starts the HTTP server or stdio loop.
func (s *Server) Start() error {
	if s.useStdio {
		return s.runStdio()
	}

	// Start ACP cleanup goroutine if handler is configured
	if s.acpHandler != nil {
		ctx := context.Background()
		s.acpHandler.StartCleanup(ctx)
		log.Printf("ACP transport enabled on /mesnada/acp")
	}

	log.Printf("MCP server starting on %s", s.addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.useStdio {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) runStdio() error {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	session := &Session{
		ID:        "stdio",
		CreatedAt: time.Now(),
		events:    make(chan []byte, 100),
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeStdioError(encoder, nil, -32700, "Parse error", err.Error())
			continue
		}

		response := s.handleRequest(context.Background(), session, &req)
		if err := encoder.Encode(response); err != nil {
			log.Printf("Error encoding response: %v", err)
			return err
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("error reading from stdin: %w", err)
	}

	return nil
}

func (s *Server) writeStdioError(encoder *json.Encoder, id interface{}, code int, message, data string) {
	_ = encoder.Encode(&JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := s.orchestrator.GetStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"stats":  stats,
	})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	s.sessionMu.Lock()
	session, exists := s.sessions[sessionID]
	if !exists {
		session = &Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			events:    make(chan []byte, 100),
		}
		s.sessions[sessionID] = session
	}
	s.sessionMu.Unlock()

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, nil, -32700, "Parse error", err.Error())
		return
	}

	w.Header().Set("Mcp-Session-Id", sessionID)
	w.Header().Set("Content-Type", "application/json")

	response := s.handleRequest(r.Context(), session, &req)
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Missing Mcp-Session-Id header", http.StatusBadRequest)
		return
	}

	s.sessionMu.RLock()
	session, exists := s.sessions[sessionID]
	s.sessionMu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "event: connected\ndata: {\"sessionId\":\"%s\"}\n\n", sessionID)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-session.events:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *Server) handleRequest(ctx context.Context, session *Session, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		return s.handleInitialized(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "ping":
		return s.handlePing(req)
	default:
		return &JSONRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

func (s *Server) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	version := s.version
	if version == "" {
		version = "1.0.0"
	}

	return &JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": mcpVersion,
			"serverInfo": map[string]string{
				"name":    "pando",
				"version": version,
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
		},
	}
}

func (s *Server) handleInitialized(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Result:  map[string]interface{}{},
	}
}

func (s *Server) handlePing(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Result:  map[string]interface{}{},
	}
}

func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	tools := s.getToolDefinitions()
	return &JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			},
		}
	}

	handler, exists := s.tools[params.Name]
	if !exists {
		return &JSONRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: fmt.Sprintf("Unknown tool: %s", params.Name),
			},
		}
	}

	result, err := handler(ctx, params.Arguments)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("Error: %s", err.Error()),
					},
				},
				"isError": true,
			},
		}
	}

	text, _ := json.MarshalIndent(result, "", "  ")
	return &JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(text),
				},
			},
		},
	}
}

func (s *Server) writeError(w http.ResponseWriter, id interface{}, code int, message, data string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(&JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

// SendEvent sends an event to a session.
func (s *Server) SendEvent(sessionID string, event interface{}) error {
	s.sessionMu.RLock()
	session, exists := s.sessions[sessionID]
	s.sessionMu.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	select {
	case session.events <- data:
		return nil
	default:
		return fmt.Errorf("event channel full")
	}
}
