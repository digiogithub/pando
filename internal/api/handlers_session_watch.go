package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

const (
	// sessionEventCoalesce is the window in which successive events for one
	// row collapse into its latest state. A follower streams a message as one
	// update per content delta; this keeps the result looking like streaming
	// without flooding the client.
	sessionEventCoalesce = 200 * time.Millisecond
	// sessionWatchPoll is how often /watch rechecks that the session is still
	// busy elsewhere.
	sessionWatchPoll = time.Second
	// sessionEventsKeepAlive is the interval of the SSE comment that keeps
	// idle /sessions/events connections open through proxies.
	sessionEventsKeepAlive = 25 * time.Second
)

// coalescer keeps the latest pending event per row id, in first-seen order.
type coalescer[T any] struct {
	items map[string]pubsub.Event[T]
	order []string
}

func newCoalescer[T any]() *coalescer[T] {
	return &coalescer[T]{items: make(map[string]pubsub.Event[T])}
}

// add records ev for id. The latest payload wins; a row created inside the
// window stays "created", and a deletion always wins.
func (c *coalescer[T]) add(id string, ev pubsub.Event[T]) {
	prev, had := c.items[id]
	if !had {
		c.order = append(c.order, id)
	} else if prev.Type == pubsub.CreatedEvent && ev.Type == pubsub.UpdatedEvent {
		ev.Type = pubsub.CreatedEvent
	}
	c.items[id] = ev
}

func (c *coalescer[T]) empty() bool { return len(c.order) == 0 }

// take returns the pending events in first-seen order and resets the window.
func (c *coalescer[T]) take() []pubsub.Event[T] {
	out := make([]pubsub.Event[T], 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.items[id])
	}
	c.items = make(map[string]pubsub.Event[T])
	c.order = nil
	return out
}

// sseHeaders prepares w for an SSE response.
func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// handleSessionWatch follows a session that another Pando process (an IPC
// follower such as a mesnada subagent) is running. That process writes the
// database and reports its changes to this leader, which republishes them on
// its session/message brokers (see app/follower_sync.go). The stream pushes
// them as data so the client patches its view in place:
//
//	event: session        {"sessionId", "busyElsewhere"}         (first event)
//	event: message        {"sessionId", "message": <message.Message>}
//	event: message_deleted {"sessionId", "id"}
//	event: session_update {"sessionId", "session": <session list item>}
//	event: done           {"sessionId"}                          (run ended)
//
// message carries the same JSON as GET /api/v1/sessions/{id} "messages".
// Updates of one message are coalesced (latest state wins) per window.
// GET /api/v1/sessions/{id}/watch
func (s *Server) handleSessionWatch(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	sseHeaders(w)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var sessCh <-chan pubsub.Event[session.Session]
	var msgCh <-chan pubsub.Event[message.Message]
	if s.app != nil && s.app.Sessions != nil {
		sessCh = s.app.Sessions.Subscribe(ctx)
	}
	if s.app != nil && s.app.Messages != nil {
		msgCh = s.app.Messages.Subscribe(ctx)
	}

	busy := s.sessionBusyElsewhere(sessionID)
	writeSSEEvent(w, flusher, "session", map[string]any{"sessionId": sessionID, "busyElsewhere": busy})
	if !busy {
		writeSSEEvent(w, flusher, "done", map[string]string{"sessionId": sessionID})
		return
	}

	msgs := newCoalescer[message.Message]()
	sessions := newCoalescer[session.Session]()
	poll := time.NewTicker(sessionWatchPoll)
	defer poll.Stop()
	var window <-chan time.Time
	var lastSession *session.Session
	arm := func() {
		if window == nil {
			window = time.After(sessionEventCoalesce)
		}
	}
	flush := func() {
		window = nil
		for _, ev := range msgs.take() {
			if ev.Type == pubsub.DeletedEvent {
				writeSSEEvent(w, flusher, "message_deleted", map[string]string{"sessionId": sessionID, "id": ev.Payload.ID})
				continue
			}
			writeSSEEvent(w, flusher, "message", map[string]any{"sessionId": sessionID, "message": ev.Payload})
		}
		for _, ev := range sessions.take() {
			if ev.Type == pubsub.DeletedEvent {
				continue // the global /sessions/events stream reports deletions
			}
			writeSSEEvent(w, flusher, "session_update", map[string]any{"sessionId": sessionID, "session": s.sessionListItem(ctx, ev.Payload)})
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sessCh:
			if !ok {
				sessCh = nil
				continue
			}
			if ev.Payload.ID == sessionID {
				sessions.add(ev.Payload.ID, ev)
				if ev.Type != pubsub.DeletedEvent {
					p := ev.Payload
					lastSession = &p
				}
				arm()
			}
		case ev, ok := <-msgCh:
			if !ok {
				msgCh = nil
				continue
			}
			if ev.Payload.SessionID == sessionID {
				msgs.add(ev.Payload.ID, ev)
				arm()
			}
		case <-window:
			flush()
		case <-poll.C:
			if s.sessionBusyElsewhere(sessionID) {
				continue
			}
			// Hand over what is still in the window, then the final state.
			flush()
			// Re-send the last known row with its run state recomputed (the
			// client also clears busy_elsewhere on done).
			if lastSession != nil {
				writeSSEEvent(w, flusher, "session_update", map[string]any{"sessionId": sessionID, "session": s.sessionListItem(ctx, *lastSession)})
			}
			writeSSEEvent(w, flusher, "done", map[string]string{"sessionId": sessionID})
			return
		}
	}
}

// sessionListed reports whether a session belongs in the session list (the
// same filter as ListSessions: top-level and with at least one message).
func sessionListed(sess session.Session) bool {
	return sess.ParentSessionID == "" && sess.MessageCount > 0
}

// handleSessionEvents pushes session list changes from this process's session
// broker: local sessions and the ones IPC followers (subagents) report, which
// the leader republishes there — including when a follower starts or stops
// running one (busy_elsewhere flips). Events per session are coalesced:
//
//	event: ready           {}
//	event: session_upsert  {"session": <session list item>}
//	event: session_deleted {"id"}
//
// GET /api/v1/sessions/events
func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	if s.app == nil || s.app.Sessions == nil {
		writeError(w, http.StatusServiceUnavailable, "sessions unavailable")
		return
	}
	sseHeaders(w)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ch := s.app.Sessions.Subscribe(ctx)
	writeSSEEvent(w, flusher, "ready", map[string]string{})

	pending := newCoalescer[session.Session]()
	keepAlive := time.NewTicker(sessionEventsKeepAlive)
	defer keepAlive.Stop()
	var window <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Payload.ID == "" {
				continue
			}
			pending.add(ev.Payload.ID, ev)
			if window == nil {
				window = time.After(sessionEventCoalesce)
			}
		case <-window:
			window = nil
			for _, ev := range pending.take() {
				switch {
				case ev.Type == pubsub.DeletedEvent:
					writeSSEEvent(w, flusher, "session_deleted", map[string]string{"id": ev.Payload.ID})
				case sessionListed(ev.Payload):
					writeSSEEvent(w, flusher, "session_upsert", map[string]any{"session": s.sessionListItem(ctx, ev.Payload)})
				}
			}
		case <-keepAlive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
