// +build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"www.github.com/MCP/pando/internal/mesnada/acp"
)

// TestEndToEndHTTPFlow tests a complete client-server interaction
func TestEndToEndHTTPFlow(t *testing.T) {
	// 1. Start ACP server
	server := startTestACPServer(t)
	defer server.Close()

	// 2. Initialize connection
	sessionID := initializeClient(t, server.URL)

	// 3. Create a new session
	workDir := createTempWorkspace(t)
	defer os.RemoveAll(workDir)

	sessionResp := createSession(t, server.URL, sessionID, workDir)
	if sessionResp == nil {
		t.Fatal("Failed to create session")
	}

	// 4. Send a simple prompt
	promptResp := sendPrompt(t, server.URL, sessionID, "List files in the current directory")
	if promptResp == nil {
		t.Fatal("Failed to send prompt")
	}

	t.Logf("Prompt response: %+v", promptResp)
}

// TestFileOperations tests file read/write operations
func TestFileOperations(t *testing.T) {
	server := startTestACPServer(t)
	defer server.Close()

	sessionID := initializeClient(t, server.URL)
	workDir := createTempWorkspace(t)
	defer os.RemoveAll(workDir)

	// Create a test file
	testFile := filepath.Join(workDir, "test.txt")
	testContent := "Hello from integration test"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	createSession(t, server.URL, sessionID, workDir)

	// Test reading the file (would require ACP client implementation)
	// This is a placeholder for actual file operation testing
	t.Log("File operations test - basic setup complete")
}

// TestMultipleConcurrentSessions tests handling multiple sessions
func TestMultipleConcurrentSessions(t *testing.T) {
	server := startTestACPServer(t)
	defer server.Close()

	numSessions := 5
	sessionIDs := make([]string, numSessions)

	// Create multiple sessions
	for i := 0; i < numSessions; i++ {
		sessionIDs[i] = initializeClient(t, server.URL)
		if sessionIDs[i] == "" {
			t.Fatalf("Failed to create session %d", i)
		}
	}

	// Verify all sessions are active
	stats := getServerStats(t, server.URL)
	if activeSessions, ok := stats["active_sessions"].(float64); ok {
		if int(activeSessions) != numSessions {
			t.Errorf("Expected %d active sessions, got %d", numSessions, int(activeSessions))
		}
	}

	// List sessions
	sessions := listSessions(t, server.URL)
	if len(sessions) != numSessions {
		t.Errorf("Expected %d sessions in list, got %d", numSessions, len(sessions))
	}

	t.Logf("Successfully created and verified %d concurrent sessions", numSessions)
}

// TestSessionTimeout tests session idle timeout behavior
func TestSessionTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	server := startTestACPServerWithTimeout(t, 500*time.Millisecond)
	defer server.Close()

	sessionID := initializeClient(t, server.URL)

	// Wait for session to timeout
	time.Sleep(1 * time.Second)

	// Session should be cleaned up
	stats := getServerStats(t, server.URL)
	if activeSessions, ok := stats["active_sessions"].(float64); ok {
		if int(activeSessions) != 0 {
			t.Errorf("Expected 0 active sessions after timeout, got %d", int(activeSessions))
		}
	}
}

// TestSSEConnection tests Server-Sent Events streaming
func TestSSEConnection(t *testing.T) {
	server := startTestACPServer(t)
	defer server.Close()

	sessionID := initializeClient(t, server.URL)

	// Connect to SSE endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/mesnada/acp/events", nil)
	if err != nil {
		t.Fatalf("Failed to create SSE request: %v", err)
	}
	req.Header.Set("ACP-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}

	// Read initial events
	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read SSE data: %v", err)
	}

	data := string(buf[:n])
	if !strings.Contains(data, "event: connected") {
		t.Error("Expected connected event in SSE stream")
	}

	t.Log("SSE connection successful")
}

// TestHealthEndpoint tests the health check endpoint
func TestHealthEndpoint(t *testing.T) {
	server := startTestACPServer(t)
	defer server.Close()

	// Create a session first
	initializeClient(t, server.URL)

	resp, err := http.Get(server.URL + "/mesnada/acp/health")
	if err != nil {
		t.Fatalf("Failed to call health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	if health["status"] != "healthy" {
		t.Errorf("Expected status healthy, got %v", health["status"])
	}

	if health["transport"] != "http+sse" {
		t.Errorf("Expected transport http+sse, got %v", health["transport"])
	}

	t.Logf("Health check successful: %+v", health)
}

// TestErrorHandling tests various error scenarios
func TestErrorHandling(t *testing.T) {
	server := startTestACPServer(t)
	defer server.Close()

	tests := []struct {
		name       string
		method     string
		endpoint   string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name:       "Invalid method",
			method:     http.MethodGet,
			endpoint:   "/mesnada/acp",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:     "Invalid JSON",
			method:   http.MethodPost,
			endpoint: "/mesnada/acp",
			body: map[string]interface{}{
				"invalid": "json structure",
			},
			wantStatus: http.StatusOK, // JSON-RPC returns 200 with error
		},
		{
			name:       "SSE without session",
			method:     http.MethodGet,
			endpoint:   "/mesnada/acp/events",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != nil {
				data, _ := json.Marshal(tt.body)
				body = bytes.NewReader(data)
			}

			req, err := http.NewRequest(tt.method, server.URL+tt.endpoint, body)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

// Helper functions

func startTestACPServer(t *testing.T) *httptest.Server {
	t.Helper()

	agent := acp.NewSimpleACPAgent("1.0.0-test", nil)
	config := acp.DefaultHTTPTransportConfig()
	transport := acp.NewHTTPTransport(agent, nil, config)

	mux := http.NewServeMux()
	mux.HandleFunc("/mesnada/acp", transport.HandleRequest)
	mux.HandleFunc("/mesnada/acp/events", transport.HandleSSE)
	mux.HandleFunc("/mesnada/acp/health", transport.HandleHealth)
	mux.HandleFunc("/api/acp/sessions", func(w http.ResponseWriter, r *http.Request) {
		sessions := transport.GetSessions()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessions": sessions,
		})
	})
	mux.HandleFunc("/api/acp/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := transport.GetStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	return httptest.NewServer(mux)
}

func startTestACPServerWithTimeout(t *testing.T, timeout time.Duration) *httptest.Server {
	t.Helper()

	agent := acp.NewSimpleACPAgent("1.0.0-test", nil)
	config := acp.HTTPTransportConfig{
		MaxSessions:  100,
		IdleTimeout:  timeout,
		EventBufSize: 100,
	}
	transport := acp.NewHTTPTransport(agent, nil, config)

	mux := http.NewServeMux()
	mux.HandleFunc("/mesnada/acp", transport.HandleRequest)
	mux.HandleFunc("/mesnada/acp/events", transport.HandleSSE)
	mux.HandleFunc("/mesnada/acp/health", transport.HandleHealth)
	mux.HandleFunc("/api/acp/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := transport.GetStats()
		w.Header.Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	server := httptest.NewServer(mux)

	// Start cleanup goroutine
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			// This would call transport cleanup method if available
		}
	}()

	return server
}

func initializeClient(t *testing.T, serverURL string) string {
	t.Helper()

	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "integration-test-client",
				"version": "1.0.0",
			},
		},
	}

	body, err := json.Marshal(initRequest)
	if err != nil {
		t.Fatalf("Failed to marshal init request: %v", err)
	}

	resp, err := http.Post(serverURL+"/mesnada/acp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Initialize failed with status %d", resp.StatusCode)
	}

	sessionID := resp.Header.Get("ACP-Session-Id")
	if sessionID == "" {
		t.Fatal("No session ID in response")
	}

	return sessionID
}

func createSession(t *testing.T, serverURL, sessionID, workDir string) map[string]interface{} {
	t.Helper()

	newSessionRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "newSession",
		"params": map[string]interface{}{
			"cwd": workDir,
		},
	}

	body, err := json.Marshal(newSessionRequest)
	if err != nil {
		t.Fatalf("Failed to marshal newSession request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/mesnada/acp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ACP-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to parse session response: %v", err)
	}

	return result
}

func sendPrompt(t *testing.T, serverURL, sessionID, prompt string) map[string]interface{} {
	t.Helper()

	promptRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "prompt",
		"params": map[string]interface{}{
			"sessionId": sessionID,
			"prompt": []map[string]interface{}{
				{
					"type": "text",
					"text": prompt,
				},
			},
		},
	}

	body, err := json.Marshal(promptRequest)
	if err != nil {
		t.Fatalf("Failed to marshal prompt request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/mesnada/acp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ACP-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send prompt: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to parse prompt response: %v", err)
	}

	return result
}

func getServerStats(t *testing.T, serverURL string) map[string]interface{} {
	t.Helper()

	resp, err := http.Get(serverURL + "/api/acp/stats")
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	defer resp.Body.Close()

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to parse stats: %v", err)
	}

	return stats
}

func listSessions(t *testing.T, serverURL string) []interface{} {
	t.Helper()

	resp, err := http.Get(serverURL + "/api/acp/sessions")
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to parse sessions: %v", err)
	}

	sessions, ok := result["sessions"].([]interface{})
	if !ok {
		return []interface{}{}
	}

	return sessions
}

func createTempWorkspace(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "acp-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp workspace: %v", err)
	}

	return dir
}

// TestMaxSessionsLimit tests the maximum sessions limit enforcement
func TestMaxSessionsLimit(t *testing.T) {
	agent := acp.NewSimpleACPAgent("1.0.0-test", nil)
	config := acp.HTTPTransportConfig{
		MaxSessions:  3,
		IdleTimeout:  30 * time.Minute,
		EventBufSize: 100,
	}
	transport := acp.NewHTTPTransport(agent, nil, config)

	mux := http.NewServeMux()
	mux.HandleFunc("/mesnada/acp", transport.HandleRequest)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Create max sessions
	sessionIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		sessionIDs[i] = initializeClient(t, server.URL)
	}

	// Fourth session should fail
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

	body, _ := json.Marshal(initRequest)
	resp, err := http.Post(server.URL+"/mesnada/acp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// It might return OK with a JSON-RPC error, check body
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if result["error"] == nil {
			t.Error("Expected error when exceeding max sessions")
		}
	}
}

// TestReconnectionScenario tests client reconnection
func TestReconnectionScenario(t *testing.T) {
	server := startTestACPServer(t)
	defer server.Close()

	// First connection
	sessionID := initializeClient(t, server.URL)

	// Simulate reconnection with same session ID
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

	body, _ := json.Marshal(initRequest)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/mesnada/acp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ACP-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Reconnection failed: %v", err)
	}
	defer resp.Body.Close()

	newSessionID := resp.Header.Get("ACP-Session-Id")
	if newSessionID != sessionID {
		t.Errorf("Expected same session ID on reconnection, got %s vs %s", sessionID, newSessionID)
	}
}

// TestLargePayload tests handling of large payloads
func TestLargePayload(t *testing.T) {
	server := startTestACPServer(t)
	defer server.Close()

	sessionID := initializeClient(t, server.URL)

	// Create a large prompt
	largeText := strings.Repeat("This is a large text payload. ", 1000)

	promptRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "prompt",
		"params": map[string]interface{}{
			"sessionId": sessionID,
			"prompt": []map[string]interface{}{
				{
					"type": "text",
					"text": largeText,
				},
			},
		},
	}

	body, err := json.Marshal(promptRequest)
	if err != nil {
		t.Fatalf("Failed to marshal large payload: %v", err)
	}

	t.Logf("Payload size: %d bytes", len(body))

	req, err := http.NewRequest(http.MethodPost, server.URL+"/mesnada/acp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ACP-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Large payload request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}
