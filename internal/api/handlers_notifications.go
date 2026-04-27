package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/digiogithub/pando/internal/notify"
	"github.com/digiogithub/pando/internal/pubsub"
)

// handleNotificationsStream serves a Server-Sent Events stream for user-facing
// notifications (LLM retry info, tool errors, LSP diagnostics, etc.).
//
// GET /api/v1/notifications/stream
func (s *Server) handleNotificationsStream(w http.ResponseWriter, r *http.Request) {
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

	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	ctx := r.Context()
	ch := notify.Subscribe(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.Type != pubsub.CreatedEvent {
				continue
			}
			data, err := json.Marshal(event.Payload)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: notification\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}
