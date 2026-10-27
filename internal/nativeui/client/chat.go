package client

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ChatRequest is the body of POST /api/v1/chat/stream.
type ChatRequest struct {
	// SessionID is empty to start a new session (its id comes in the first
	// `session` event).
	SessionID string `json:"sessionId,omitempty"`
	Prompt    string `json:"prompt"`
	// Model optionally selects a model for this session ("auto" for model
	// auto mode); empty keeps the session's.
	Model string `json:"model,omitempty"`
}

// StreamOutcome tells how a chat stream ended.
type StreamOutcome struct {
	// SessionID is the session the stream was about.
	SessionID string
	// Done reports that the server sent `done`. A stream that ended without
	// it (slash commands, a submit error, a dropped connection with the run
	// already over) should be followed by a history reload.
	Done bool
	// Reattached counts the reattachments after a dropped body.
	Reattached int
	// LastError is the text of the last `error` event, if any.
	LastError string
}

// ReattachPolicy tunes how a dropped run stream is reattached.
type ReattachPolicy struct {
	// MaxAttempts bounds consecutive reattachments (default 20).
	MaxAttempts int
	// Delay is the first pause before reattaching (default 300ms); it
	// doubles per failed attempt up to MaxDelay (default 5s).
	Delay, MaxDelay time.Duration
}

func (p ReattachPolicy) withDefaults() ReattachPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 20
	}
	if p.Delay <= 0 {
		p.Delay = 300 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 5 * time.Second
	}
	return p
}

// DefaultReattach is the policy used by ChatStream and AttachStream.
var DefaultReattach = ReattachPolicy{}

// ChatStream sends a prompt and calls fn with every event of the run, on the
// calling goroutine, until `done`, ctx is canceled, or the run cannot be
// followed any more.
//
// The agent run survives HTTP disconnects. When the body closes without
// `done` while the run is still active (GET /sessions/{id}/pending), the
// client emits a synthetic EventReattach and follows the run again through
// GET /sessions/{id}/stream, which replays it from its start: consumers must
// discard the partial turn on EventReattach.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, fn func(StreamEvent)) (StreamOutcome, error) {
	out := StreamOutcome{SessionID: req.SessionID}
	err := c.OpenStream(ctx, http.MethodPost, "/api/v1/chat/stream", req, c.collect(&out, fn))
	return c.follow(ctx, &out, err, fn, DefaultReattach)
}

// AttachStream follows the run currently active for a session (replaying it
// from its start), with the same reattach behaviour as ChatStream. When no
// run is active the server answers `done` at once.
func (c *Client) AttachStream(ctx context.Context, sessionID string, fn func(StreamEvent)) (StreamOutcome, error) {
	out := StreamOutcome{SessionID: sessionID}
	err := c.OpenStream(ctx, http.MethodGet, "/api/v1/sessions/"+pathEscape(sessionID)+"/stream", nil, c.collect(&out, fn))
	return c.follow(ctx, &out, err, fn, DefaultReattach)
}

func (c *Client) collect(out *StreamOutcome, fn func(StreamEvent)) func(SSEFrame) error {
	return func(f SSEFrame) error {
		ev := ParseStreamEvent(f)
		if ev.Type == EventSession && ev.SessionID != "" {
			out.SessionID = ev.SessionID
		}
		if ev.Type == EventError {
			out.LastError = ev.Error
		}
		fn(ev)
		if ev.Type == EventDone {
			out.Done = true
			return errStopSSE
		}
		return nil
	}
}

// follow reattaches a stream that ended without `done` while its run is
// still active.
func (c *Client) follow(ctx context.Context, out *StreamOutcome, err error, fn func(StreamEvent), pol ReattachPolicy) (StreamOutcome, error) {
	pol = pol.withDefaults()
	delay := pol.Delay
	failures := 0
	for {
		if out.Done {
			return *out, nil
		}
		if cerr := ctx.Err(); cerr != nil {
			return *out, cerr
		}
		// Errors that reattaching cannot fix.
		if err != nil && (errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrBasicAuthRequired) || isClientError(err)) {
			return *out, err
		}
		if out.SessionID == "" {
			// Never learned the session: nothing to reattach to.
			if err == nil {
				err = errors.New("pando: stream ended before the session was known")
			}
			return *out, err
		}
		if out.Reattached >= pol.MaxAttempts {
			if err == nil {
				err = errors.New("pando: run stream kept dropping")
			}
			return *out, err
		}
		if err != nil {
			failures++
		} else {
			failures = 0
		}
		if failures > 0 {
			if !sleepCtx(ctx, delay) {
				return *out, ctx.Err()
			}
			delay = min(delay*2, pol.MaxDelay)
		} else {
			if !sleepCtx(ctx, pol.Delay) {
				return *out, ctx.Err()
			}
		}
		pending, perr := c.Pending(ctx, out.SessionID)
		if perr != nil {
			if failures >= pol.MaxAttempts {
				return *out, perr
			}
			err = perr
			out.Reattached++
			continue
		}
		if !pending.Running {
			// The run is over: the history holds its result.
			return *out, nil
		}
		out.Reattached++
		fn(StreamEvent{Type: EventReattach, SessionID: out.SessionID})
		err = c.OpenStream(ctx, http.MethodGet, "/api/v1/sessions/"+pathEscape(out.SessionID)+"/stream", nil, c.collect(out, fn))
	}
}

func isClientError(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 && ae.Status != http.StatusRequestTimeout && ae.Status != http.StatusTooManyRequests
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ErrNotBusy is returned by Steer when the session has no active run: send
// the prompt as a normal chat message instead (see SendOrSteer).
var ErrNotBusy = errors.New("pando: session not busy")

// Steer queues mid-run feedback (POST /sessions/{id}/steer). It returns
// ErrNotBusy (409) when no run is active.
func (c *Client) Steer(ctx context.Context, sessionID, prompt string) error {
	err := c.Post(ctx, "/api/v1/sessions/"+pathEscape(sessionID)+"/steer", map[string]string{"prompt": prompt}, nil)
	if IsStatus(err, http.StatusConflict) {
		return ErrNotBusy
	}
	return err
}

// Cancel stops the active run of a session.
func (c *Client) Cancel(ctx context.Context, sessionID string) error {
	return c.Post(ctx, "/api/v1/sessions/"+pathEscape(sessionID)+"/cancel", map[string]any{}, nil)
}

// RespondPermission answers a permission request: action is allow,
// allow_session or deny.
func (c *Client) RespondPermission(ctx context.Context, sessionID, id, action string) error {
	return c.Post(ctx, "/api/v1/permissions/respond", map[string]string{"id": id, "sessionId": sessionID, "action": action}, nil)
}
