package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// handleConfigEvents serves a Server-Sent Events stream for configuration
// changes. Web-UI clients connect here to receive live hot-reload notifications
// without polling. Each event is emitted as a JSON-encoded ConfigChangeEvent.
//
// GET /api/v1/config/events
func (s *Server) handleConfigEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Subscribe to the config event bus.
	ch := make(chan config.ConfigChangeEvent, 8)
	config.Bus.Subscribe(ch)
	defer config.Bus.Unsubscribe(ch)

	// Send an initial "connected" heartbeat so the client knows the stream is live.
	fmt.Fprintf(w, "event: connected\ndata: {\"ts\":%d}\n\n", time.Now().UnixMilli())
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
