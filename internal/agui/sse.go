package agui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// defaultMaxRequestBytes caps a RunAgentInput payload. The transcript grows with
// the conversation, so the limit is generous but finite.
const defaultMaxRequestBytes = 8 << 20 // 8 MiB

// ErrStreamingUnsupported is returned when the ResponseWriter cannot flush, in
// which case SSE cannot be served at all.
var ErrStreamingUnsupported = errors.New("agui: streaming unsupported by the response writer")

// SSEWriter serializes AG-UI events to the Server-Sent Events wire format.
//
// AG-UI puts the discriminator inside the JSON payload rather than in the SSE
// `event:` field, so every frame is a bare `data:` line. Clients (including
// AG-UI's HttpAgent) parse the JSON and switch on `type`.
type SSEWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	closed  bool
}

// NewSSEWriter writes the SSE response headers and returns a writer bound to w.
// Headers other than the streaming ones (CORS, auth) must already be set.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrStreamingUnsupported
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Defeat proxy buffering, which would otherwise hold deltas back.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &SSEWriter{w: w, flusher: flusher}, nil
}

// Write encodes and flushes a single event.
func (s *SSEWriter) Write(ev Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("agui: encode %s: %w", ev.EventType(), err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return http.ErrBodyNotAllowed
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", payload); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteAll encodes a batch, stopping at the first failure.
func (s *SSEWriter) WriteAll(events []Event) error {
	for _, ev := range events {
		if err := s.Write(ev); err != nil {
			return err
		}
	}
	return nil
}

// Comment emits an SSE comment. Used as a heartbeat to keep intermediaries from
// dropping an idle connection while the agent is busy in a long tool call.
func (s *SSEWriter) Comment(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return http.ErrBodyNotAllowed
	}
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// Close marks the stream finished. Further writes are refused; the HTTP handler
// returning is what actually terminates the response.
func (s *SSEWriter) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}
