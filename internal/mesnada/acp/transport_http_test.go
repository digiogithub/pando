package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPTransport_HandleRequest(t *testing.T) {
	// Create agent and transport
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Test initialize request
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}

	body, err := json.Marshal(initRequest)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	transport.HandleRequest(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Should have session ID
	sessionID := w.Header().Get("ACP-Session-Id")
	if sessionID == "" {
		t.Error("Expected ACP-Session-Id header")
	}

	// Parse response
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check response structure
	if response["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
	}

	if response["id"] != float64(1) {
		t.Errorf("Expected id 1, got %v", response["id"])
	}

	// Should have result with agent info
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected result object")
	}

	agentInfo, ok := result["agentInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected agentInfo in result")
	}

	if agentInfo["name"] != "pando" {
		t.Errorf("Expected agent name 'pando', got %v", agentInfo["name"])
	}
}

func TestHTTPTransport_SessionManagement(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// First request - should create session
	req1 := createInitRequest(t, "")
	w1 := httptest.NewRecorder()
	transport.HandleRequest(w1, req1)

	sessionID := w1.Header().Get("ACP-Session-Id")
	if sessionID == "" {
		t.Fatal("Expected session ID in first response")
	}

	// Second request with same session ID
	req2 := createInitRequest(t, sessionID)
	w2 := httptest.NewRecorder()
	transport.HandleRequest(w2, req2)

	sessionID2 := w2.Header().Get("ACP-Session-Id")
	if sessionID2 != sessionID {
		t.Errorf("Expected same session ID, got %s vs %s", sessionID, sessionID2)
	}

	// Verify session count
	if transport.ActiveSessions() != 1 {
		t.Errorf("Expected 1 active session, got %d", transport.ActiveSessions())
	}
}

func TestHTTPTransport_MaxSessions(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := HTTPTransportConfig{
		MaxSessions:  2,
		IdleTimeout:  30 * time.Minute,
		EventBufSize: 100,
	}
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Create 2 sessions (max)
	for i := 0; i < 2; i++ {
		req := createInitRequest(t, "")
		w := httptest.NewRecorder()
		transport.HandleRequest(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d failed with status %d", i, w.Code)
		}
	}

	// Third session should fail
	req := createInitRequest(t, "")
	w := httptest.NewRecorder()
	transport.HandleRequest(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 for max sessions, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "max sessions") {
		t.Errorf("Expected 'max sessions' error, got: %s", body)
	}
}

func TestHTTPTransport_InvalidMethod(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Try GET instead of POST
	req := httptest.NewRequest(http.MethodGet, "/mesnada/acp", nil)
	w := httptest.NewRecorder()
	transport.HandleRequest(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHTTPTransport_InvalidJSON(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	transport.HandleRequest(w, req)

	// Should return JSON-RPC error
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if response["error"] == nil {
		t.Error("Expected error in response")
	}
}

func TestHTTPTransport_SSE(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// First create a session
	req1 := createInitRequest(t, "")
	w1 := httptest.NewRecorder()
	transport.HandleRequest(w1, req1)

	sessionID := w1.Header().Get("ACP-Session-Id")

	// Now connect to SSE endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req2 := httptest.NewRequest(http.MethodGet, "/mesnada/acp/events", nil)
	req2.Header.Set("ACP-Session-Id", sessionID)
	req2 = req2.WithContext(ctx)

	w2 := httptest.NewRecorder()

	// Run SSE handler in goroutine since it blocks
	done := make(chan bool)
	go func() {
		transport.HandleSSE(w2, req2)
		done <- true
	}()

	// Wait a bit for initial connection
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop SSE
	cancel()

	// Wait for handler to finish
	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Fatal("SSE handler didn't stop")
	}

	// Check headers
	contentType := w2.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected text/event-stream, got %s", contentType)
	}

	// Check for connected event
	body := w2.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Error("Expected connected event")
	}

	if !strings.Contains(body, sessionID) {
		t.Error("Expected session ID in connected event")
	}
}

func TestHTTPTransport_SSE_NoSession(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Try to connect to SSE without valid session
	req := httptest.NewRequest(http.MethodGet, "/mesnada/acp/events", nil)
	req.Header.Set("ACP-Session-Id", "nonexistent")

	w := httptest.NewRecorder()
	transport.HandleSSE(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestHTTPTransport_SSE_MissingSessionID(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Try to connect without session ID header
	req := httptest.NewRequest(http.MethodGet, "/mesnada/acp/events", nil)
	w := httptest.NewRecorder()
	transport.HandleSSE(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHTTPTransport_Health(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Create a session first
	req1 := createInitRequest(t, "")
	w1 := httptest.NewRecorder()
	transport.HandleRequest(w1, req1)

	// Check health
	req2 := httptest.NewRequest(http.MethodGet, "/mesnada/acp/health", nil)
	w2 := httptest.NewRecorder()
	transport.HandleHealth(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w2.Code)
	}

	var health map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &health); err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	if health["status"] != "healthy" {
		t.Errorf("Expected status healthy, got %v", health["status"])
	}

	if health["transport"] != "http+sse" {
		t.Errorf("Expected transport http+sse, got %v", health["transport"])
	}

	// Should have 1 active session
	activeSessions := health["active_sessions"].(float64)
	if activeSessions != 1 {
		t.Errorf("Expected 1 active session, got %v", activeSessions)
	}
}

func TestHTTPTransport_ConcurrentRequests(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Create multiple concurrent sessions
	numClients := 10
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			req := createInitRequest(t, "")
			w := httptest.NewRecorder()
			transport.HandleRequest(w, req)

			if w.Code != http.StatusOK {
				errors <- io.EOF
				return
			}

			sessionID := w.Header().Get("ACP-Session-Id")
			if sessionID == "" {
				errors <- io.EOF
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		if err != nil {
			t.Errorf("Concurrent request failed")
		}
	}

	// Should have 10 sessions
	if transport.ActiveSessions() != numClients {
		t.Errorf("Expected %d active sessions, got %d", numClients, transport.ActiveSessions())
	}
}

func TestHTTPTransport_CloseSession(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Create session
	req := createInitRequest(t, "")
	w := httptest.NewRecorder()
	transport.HandleRequest(w, req)

	sessionID := w.Header().Get("ACP-Session-Id")

	// Close session
	if err := transport.CloseSession(sessionID); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}

	// Should have 0 sessions
	if transport.ActiveSessions() != 0 {
		t.Errorf("Expected 0 active sessions after close, got %d", transport.ActiveSessions())
	}

	// Closing again should error
	if err := transport.CloseSession(sessionID); err == nil {
		t.Error("Expected error closing non-existent session")
	}
}

func TestHTTPTransport_IdleCleanup(t *testing.T) {
	agent := NewSimpleACPAgent("1.0.0-test", log.Default())
	config := HTTPTransportConfig{
		MaxSessions:  100,
		IdleTimeout:  100 * time.Millisecond, // Very short timeout for testing
		EventBufSize: 100,
	}
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Create session
	req := createInitRequest(t, "")
	w := httptest.NewRecorder()
	transport.HandleRequest(w, req)

	if transport.ActiveSessions() != 1 {
		t.Fatalf("Expected 1 active session, got %d", transport.ActiveSessions())
	}

	// Wait for idle timeout
	time.Sleep(200 * time.Millisecond)

	// Run cleanup
	transport.cleanupIdleSessions()

	// Session should be removed
	if transport.ActiveSessions() != 0 {
		t.Errorf("Expected 0 active sessions after cleanup, got %d", transport.ActiveSessions())
	}
}

// Helper functions

func createInitRequest(t *testing.T, sessionID string) *http.Request {
	t.Helper()

	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}

	body, err := json.Marshal(initRequest)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if sessionID != "" {
		req.Header.Set("ACP-Session-Id", sessionID)
	}

	return req
}
