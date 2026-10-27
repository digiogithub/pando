package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ListSessions returns a page of sessions, newest first. limit <= 0 uses the
// server default.
func (c *Client) ListSessions(ctx context.Context, limit, offset int) (SessionList, error) {
	path := "/api/v1/sessions"
	if limit > 0 || offset > 0 {
		path = fmt.Sprintf("%s?limit=%d&offset=%d", path, limit, offset)
	}
	var out SessionList
	err := c.Get(ctx, path, &out)
	return out, err
}

// GetSession returns a session with its history, oldest message first.
func (c *Client) GetSession(ctx context.Context, id string) (SessionDetail, error) {
	var out SessionDetail
	err := c.Get(ctx, "/api/v1/sessions/"+pathEscape(id), &out)
	return out, err
}

// RenameSession sets a session's title.
func (c *Client) RenameSession(ctx context.Context, id, title string) error {
	return c.Patch(ctx, "/api/v1/sessions/"+pathEscape(id), map[string]string{"title": title}, nil)
}

// DeleteSession deletes a session and its messages.
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.Delete(ctx, "/api/v1/sessions/"+pathEscape(id), nil)
}

// Pending returns a session's run state and waiting requests.
func (c *Client) Pending(ctx context.Context, id string) (Pending, error) {
	var out Pending
	err := c.Get(ctx, "/api/v1/sessions/"+pathEscape(id)+"/pending", &out)
	return out, err
}

// ListModels returns the models of every configured provider account.
func (c *Client) ListModels(ctx context.Context) (ModelList, error) {
	var out ModelList
	err := c.Get(ctx, "/api/v1/models", &out)
	return out, err
}

// SessionModel returns the model a session runs on.
func (c *Client) SessionModel(ctx context.Context, sessionID string) (SessionModel, error) {
	var out SessionModel
	err := c.Get(ctx, "/api/v1/sessions/"+pathEscape(sessionID)+"/model", &out)
	return out, err
}

// SetActiveModel selects a model. With a sessionID the selection is scoped
// to that session; always pass it from a chat view, or the selection becomes
// the persisted coder model of every session (lesson of the GPUI client).
func (c *Client) SetActiveModel(ctx context.Context, model, sessionID string) error {
	return c.Put(ctx, "/api/v1/models/active", map[string]string{"model": model, "sessionId": sessionID}, nil)
}

// SessionEvent is an event of GET /api/v1/sessions/events: Type is "ready",
// "session_upsert" (Session set) or "session_deleted" (ID set).
type SessionEvent struct {
	Type    string
	Session SessionSummary
	ID      string
}

// SubscribeSessionEvents follows the session list stream until ctx ends,
// reconnecting with backoff. fn runs on the subscriber goroutine (the
// caller's): post UI changes through the window's Update.
func (c *Client) SubscribeSessionEvents(ctx context.Context, fn func(SessionEvent)) error {
	return c.Subscribe(ctx, "/api/v1/sessions/events", func(f SSEFrame) {
		ev := SessionEvent{Type: f.Event}
		switch f.Event {
		case "session_upsert":
			var p struct {
				Session SessionSummary `json:"session"`
			}
			if json.Unmarshal([]byte(f.Data), &p) != nil {
				return
			}
			ev.Session, ev.ID = p.Session, p.Session.ID
		case "session_deleted":
			var p struct {
				ID string `json:"id"`
			}
			if json.Unmarshal([]byte(f.Data), &p) != nil {
				return
			}
			ev.ID = p.ID
		}
		fn(ev)
	})
}

// Subscribe follows a long-lived SSE endpoint (/sessions/events,
// /notifications/stream, /config/events, /projects/events, /logs/stream)
// until ctx ends, reconnecting with exponential backoff (0.5s to 15s) after
// errors and closed bodies. It returns ctx's error, or an authorization
// error that a reconnect cannot fix.
func (c *Client) Subscribe(ctx context.Context, path string, fn func(SSEFrame)) error {
	delay := 500 * time.Millisecond
	for {
		start := time.Now()
		err := c.OpenStream(ctx, http.MethodGet, path, nil, func(f SSEFrame) error {
			fn(f)
			return nil
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == ErrUnauthorized || err == ErrBasicAuthRequired || IsStatus(err, http.StatusNotFound) {
			return err
		}
		if time.Since(start) > 30*time.Second {
			delay = 500 * time.Millisecond // it was healthy for a while
		}
		if !sleepCtx(ctx, delay) {
			return ctx.Err()
		}
		delay = min(delay*2, 15*time.Second)
	}
}
