package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/digiogithub/pando/internal/logging"
)

const (
	ptyWriteWait  = 10 * time.Second
	ptyPongWait   = 60 * time.Second
	ptyPingPeriod = (ptyPongWait * 9) / 10
	// ptyMaxClientMessage bounds a single client frame (keystrokes / paste).
	ptyMaxClientMessage = 1 * 1024 * 1024
)

// ptyControlMessage is the JSON sent over text frames. Raw terminal I/O travels
// as binary frames in both directions; text frames carry only control.
type ptyControlMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	// Ready payload.
	SessionID string `json:"session_id,omitempty"`
	Shell     string `json:"shell,omitempty"`
	Dir       string `json:"dir,omitempty"`
	Reattach  bool   `json:"reattach,omitempty"`
	// Exit payload.
	Code int `json:"code,omitempty"`
	// Error payload.
	Message string `json:"message,omitempty"`
}

var ptyUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     checkPtyOrigin,
}

// checkPtyOrigin gates websocket handshakes by Origin. WebSockets are not
// subject to CORS, so this is the only origin-level control on the endpoint.
//
// It is defence in depth rather than the primary boundary: authMiddleware
// already requires the token, and the token lives in localStorage rather than a
// cookie, so a hostile page has no ambient credential to replay. Loopback
// origins are allowed so the Vite dev server (:5173 against the API on :8080)
// keeps working.
func checkPtyOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser client (CLI, tests): no ambient credentials to abuse.
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	return isLoopbackHost(parsed.Hostname())
}

func atoiDefault(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

// handleTerminalPTY upgrades to a websocket and wires it to a shell running in a
// real PTY, the same way the TUI terminal does. This is the interactive path:
// vim/htop/ssh, colors, job control and Ctrl+C all work.
//
// Security: there is deliberately no dangerous-command filter here, matching the
// TUI. A PTY receives keystrokes, not commands, so such a filter is trivially
// bypassed (aliases, scripts, shell-out from an editor) and would only give false
// assurance. The boundary is the auth token plus the default localhost bind —
// exposing `pando serve` on a public interface exposes an interactive shell.
func (s *Server) handleTerminalPTY(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	cols := atoiDefault(query.Get("cols"), 80)
	rows := atoiDefault(query.Get("rows"), 24)

	session, reattached, err := s.resolvePTYSession(query.Get("session_id"), cols, rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	conn, err := ptyUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote the error response.
		logging.Error("terminal.pty: upgrade failed", "error", err)
		return
	}

	replay, sub, unsubscribe := session.subscribe()
	defer unsubscribe()

	go s.pumpPTYToClient(conn, session, replay, sub, reattached)
	s.pumpClientToPTY(conn, session)
}

// resolvePTYSession reattaches to a live shell when the client supplies a known
// session id (page reload), otherwise starts a new one.
func (s *Server) resolvePTYSession(sessionID string, cols, rows int) (*ptySession, bool, error) {
	if sessionID != "" {
		if session, ok := ptySessions.get(sessionID); ok {
			_ = session.resize(cols, rows)
			return session, true, nil
		}
	}
	session, err := newPTYSession(s.config.CWD, cols, rows)
	if err != nil {
		return nil, false, err
	}
	ptySessions.add(session)
	return session, false, nil
}

// pumpPTYToClient owns every write to the websocket: gorilla allows only one
// concurrent writer, so ping, control and data frames are all funnelled here.
func (s *Server) pumpPTYToClient(
	conn *websocket.Conn,
	session *ptySession,
	replay []byte,
	sub chan []byte,
	reattached bool,
) {
	ticker := time.NewTicker(ptyPingPeriod)
	defer ticker.Stop()
	defer conn.Close()

	writeControl := func(msg ptyControlMessage) error {
		payload, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		_ = conn.SetWriteDeadline(time.Now().Add(ptyWriteWait))
		return conn.WriteMessage(websocket.TextMessage, payload)
	}

	if err := writeControl(ptyControlMessage{
		Type:      "ready",
		SessionID: session.id,
		Shell:     session.shell,
		Dir:       session.dir,
		Reattach:  reattached,
	}); err != nil {
		return
	}

	if len(replay) > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(ptyWriteWait))
		if err := conn.WriteMessage(websocket.BinaryMessage, replay); err != nil {
			return
		}
	}

	for {
		select {
		case chunk, ok := <-sub:
			if !ok {
				session.mu.Lock()
				code := session.exitCode
				session.mu.Unlock()
				_ = writeControl(ptyControlMessage{Type: "exit", Code: code})
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(ptyWriteWait))
			if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(ptyWriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// pumpClientToPTY reads keystrokes (binary) and control frames (text) until the
// client disconnects. It runs on the handler goroutine, so returning from it
// tears the connection down.
func (s *Server) pumpClientToPTY(conn *websocket.Conn, session *ptySession) {
	conn.SetReadLimit(ptyMaxClientMessage)
	_ = conn.SetReadDeadline(time.Now().Add(ptyPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(ptyPongWait))
	})

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(ptyPongWait))

		switch messageType {
		case websocket.BinaryMessage:
			if err := session.write(data); err != nil {
				return
			}
		case websocket.TextMessage:
			var msg ptyControlMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "resize":
				_ = session.resize(msg.Cols, msg.Rows)
			case "close":
				ptySessions.remove(session.id)
				return
			default:
				// Unknown control types are ignored so the protocol can grow
				// without breaking older clients.
			}
		}
	}
}
