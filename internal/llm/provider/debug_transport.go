package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
)

var httpDebugSeq atomic.Int64

func nextHTTPDebugSeq() int {
	return int(httpDebugSeq.Add(1))
}

// debugRoundTripper is an http.RoundTripper that logs all HTTP requests and
// responses when debug mode is enabled. Sensitive headers (Authorization,
// x-api-key, api-key) are redacted before writing to disk.
//
// For regular JSON responses the full body is captured and written as
// {seq}_http_resp.json inside the session log directory.
//
// For SSE streams (text/event-stream) the response metadata is written to
// {seq}_http_resp.json and the raw stream chunks are appended line-by-line
// to {seq}_http_stream.log via a transparent tee reader so the provider
// client continues to receive the stream uninterrupted.
type debugRoundTripper struct {
	wrapped http.RoundTripper
}

// newDebugRoundTripper returns an http.RoundTripper that wraps base (falling
// back to http.DefaultTransport) and logs all traffic to the session log dir.
func newDebugRoundTripper(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &debugRoundTripper{wrapped: base}
}

// newDebugHTTPClient is a convenience helper used by each provider to create
// an *http.Client that injects debug logging into every HTTP call.
func newDebugHTTPClient() *http.Client {
	return &http.Client{Transport: newDebugRoundTripper(nil)}
}

func (d *debugRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	seq := nextHTTPDebugSeq()

	// Extract session ID from context so log files land in the right directory.
	sessionId := ""
	if sid, ok := req.Context().Value(toolsPkg.SessionIDContextKey).(string); ok {
		sessionId = sid
	}

	// ── Request ──────────────────────────────────────────────────────────────
	reqInfo := map[string]any{
		"method":  req.Method,
		"url":     req.URL.String(),
		"headers": sanitizeHeaders(req.Header),
	}
	if req.Body != nil && req.Body != http.NoBody {
		bodyBytes, err := io.ReadAll(req.Body)
		if err == nil {
			// Restore body so the SDK can still send it.
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			reqInfo["body_bytes"] = len(bodyBytes)
			var bodyJSON any
			if json.Unmarshal(bodyBytes, &bodyJSON) == nil {
				reqInfo["body"] = bodyJSON
			} else {
				reqInfo["body_raw"] = string(bodyBytes)
			}
		}
	}

	logging.Debug("HTTP provider request", "seq", seq, "method", req.Method, "url", req.URL.String())
	if fp := logging.WriteHTTPRequest(sessionId, seq, reqInfo); fp != "" {
		logging.Debug("HTTP request logged", "seq", seq, "file", fp)
	}

	// ── Execute ───────────────────────────────────────────────────────────────
	resp, err := d.wrapped.RoundTrip(req)
	if err != nil {
		logging.Error("HTTP provider request failed", "seq", seq, "url", req.URL.String(), "error", err)
		logging.WriteHTTPResponse(sessionId, seq, map[string]any{"error": err.Error()})
		return nil, err
	}

	// ── Response ──────────────────────────────────────────────────────────────
	respInfo := map[string]any{
		"status":      resp.Status,
		"status_code": resp.StatusCode,
		"headers":     sanitizeHeaders(resp.Header),
	}
	logging.Debug("HTTP provider response", "seq", seq, "status", resp.Status, "url", req.URL.String())

	contentType := resp.Header.Get("Content-Type")
	isStreaming := strings.Contains(contentType, "text/event-stream")

	if isStreaming {
		// Write response metadata now; stream chunks are appended as they flow.
		if fp := logging.WriteHTTPResponse(sessionId, seq, respInfo); fp != "" {
			logging.Debug("HTTP response metadata logged", "seq", seq, "file", fp)
		}
		resp.Body = newStreamTeeReadCloser(resp.Body, sessionId, seq)
	} else {
		// Buffer the full response body for logging, then restore it.
		if resp.Body != nil {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err == nil {
				resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				respInfo["body_bytes"] = len(bodyBytes)
				var bodyJSON any
				if json.Unmarshal(bodyBytes, &bodyJSON) == nil {
					respInfo["body"] = bodyJSON
				} else {
					respInfo["body_raw"] = string(bodyBytes)
				}
			}
		}
		logging.WriteHTTPResponse(sessionId, seq, respInfo)
	}

	return resp, nil
}

// sanitizeHeaders returns a flattened map of headers with sensitive values
// replaced by "[REDACTED]".
func sanitizeHeaders(headers http.Header) map[string]string {
	sensitive := map[string]bool{
		"authorization": true,
		"x-api-key":     true,
		"api-key":       true,
	}
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		if sensitive[strings.ToLower(k)] {
			result[k] = "[REDACTED]"
		} else {
			result[k] = strings.Join(v, ", ")
		}
	}
	return result
}

// ── Stream helpers ────────────────────────────────────────────────────────────

// streamTeeReadCloser wraps a response body and writes every byte read from it
// to the session stream log file while forwarding all reads to the caller.
type streamTeeReadCloser struct {
	reader io.Reader
	closer io.Closer
}

func newStreamTeeReadCloser(body io.ReadCloser, sessionId string, seq int) io.ReadCloser {
	w := &streamLogWriter{sessionId: sessionId, seq: seq}
	return &streamTeeReadCloser{
		reader: io.TeeReader(body, w),
		closer: body,
	}
}

func (s *streamTeeReadCloser) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s *streamTeeReadCloser) Close() error {
	return s.closer.Close()
}

// streamLogWriter is the write-side of the tee: it appends each chunk to
// {seq}_http_stream.log inside the session log directory.
type streamLogWriter struct {
	sessionId string
	seq       int
}

func (w *streamLogWriter) Write(p []byte) (int, error) {
	logging.AppendHTTPStream(w.sessionId, w.seq, string(p))
	return len(p), nil
}
