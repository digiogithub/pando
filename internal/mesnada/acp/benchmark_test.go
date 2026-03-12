package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// BenchmarkHTTPTransport_Initialize tests initialization performance
func BenchmarkHTTPTransport_Initialize(b *testing.B) {
	agent := NewSimpleACPAgent("1.0.0-bench", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "bench-client",
				"version": "1.0.0",
			},
		},
	}

	body, _ := json.Marshal(initRequest)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		transport.HandleRequest(w, req)
	}
}

// BenchmarkHTTPTransport_ConcurrentSessions tests concurrent session handling
func BenchmarkHTTPTransport_ConcurrentSessions(b *testing.B) {
	agent := NewSimpleACPAgent("1.0.0-bench", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "bench-client",
				"version": "1.0.0",
			},
		},
	}

	body, _ := json.Marshal(initRequest)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			transport.HandleRequest(w, req)
		}
	})
}

// BenchmarkClientConnection_ReadFile tests file reading performance
func BenchmarkClientConnection_ReadFile(b *testing.B) {
	mock := &mockAgentSideConnection{
		readTextFileFunc: func(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
			return acpsdk.ReadTextFileResponse{Content: "benchmark content that is somewhat long to simulate realistic file sizes"}, nil
		},
	}

	conn := NewACPClientConnection("bench-session", mock, "/workspace", nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := conn.ReadTextFile(ctx, "test.txt")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClientConnection_WriteFile tests file writing performance
func BenchmarkClientConnection_WriteFile(b *testing.B) {
	mock := &mockAgentSideConnection{
		writeTextFileFunc: func(ctx context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
			return acpsdk.WriteTextFileResponse{}, nil
		},
	}

	conn := NewACPClientConnection("bench-session", mock, "/workspace", nil)
	ctx := context.Background()
	content := "benchmark content that is somewhat long to simulate realistic file sizes"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := conn.WriteTextFile(ctx, "test.txt", content)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClientConnection_CreateTerminal tests terminal creation performance
func BenchmarkClientConnection_CreateTerminal(b *testing.B) {
	terminalCounter := 0
	mock := &mockAgentSideConnection{
		createTerminalFunc: func(ctx context.Context, req acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
			terminalCounter++
			return acpsdk.CreateTerminalResponse{TerminalId: "terminal-bench"}, nil
		},
	}

	conn := NewACPClientConnection("bench-session", mock, "/workspace", nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := conn.CreateTerminal(ctx, "echo", []string{"test"}, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPathValidation tests path validation performance
func BenchmarkPathValidation(b *testing.B) {
	conn := NewACPClientConnection("bench-session", &mockAgentSideConnection{}, "/workspace", nil)

	testPaths := []string{
		"file.txt",
		"subdir/file.txt",
		"a/b/c/d/file.txt",
		"../../../etc/passwd",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := testPaths[i%len(testPaths)]
		_ = conn.validatePath(path)
	}
}

// BenchmarkPermissionQueue tests permission queue performance
func BenchmarkPermissionQueue(b *testing.B) {
	queue := NewPermissionQueue()
	title := "Test Tool"

	req := acpsdk.RequestPermissionRequest{
		SessionId: "session-1",
		ToolCall: acpsdk.RequestPermissionToolCall{
			Title: &title,
		},
		Options: []acpsdk.PermissionOption{
			{OptionId: "approve", Name: "Approve"},
			{OptionId: "deny", Name: "Deny"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		requestID := queue.QueuePermission("task-1", "session-1", req)
		_ = queue.ResolvePermission(requestID, acpsdk.NewRequestPermissionOutcomeSelected("approve"))
	}
}

// BenchmarkPermissionQueue_Concurrent tests concurrent permission queue operations
func BenchmarkPermissionQueue_Concurrent(b *testing.B) {
	queue := NewPermissionQueue()
	title := "Test Tool"

	req := acpsdk.RequestPermissionRequest{
		SessionId: "session-1",
		ToolCall: acpsdk.RequestPermissionToolCall{
			Title: &title,
		},
		Options: []acpsdk.PermissionOption{
			{OptionId: "approve", Name: "Approve"},
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			requestID := queue.QueuePermission("task-1", "session-1", req)
			_ = queue.ResolvePermission(requestID, acpsdk.NewRequestPermissionOutcomeSelected("approve"))
			i++
		}
	})
}

// BenchmarkHTTPTransport_SessionManagement tests session management performance
func BenchmarkHTTPTransport_SessionManagement(b *testing.B) {
	agent := NewSimpleACPAgent("1.0.0-bench", log.Default())
	config := HTTPTransportConfig{
		MaxSessions:  1000,
		IdleTimeout:  30 * time.Minute,
		EventBufSize: 100,
	}
	transport := NewHTTPTransport(agent, log.Default(), config)

	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "bench-client",
				"version": "1.0.0",
			},
		},
	}

	body, _ := json.Marshal(initRequest)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create session
		req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		transport.HandleRequest(w, req)

		// Close session
		sessionID := w.Header().Get("ACP-Session-Id")
		if sessionID != "" {
			_ = transport.CloseSession(sessionID)
		}
	}
}

// BenchmarkHTTPTransport_Health tests health check performance
func BenchmarkHTTPTransport_Health(b *testing.B) {
	agent := NewSimpleACPAgent("1.0.0-bench", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/mesnada/acp/health", nil)
		w := httptest.NewRecorder()
		transport.HandleHealth(w, req)
	}
}

// BenchmarkJSONRPCEncoding tests JSON-RPC encoding/decoding performance
func BenchmarkJSONRPCEncoding(b *testing.B) {
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "bench-client",
				"version": "1.0.0",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := json.Marshal(request)
		if err != nil {
			b.Fatal(err)
		}

		var decoded map[string]interface{}
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSessionConcurrency tests concurrent operations on different sessions
func BenchmarkSessionConcurrency(b *testing.B) {
	agent := NewSimpleACPAgent("1.0.0-bench", log.Default())
	config := HTTPTransportConfig{
		MaxSessions:  100,
		IdleTimeout:  30 * time.Minute,
		EventBufSize: 100,
	}
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Create multiple sessions first
	numSessions := 10
	sessionIDs := make([]string, numSessions)

	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "bench-client",
				"version": "1.0.0",
			},
		},
	}

	body, _ := json.Marshal(initRequest)

	for i := 0; i < numSessions; i++ {
		req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		transport.HandleRequest(w, req)
		sessionIDs[i] = w.Header().Get("ACP-Session-Id")
	}

	// Benchmark concurrent requests across sessions
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sessionID := sessionIDs[i%numSessions]
			req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("ACP-Session-Id", sessionID)
			w := httptest.NewRecorder()
			transport.HandleRequest(w, req)
			i++
		}
	})
}

// BenchmarkMemoryAllocation tests memory allocation patterns
func BenchmarkMemoryAllocation(b *testing.B) {
	agent := NewSimpleACPAgent("1.0.0-bench", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "bench-client",
				"version": "1.0.0",
			},
		},
	}

	body, _ := json.Marshal(initRequest)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		transport.HandleRequest(w, req)
	}
}

// BenchmarkEventStreaming tests SSE event streaming performance
func BenchmarkEventStreaming(b *testing.B) {
	agent := NewSimpleACPAgent("1.0.0-bench", log.Default())
	config := DefaultHTTPTransportConfig()
	transport := NewHTTPTransport(agent, log.Default(), config)

	// Create session first
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": 1,
			"clientInfo": map[string]string{
				"name":    "bench-client",
				"version": "1.0.0",
			},
		},
	}

	body, _ := json.Marshal(initRequest)
	req1 := httptest.NewRequest(http.MethodPost, "/mesnada/acp", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	transport.HandleRequest(w1, req1)
	sessionID := w1.Header().Get("ACP-Session-Id")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		req := httptest.NewRequest(http.MethodGet, "/mesnada/acp/events", nil)
		req.Header.Set("ACP-Session-Id", sessionID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			transport.HandleSSE(w, req)
		}()

		time.Sleep(5 * time.Millisecond)
		cancel()
		wg.Wait()
	}
}
