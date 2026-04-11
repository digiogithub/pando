package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// syncWriter wraps an io.Writer with a mutex so the SDK's connection and our
// interceptor can share stdout safely without interleaving writes.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newSyncWriter(w io.Writer) *syncWriter {
	return &syncWriter{w: w}
}

func (sw *syncWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(p)
}

// jsonRPCMsg is a minimal JSON-RPC 2.0 message used by the interceptor.
type jsonRPCMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// StdioTransport wraps the SDK's AgentSideConnection for stdio transport.
type StdioTransport struct {
	agent  *PandoACPAgent
	logger *log.Logger
	conn   *acpsdk.AgentSideConnection
}

func logACPJSONRPC(logger *log.Logger, dir string, payload []byte) {
	if logger == nil {
		return
	}

	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return
	}

	const maxLen = 1200
	if len(trimmed) > maxLen {
		trimmed = trimmed[:maxLen] + "…(truncated)"
	}

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &msg); err == nil {
		method, _ := msg["method"].(string)
		logger.Printf("[ACP JSONRPC %s] method=%s payload=%s", dir, method, trimmed)
		return
	}

	logger.Printf("[ACP JSONRPC %s] payload=%s", dir, trimmed)
}

func newLoggingWriter(w io.Writer, logger *log.Logger) io.Writer {
	return &loggingWriter{w: w, logger: logger}
}

type loggingWriter struct {
	w      io.Writer
	logger *log.Logger
}

func (lw *loggingWriter) Write(p []byte) (int, error) {
	logACPJSONRPC(lw.logger, "out", p)
	return lw.w.Write(p)
}

// NewStdioTransport creates a new stdio transport for the ACP agent.
// A thin interceptor layer sits between raw stdin and the SDK connection to
// handle protocol methods that the Go SDK v0.6.3 does not yet implement
// (e.g. "session/list", which the TypeScript SDK v0.14+ clients send).
func NewStdioTransport(agent *PandoACPAgent, logger *log.Logger) *StdioTransport {
	if logger == nil {
		logger = log.Default()
	}

	// Wrap stdout so both the SDK and our interceptor can write without races.
	stdout := newSyncWriter(os.Stdout)

	// The SDK reads from pipeReader; the interceptor writes non-intercepted
	// messages to pipeWriter and handles intercepted ones directly.
	pipeReader, pipeWriter := io.Pipe()

	go interceptStdin(os.Stdin, stdout, pipeWriter, agent, logger)

	conn := acpsdk.NewAgentSideConnection(agent, newLoggingWriter(stdout, logger), pipeReader)
	agent.SetConnection(conn)

	return &StdioTransport{
		agent:  agent,
		logger: logger,
		conn:   conn,
	}
}

// Run waits until the context is cancelled or the connection closes.
func (t *StdioTransport) Run(ctx context.Context) error {
	t.logger.Printf("[ACP TRANSPORT] Starting stdio transport with interceptor")

	select {
	case <-ctx.Done():
		t.logger.Printf("[ACP TRANSPORT] Context cancelled")
		return ctx.Err()
	case <-t.conn.Done():
		t.logger.Printf("[ACP TRANSPORT] Connection closed")
		return nil
	}
}

// interceptStdin reads JSON-RPC lines from in. Lines whose method is
// "session/list" are handled locally (response written to out). All other
// lines are forwarded verbatim to fwd for the SDK to process.
func interceptStdin(in io.Reader, out *syncWriter, fwd *io.PipeWriter, agent *PandoACPAgent, logger *log.Logger) {
	defer fwd.Close()

	const (
		initialBufSize = 1 * 1024 * 1024
		maxBufSize     = 10 * 1024 * 1024
	)

	scanner := bufio.NewScanner(in)
	buf := make([]byte, 0, initialBufSize)
	scanner.Buffer(buf, maxBufSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var msg jsonRPCMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			logACPJSONRPC(logger, "in", line)
			// Unparseable: forward as-is so the SDK can log/handle it.
			fwd.Write(line)
			fwd.Write([]byte("\n"))
			continue
		}
		logACPJSONRPC(logger, "in", line)

		if msg.Method == "session/list" {
			logger.Printf("[ACP INTERCEPT] Handling session/list (id=%s)", string(msg.ID))
			handleSessionListRPC(msg, out, agent, logger)
			continue
		}

		if msg.Method == "persona/list" {
			logger.Printf("[ACP INTERCEPT] Handling persona/list (id=%s)", string(msg.ID))
			handlePersonaListRPC(msg, out, agent, logger)
			continue
		}

		if msg.Method == "persona/get" {
			logger.Printf("[ACP INTERCEPT] Handling persona/get (id=%s)", string(msg.ID))
			handlePersonaGetRPC(msg, out, agent, logger)
			continue
		}

		if msg.Method == "persona/set" {
			logger.Printf("[ACP INTERCEPT] Handling persona/set (id=%s)", string(msg.ID))
			handlePersonaSetRPC(msg, out, agent, logger)
			continue
		}

		// Forward everything else to the SDK connection.
		fwd.Write(line)
		fwd.Write([]byte("\n"))
	}

	if err := scanner.Err(); err != nil {
		logger.Printf("[ACP TRANSPORT] stdin scanner error: %v", err)
	}
}

// ---- session/list helpers --------------------------------------------------

// sessionListParams are the optional request params for session/list.
type sessionListParams struct {
	Cursor *string `json:"cursor,omitempty"`
	Cwd    *string `json:"cwd,omitempty"`
}

// sessionInfoEntry is one entry in a session/list response.
type sessionInfoEntry struct {
	SessionID string  `json:"sessionId"`
	Cwd       string  `json:"cwd"`
	Title     *string `json:"title,omitempty"`
	UpdatedAt *string `json:"updatedAt,omitempty"`
}

// sessionListResult is the result payload for a session/list response.
type sessionListResult struct {
	Sessions   []sessionInfoEntry `json:"sessions"`
	NextCursor *string            `json:"nextCursor,omitempty"`
}

const sessionListPageSize = 100

// handleSessionListRPC resolves a session/list JSON-RPC request and writes
// the response to out. It is used by both the stdio interceptor and the HTTP
// transport.
func handleSessionListRPC(req jsonRPCMsg, out io.Writer, agent *PandoACPAgent, logger *log.Logger) {
	ctx := context.Background()

	var params sessionListParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &params)
	}

	allSessions, err := agent.ListSessions(ctx)
	if err != nil {
		logger.Printf("[ACP INTERCEPT] session/list error: %v", err)
		writeRPCError(out, req.ID, -32603, "failed to list sessions: "+err.Error())
		return
	}

	// Apply cursor-based pagination (cursor = last session ID of previous page).
	startIdx := 0
	if params.Cursor != nil && *params.Cursor != "" {
		for i, s := range allSessions {
			if s.ID == *params.Cursor {
				startIdx = i + 1
				break
			}
		}
	}

	end := startIdx + sessionListPageSize
	hasMore := false
	if end < len(allSessions) {
		hasMore = true
	} else {
		end = len(allSessions)
	}

	page := allSessions[startIdx:end]
	entries := make([]sessionInfoEntry, 0, len(page))
	workDir := agent.workDir
	for _, s := range page {
		entry := sessionInfoEntry{
			SessionID: s.ID,
			Cwd:       workDir,
		}
		if s.Title != "" {
			t := s.Title
			entry.Title = &t
		}
		if s.UpdatedAt != 0 {
			ts := time.Unix(s.UpdatedAt, 0).UTC().Format(time.RFC3339)
			entry.UpdatedAt = &ts
		}
		entries = append(entries, entry)
	}

	result := sessionListResult{Sessions: entries}
	if hasMore && len(page) > 0 {
		cursor := page[len(page)-1].ID
		result.NextCursor = &cursor
	}

	writeRPCResult(out, req.ID, result)
	if logger != nil {
		logger.Printf("[ACP INTERCEPT] session/list: returned %d sessions (hasMore=%v)", len(entries), hasMore)
	}
}

// ---- persona/* helpers -------------------------------------------------------

// personaListResult is the result payload for a persona/list response.
type personaListResult struct {
	Personas []string `json:"personas"`
}

// personaGetResult is the result payload for a persona/get response.
type personaGetResult struct {
	Active string `json:"active"`
}

// personaSetParams are the request params for persona/set.
type personaSetParams struct {
	Name string `json:"name"`
}

// handlePersonaListRPC returns all available persona names.
func handlePersonaListRPC(req jsonRPCMsg, out io.Writer, agent *PandoACPAgent, logger *log.Logger) {
	personas := agent.agentService.ListPersonas()
	if personas == nil {
		personas = []string{}
	}
	writeRPCResult(out, req.ID, personaListResult{Personas: personas})
	if logger != nil {
		logger.Printf("[ACP INTERCEPT] persona/list: returned %d personas", len(personas))
	}
}

// handlePersonaGetRPC returns the currently active persona name.
func handlePersonaGetRPC(req jsonRPCMsg, out io.Writer, agent *PandoACPAgent, logger *log.Logger) {
	active := agent.agentService.GetActivePersona()
	writeRPCResult(out, req.ID, personaGetResult{Active: active})
	if logger != nil {
		logger.Printf("[ACP INTERCEPT] persona/get: active=%q", active)
	}
}

// handlePersonaSetRPC sets the active persona by name.
func handlePersonaSetRPC(req jsonRPCMsg, out io.Writer, agent *PandoACPAgent, logger *log.Logger) {
	var params personaSetParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &params)
	}
	if err := agent.agentService.SetActivePersona(params.Name); err != nil {
		writeRPCError(out, req.ID, -32602, "invalid persona: "+err.Error())
		return
	}
	writeRPCResult(out, req.ID, personaGetResult{Active: params.Name})
	if logger != nil {
		logger.Printf("[ACP INTERCEPT] persona/set: active=%q", params.Name)
	}
}

// writeRPCResult writes a JSON-RPC 2.0 success response as a newline-terminated JSON line.
func writeRPCResult(out io.Writer, id json.RawMessage, result interface{}) {
	type rpcResponse struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  interface{}     `json:"result"`
	}
	b, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
	b = append(b, '\n')
	out.Write(b) //nolint:errcheck
}

// writeRPCError writes a JSON-RPC 2.0 error response as a newline-terminated JSON line.
func writeRPCError(out io.Writer, id json.RawMessage, code int, message string) {
	type rpcErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type rpcResponse struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcErr          `json:"error"`
	}
	b, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Error: rpcErr{Code: code, Message: message}})
	b = append(b, '\n')
	out.Write(b) //nolint:errcheck
}
