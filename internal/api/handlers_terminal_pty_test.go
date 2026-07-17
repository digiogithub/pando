package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newPTYTestServer spins up the real websocket handler. handleTerminalPTY only
// needs config.CWD, so the Server can be built directly without an app/DB.
func newPTYTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	srv := &Server{config: ServerConfig{CWD: t.TempDir()}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/terminal/pty", srv.handleTerminalPTY)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")
	return httpSrv, wsURL
}

func dialPTY(t *testing.T, wsURL, query string) *websocket.Conn {
	t.Helper()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/terminal/pty?"+query, nil)
	if err != nil {
		t.Fatalf("dial pty websocket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	return conn
}

// readReady consumes the first control frame, which carries the session id.
func readReady(t *testing.T, conn *websocket.Conn) ptyControlMessage {
	t.Helper()

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read ready frame: %v", err)
		}
		if msgType != websocket.TextMessage {
			continue
		}
		var msg ptyControlMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode control frame %q: %v", data, err)
		}
		if msg.Type == "ready" {
			return msg
		}
	}
}

// readUntil accumulates binary output until it contains want, or the deadline hits.
func readUntil(t *testing.T, conn *websocket.Conn, want string) string {
	t.Helper()

	var sb strings.Builder
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q, got error: %v (output so far: %q)", want, err, sb.String())
		}
		if msgType != websocket.BinaryMessage {
			continue
		}
		sb.Write(data)
		if strings.Contains(sb.String(), want) {
			return sb.String()
		}
	}
	t.Fatalf("timed out waiting for %q (output so far: %q)", want, sb.String())
	return ""
}

func TestPTYRunsInteractiveShell(t *testing.T) {
	_, wsURL := newPTYTestServer(t)
	conn := dialPTY(t, wsURL, "cols=80&rows=24")

	ready := readReady(t, conn)
	if ready.SessionID == "" {
		t.Fatal("ready frame carried no session_id")
	}
	if ready.Reattach {
		t.Fatal("a fresh connection must not report a reattach")
	}
	t.Cleanup(func() { ptySessions.remove(ready.SessionID) })

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("echo pty-works\n")); err != nil {
		t.Fatalf("write keystrokes: %v", err)
	}
	readUntil(t, conn, "pty-works")
}

// TestPTYReattachReplaysOutput is the page-reload path: a second connection with
// the same session id must reach the same live shell and repaint its scrollback.
func TestPTYReattachReplaysOutput(t *testing.T) {
	_, wsURL := newPTYTestServer(t)

	first := dialPTY(t, wsURL, "cols=80&rows=24")
	ready := readReady(t, first)
	t.Cleanup(func() { ptySessions.remove(ready.SessionID) })

	if err := first.WriteMessage(websocket.BinaryMessage, []byte("echo marker-before-reload\n")); err != nil {
		t.Fatalf("write keystrokes: %v", err)
	}
	readUntil(t, first, "marker-before-reload")
	first.Close()

	second := dialPTY(t, wsURL, "cols=80&rows=24&session_id="+ready.SessionID)
	reattached := readReady(t, second)
	if !reattached.Reattach {
		t.Fatal("second connection with a known session_id must report reattach")
	}
	if reattached.SessionID != ready.SessionID {
		t.Fatalf("reattached to session %q, want %q", reattached.SessionID, ready.SessionID)
	}
	// The replay buffer must repaint what happened before the reload.
	readUntil(t, second, "marker-before-reload")

	// And the shell must still be alive and accept new input.
	if err := second.WriteMessage(websocket.BinaryMessage, []byte("echo marker-after-reload\n")); err != nil {
		t.Fatalf("write keystrokes after reattach: %v", err)
	}
	readUntil(t, second, "marker-after-reload")
}

func TestPTYUnknownSessionStartsFreshShell(t *testing.T) {
	_, wsURL := newPTYTestServer(t)
	conn := dialPTY(t, wsURL, "cols=80&rows=24&session_id=does-not-exist")

	ready := readReady(t, conn)
	t.Cleanup(func() { ptySessions.remove(ready.SessionID) })

	if ready.Reattach {
		t.Fatal("an unknown session_id must start a fresh shell, not claim a reattach")
	}
	if ready.SessionID == "does-not-exist" || ready.SessionID == "" {
		t.Fatalf("expected a newly generated session id, got %q", ready.SessionID)
	}
}

// TestPTYResizeIsAppliedToShell checks the resize control frame reaches the PTY:
// the shell's own $COLUMNS reflects the new width.
func TestPTYResizeIsAppliedToShell(t *testing.T) {
	_, wsURL := newPTYTestServer(t)
	conn := dialPTY(t, wsURL, "cols=80&rows=24")
	ready := readReady(t, conn)
	t.Cleanup(func() { ptySessions.remove(ready.SessionID) })

	session, ok := ptySessions.get(ready.SessionID)
	if !ok {
		t.Fatal("session missing from registry")
	}
	if err := session.resize(132, 43); err != nil {
		t.Fatalf("resize: %v", err)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("tput cols\n")); err != nil {
		t.Fatalf("write keystrokes: %v", err)
	}
	readUntil(t, conn, "132")
}

func TestCheckPtyOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{name: "no origin (non-browser client)", origin: "", host: "localhost:8080", want: true},
		{name: "same origin", origin: "http://localhost:8080", host: "localhost:8080", want: true},
		{name: "same host different scheme", origin: "https://localhost:8080", host: "localhost:8080", want: true},
		// The Vite dev server runs on another port against the same API.
		{name: "loopback dev server", origin: "http://localhost:5173", host: "localhost:8080", want: true},
		{name: "loopback ip dev server", origin: "http://127.0.0.1:5173", host: "0.0.0.0:8080", want: true},
		{name: "hostile cross origin", origin: "http://evil.example", host: "localhost:8080", want: false},
		{name: "hostile cross origin over https", origin: "https://evil.example", host: "pando.internal:8080", want: false},
		// A hostname merely containing "localhost" must not pass.
		{name: "lookalike host", origin: "http://localhost.evil.example", host: "localhost:8080", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/terminal/pty", nil)
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if got := checkPtyOrigin(r); got != tt.want {
				t.Fatalf("checkPtyOrigin(origin=%q, host=%q) = %v, want %v", tt.origin, tt.host, got, tt.want)
			}
		})
	}
}

func TestAppendReplayIsBounded(t *testing.T) {
	replay := []byte("head")
	replay = appendReplay(replay, make([]byte, ptyReplayBytes))
	if len(replay) != ptyReplayBytes {
		t.Fatalf("replay buffer grew to %d, want it capped at %d", len(replay), ptyReplayBytes)
	}

	replay = appendReplay(replay, []byte("tail"))
	if len(replay) != ptyReplayBytes {
		t.Fatalf("replay buffer grew to %d, want it capped at %d", len(replay), ptyReplayBytes)
	}
	if !strings.HasSuffix(string(replay), "tail") {
		t.Fatal("replay buffer must keep the most recent bytes")
	}
}
