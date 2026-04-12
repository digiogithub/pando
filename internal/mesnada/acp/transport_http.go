package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	acpsdk "github.com/coder/acp-go-sdk"
)

// HTTPTransport implements HTTP/SSE transport for ACP.
// It manages multiple client sessions and routes requests to the ACP agent.
type HTTPTransport struct {
	agent      ACPAgent
	logger     *log.Logger
	sessions   map[string]*httpSession
	sessionsMu sync.RWMutex

	// Configuration
	maxSessions  int
	idleTimeout  time.Duration
	eventBufSize int

	// Statistics
	startTime         time.Time
	totalSessions     int
	requestsProcessed int
	statsMu           sync.RWMutex
}

// httpSession represents an HTTP client session.
type httpSession struct {
	id        string
	createdAt time.Time
	lastUsed  time.Time
	eventCh   chan []byte
	closed    bool
	mu        sync.Mutex

	// For bridging HTTP to ACP SDK connection
	reqPipe    *io.PipeWriter
	respReader *bufio.Reader // buffered reader over the response pipe
	respPipe   *io.PipeReader
	conn       *acpsdk.AgentSideConnection
}

// HTTPTransportConfig holds configuration for the HTTP transport.
type HTTPTransportConfig struct {
	MaxSessions  int
	IdleTimeout  time.Duration
	EventBufSize int
}

// DefaultHTTPTransportConfig returns default configuration.
func DefaultHTTPTransportConfig() HTTPTransportConfig {
	return HTTPTransportConfig{
		MaxSessions:  100,
		IdleTimeout:  30 * time.Minute,
		EventBufSize: 100,
	}
}

// NewHTTPTransport creates a new HTTP transport for the ACP agent.
func NewHTTPTransport(agent ACPAgent, logger *log.Logger, config HTTPTransportConfig) *HTTPTransport {
	if logger == nil {
		logger = log.Default()
	}

	if config.MaxSessions == 0 {
		config = DefaultHTTPTransportConfig()
	}

	return &HTTPTransport{
		agent:        agent,
		logger:       logger,
		sessions:     make(map[string]*httpSession),
		maxSessions:  config.MaxSessions,
		idleTimeout:  config.IdleTimeout,
		eventBufSize: config.EventBufSize,
		startTime:    time.Now(),
	}
}

// HandleRequest handles HTTP POST requests for ACP JSON-RPC.
func (t *HTTPTransport) HandleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get or create session
	sessionID := r.Header.Get("ACP-Session-Id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	session, err := t.getOrCreateSession(sessionID)
	if err != nil {
		t.logger.Printf("[ACP HTTP] Error getting/creating session: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update last used time
	session.mu.Lock()
	session.lastUsed = time.Now()
	session.mu.Unlock()

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.logger.Printf("[ACP HTTP] Error reading request body: %v", err)
		t.writeError(w, sessionID, -32700, "Parse error", err.Error())
		return
	}

	// Validate it's valid JSON-RPC
	var jsonrpcReq map[string]interface{}
	if err := json.Unmarshal(body, &jsonrpcReq); err != nil {
		t.logger.Printf("[ACP HTTP] Invalid JSON-RPC: %v", err)
		t.writeError(w, sessionID, -32700, "Parse error", err.Error())
		return
	}

	t.logger.Printf("[ACP HTTP] Request: session=%s, method=%v", sessionID, jsonrpcReq["method"])

	// Increment request counter
	t.statsMu.Lock()
	t.requestsProcessed++
	t.statsMu.Unlock()

	// Handle "session/list" inline: the Go SDK v0.6.3 does not implement this
	// method, so we intercept it here before forwarding to the SDK pipe.
	if method, _ := jsonrpcReq["method"].(string); method == "session/list" {
		if pandoAgent, ok := t.agent.(*PandoACPAgent); ok {
			var reqMsg jsonRPCMsg
			if err := json.Unmarshal(body, &reqMsg); err == nil {
				t.logger.Printf("[ACP HTTP] Intercepting session/list")
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("ACP-Session-Id", sessionID)
				handleSessionListRPC(reqMsg, w, pandoAgent, t.logger)
				return
			}
		}
	}

	// Write request to the pipe (this goes to the ACP SDK connection)
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		http.Error(w, "Session closed", http.StatusGone)
		return
	}

	// Write the request followed by newline
	_, err = session.reqPipe.Write(append(body, '\n'))
	session.mu.Unlock()

	if err != nil {
		t.logger.Printf("[ACP HTTP] Error writing to pipe: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Read response from the pipe. Notifications that arrive before the
	// actual response are automatically routed to the SSE eventCh.
	response, err := t.readJSONRPCResponse(session, session.respReader)
	if err != nil {
		t.logger.Printf("[ACP HTTP] Error reading response: %v", err)
		t.writeError(w, sessionID, -32603, "Internal error", err.Error())
		return
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ACP-Session-Id", sessionID)
	w.Write(response)
}

// HandleSSE handles Server-Sent Events for notifications.
func (t *HTTPTransport) HandleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("ACP-Session-Id")
	if sessionID == "" {
		http.Error(w, "Missing ACP-Session-Id header", http.StatusBadRequest)
		return
	}

	// Get session
	t.sessionsMu.RLock()
	session, exists := t.sessions[sessionID]
	t.sessionsMu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Setup SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	t.logger.Printf("[ACP SSE] Client connected: session=%s", sessionID)

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\ndata: {\"sessionId\":\"%s\"}\n\n", sessionID)
	flusher.Flush()

	// Stream events
	for {
		select {
		case <-r.Context().Done():
			t.logger.Printf("[ACP SSE] Client disconnected: session=%s", sessionID)
			return
		case data := <-session.eventCh:
			fmt.Fprintf(w, "event: session-update\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// HandleHealth handles health check requests.
func (t *HTTPTransport) HandleHealth(w http.ResponseWriter, r *http.Request) {
	t.sessionsMu.RLock()
	activeSessions := len(t.sessions)
	t.sessionsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "healthy",
		"transport":       "http+sse",
		"active_sessions": activeSessions,
		"max_sessions":    t.maxSessions,
	})
}

// getOrCreateSession gets an existing session or creates a new one.
func (t *HTTPTransport) getOrCreateSession(sessionID string) (*httpSession, error) {
	t.sessionsMu.Lock()
	defer t.sessionsMu.Unlock()

	// Check if session exists
	if session, exists := t.sessions[sessionID]; exists {
		return session, nil
	}

	// Check max sessions limit
	if len(t.sessions) >= t.maxSessions {
		return nil, fmt.Errorf("max sessions reached (%d)", t.maxSessions)
	}

	// Create new session
	session := &httpSession{
		id:        sessionID,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		eventCh:   make(chan []byte, t.eventBufSize),
	}

	// Create pipes for bidirectional communication
	reqReader, reqWriter := io.Pipe()
	respReader, respWriter := io.Pipe()

	session.reqPipe = reqWriter
	session.respPipe = respReader
	session.respReader = bufio.NewReaderSize(respReader, 256*1024)

	// Create ACP SDK connection
	// Agent writes responses to respWriter, reads requests from reqReader
	session.conn = acpsdk.NewAgentSideConnection(t.agent, respWriter, reqReader)

	t.sessions[sessionID] = session

	// Increment total sessions counter
	t.statsMu.Lock()
	t.totalSessions++
	t.statsMu.Unlock()

	t.logger.Printf("[ACP HTTP] Created new session: %s", sessionID)

	return session, nil
}

// readJSONRPCResponse reads newline-delimited JSON messages from the SDK pipe.
// The ACP SDK writes both JSON-RPC responses (with "id") and notifications
// (without "id", e.g. session/update) to the same writer. This method reads
// messages one by one, routing notifications to the SSE eventCh and returning
// only the actual JSON-RPC response to the caller.
func (t *HTTPTransport) readJSONRPCResponse(session *httpSession, r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return nil, err
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		// Quick check: is this a response (has "id") or a notification?
		if isJSONRPCNotification(trimmed) {
			// Route to SSE channel for connected clients.
			session.mu.Lock()
			closed := session.closed
			session.mu.Unlock()
			if !closed {
				select {
				case session.eventCh <- trimmed:
				default:
					t.logger.Printf("[ACP HTTP] SSE event buffer full, dropping notification")
				}
			}
			continue
		}

		// This is a JSON-RPC response — return it.
		return trimmed, nil
	}
}

// isJSONRPCNotification returns true if the raw JSON is a JSON-RPC notification
// (has "method" but no "id" field, or has "id": null).
func isJSONRPCNotification(data []byte) bool {
	var probe struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	// Notifications have method but no id (or null id).
	if probe.Method != "" && (len(probe.ID) == 0 || string(probe.ID) == "null") {
		return true
	}
	return false
}

// writeError writes a JSON-RPC error response.
func (t *HTTPTransport) writeError(w http.ResponseWriter, sessionID string, code int, message, data string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ACP-Session-Id", sessionID)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jsonrpc": "2.0",
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
			"data":    data,
		},
	})
}

// CloseSession closes a session and cleans up resources.
func (t *HTTPTransport) CloseSession(sessionID string) error {
	t.sessionsMu.Lock()
	defer t.sessionsMu.Unlock()

	session, exists := t.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.closed {
		return nil
	}

	session.closed = true
	close(session.eventCh)
	session.reqPipe.Close()
	session.respPipe.Close()

	delete(t.sessions, sessionID)

	t.logger.Printf("[ACP HTTP] Closed session: %s", sessionID)

	return nil
}

// Cleanup removes idle sessions.
func (t *HTTPTransport) Cleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.cleanupIdleSessions()
		}
	}
}

// cleanupIdleSessions removes sessions that have been idle for too long.
func (t *HTTPTransport) cleanupIdleSessions() {
	t.sessionsMu.Lock()
	defer t.sessionsMu.Unlock()

	now := time.Now()
	var toRemove []string

	for id, session := range t.sessions {
		session.mu.Lock()
		idle := now.Sub(session.lastUsed)
		session.mu.Unlock()

		if idle > t.idleTimeout {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		session := t.sessions[id]
		session.mu.Lock()
		session.closed = true
		close(session.eventCh)
		session.reqPipe.Close()
		session.respPipe.Close()
		session.mu.Unlock()

		delete(t.sessions, id)
		t.logger.Printf("[ACP HTTP] Removed idle session: %s", id)
	}
}

// ActiveSessions returns the number of active sessions.
func (t *HTTPTransport) ActiveSessions() int {
	t.sessionsMu.RLock()
	defer t.sessionsMu.RUnlock()
	return len(t.sessions)
}

// TransportStats holds statistics about the HTTP transport.
type TransportStats struct {
	ActiveSessions    int           `json:"active_sessions"`
	TotalSessions     int           `json:"total_sessions"`
	RequestsProcessed int           `json:"requests_processed"`
	MaxSessions       int           `json:"max_sessions"`
	Uptime            time.Duration `json:"uptime"`
	IdleTimeout       time.Duration `json:"idle_timeout"`
}

// GetStats returns current transport statistics.
func (t *HTTPTransport) GetStats() TransportStats {
	t.sessionsMu.RLock()
	activeSessions := len(t.sessions)
	t.sessionsMu.RUnlock()

	t.statsMu.RLock()
	totalSessions := t.totalSessions
	requestsProcessed := t.requestsProcessed
	uptime := time.Since(t.startTime)
	t.statsMu.RUnlock()

	return TransportStats{
		ActiveSessions:    activeSessions,
		TotalSessions:     totalSessions,
		RequestsProcessed: requestsProcessed,
		MaxSessions:       t.maxSessions,
		Uptime:            uptime,
		IdleTimeout:       t.idleTimeout,
	}
}

// SessionInfo contains information about a specific session.
type SessionInfo struct {
	ID        string        `json:"id"`
	CreatedAt time.Time     `json:"created_at"`
	LastUsed  time.Time     `json:"last_used"`
	IdleTime  time.Duration `json:"idle_time"`
}

// GetSessionInfo returns information about a specific session.
func (t *HTTPTransport) GetSessionInfo(sessionID string) (*SessionInfo, error) {
	t.sessionsMu.RLock()
	session, exists := t.sessions[sessionID]
	t.sessionsMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	session.mu.Lock()
	info := &SessionInfo{
		ID:        session.id,
		CreatedAt: session.createdAt,
		LastUsed:  session.lastUsed,
		IdleTime:  time.Since(session.lastUsed),
	}
	session.mu.Unlock()

	return info, nil
}

// ListSessions returns information about all active sessions.
func (t *HTTPTransport) ListSessions() []SessionInfo {
	t.sessionsMu.RLock()
	defer t.sessionsMu.RUnlock()

	sessions := make([]SessionInfo, 0, len(t.sessions))
	for _, session := range t.sessions {
		session.mu.Lock()
		sessions = append(sessions, SessionInfo{
			ID:        session.id,
			CreatedAt: session.createdAt,
			LastUsed:  session.lastUsed,
			IdleTime:  time.Since(session.lastUsed),
		})
		session.mu.Unlock()
	}

	return sessions
}
