package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gen2brain/beeep"
)

// sseNotification mirrors the JSON payload from /api/v1/notifications/stream.
type sseNotification struct {
	ID      string `json:"id"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

// startNotificationListener connects to the Pando SSE notification stream and
// shows native OS notifications when the desktop window is not focused.
// It reconnects automatically on disconnection with exponential backoff.
func (a *App) startNotificationListener(ctx context.Context) {
	url := strings.TrimRight(a.pandoURL, "/") + "/api/v1/notifications/stream"
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := a.listenSSE(ctx, url)
		if err != nil && ctx.Err() == nil {
			slog.Warn("desktop: notification SSE disconnected, reconnecting",
				"error", err, "backoff", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff capped at 30s.
		backoff = min(backoff*2, 30*time.Second)
	}
}

// listenSSE opens an SSE connection and processes events until error or context
// cancellation. Returns nil on clean shutdown.
func (a *App) listenSSE(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Reset backoff on successful connection.
	scanner := bufio.NewScanner(resp.Body)
	var eventType string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") && eventType == "notification" {
			data := strings.TrimPrefix(line, "data: ")
			a.handleNotificationData(data)
			eventType = ""
			continue
		}

		// Empty line resets event state per SSE spec.
		if line == "" {
			eventType = ""
		}
	}

	return scanner.Err()
}

// handleNotificationData parses a JSON notification payload and shows an OS
// notification if the desktop window is not focused.
func (a *App) handleNotificationData(data string) {
	var n sseNotification
	if err := json.Unmarshal([]byte(data), &n); err != nil {
		slog.Debug("desktop: failed to parse notification", "error", err)
		return
	}

	// Only show OS notifications when the window is not focused.
	if a.windowFocused.Load() {
		return
	}

	// Filter for session completion notifications from the agent source.
	if n.Source != "agent" {
		return
	}

	title := "Pando"
	if err := beeep.Notify(title, n.Message, ""); err != nil {
		slog.Debug("desktop: failed to show OS notification", "error", err)
	}
}
