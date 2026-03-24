package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
)

// LogEntry is the JSON representation of a log message returned by the API.
type LogEntry struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	Details   string `json:"details,omitempty"`
}

// logMessageToEntry converts a logging.LogMessage into a LogEntry suitable for JSON output.
func logMessageToEntry(msg logging.LogMessage) LogEntry {
	// Extract source from attributes if present.
	source := ""
	detailParts := make([]string, 0, len(msg.Attributes))
	for _, attr := range msg.Attributes {
		if attr.Key == "source" {
			source = attr.Value
		} else {
			detailParts = append(detailParts, attr.Key+"="+attr.Value)
		}
	}

	return LogEntry{
		ID:        msg.ID,
		Timestamp: msg.Time.UTC().Format(time.RFC3339),
		Level:     msg.Level,
		Source:    source,
		Message:   msg.Message,
		Details:   strings.Join(detailParts, " "),
	}
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()
	levelFilter := strings.ToLower(q.Get("level"))
	sinceStr := q.Get("since")
	search := strings.ToLower(q.Get("search"))
	limitStr := q.Get("limit")
	offsetStr := q.Get("offset")

	limit := 100
	offset := 0

	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}
	if offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
			offset = v
		}
	}

	var sinceTime time.Time
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			sinceTime = t
		}
	}

	messages := logging.List()

	// Filter messages.
	filtered := make([]LogEntry, 0, len(messages))
	for _, msg := range messages {
		if levelFilter != "" && msg.Level != levelFilter {
			continue
		}
		if !sinceTime.IsZero() && !msg.Time.After(sinceTime) {
			continue
		}
		if search != "" {
			msgLower := strings.ToLower(msg.Message)
			sourceLower := ""
			for _, attr := range msg.Attributes {
				if attr.Key == "source" {
					sourceLower = strings.ToLower(attr.Value)
					break
				}
			}
			if !strings.Contains(msgLower, search) && !strings.Contains(sourceLower, search) {
				continue
			}
		}
		filtered = append(filtered, logMessageToEntry(msg))
	}

	total := len(filtered)

	// Apply pagination.
	if offset >= len(filtered) {
		filtered = []LogEntry{}
	} else {
		filtered = filtered[offset:]
		if limit < len(filtered) {
			filtered = filtered[:limit]
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   filtered,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) handleLogsStream(w http.ResponseWriter, r *http.Request) {
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

	// Send existing buffered logs first.
	for _, msg := range logging.List() {
		entry := logMessageToEntry(msg)
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
	}
	flusher.Flush()

	// Subscribe to new log events.
	ctx := r.Context()
	ch := logging.Subscribe(ctx)

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
			entry := logMessageToEntry(event.Payload)
			data, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}
