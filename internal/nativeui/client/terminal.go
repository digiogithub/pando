package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// PtyReady is the "ready" control frame of the PTY websocket.
type PtyReady struct {
	SessionID string `json:"session_id"`
	Shell     string `json:"shell"`
	Dir       string `json:"dir"`
	Reattach  bool   `json:"reattach"`
}

// PtyHandlers receive what the server sends. They are called from the read
// goroutine, in order.
type PtyHandlers struct {
	OnData  func([]byte)
	OnReady func(PtyReady)
	OnExit  func(code int)
	// OnClose is called once when the socket ends without Detach/Terminate
	// having been called; err is nil on a clean close.
	OnClose func(err error)
}

// PtyConn is a connection to GET /api/v1/terminal/pty: raw terminal I/O in
// binary frames, JSON control in text frames (ready, exit, resize, close).
type PtyConn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	ours    bool
	done    chan struct{}
}

// ConnectPty opens a shell (sessionID "") or reattaches to a running one.
func (c *Client) ConnectPty(ctx context.Context, sessionID string, cols, rows int, h PtyHandlers) (*PtyConn, error) {
	tok, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.URL("/api/v1/terminal/pty"))
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	q := url.Values{}
	q.Set("cols", strconv.Itoa(cols))
	q.Set("rows", strconv.Itoa(rows))
	if sessionID != "" {
		q.Set("session_id", sessionID)
	}
	q.Set("token", tok)
	u.RawQuery = q.Encode()

	d := websocket.Dialer{HandshakeTimeout: 15 * time.Second, Proxy: http.ProxyFromEnvironment}
	if tr, ok := c.http.Transport.(*http.Transport); ok && tr.TLSClientConfig != nil {
		d.TLSClientConfig = tr.TLSClientConfig.Clone()
	} else if u.Scheme == "wss" {
		d.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	hdr := http.Header{}
	hdr.Set(HeaderClient, ClientName)
	ws, resp, err := d.DialContext(ctx, u.String(), hdr)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
			return nil, apiError(resp.StatusCode, nil)
		}
		return nil, err
	}
	p := &PtyConn{ws: ws, done: make(chan struct{})}
	go p.readLoop(h)
	return p, nil
}

func (p *PtyConn) readLoop(h PtyHandlers) {
	defer close(p.done)
	var rerr error
	for {
		typ, data, err := p.ws.ReadMessage()
		if err != nil {
			rerr = err
			break
		}
		if typ == websocket.BinaryMessage {
			if h.OnData != nil {
				h.OnData(data)
			}
			continue
		}
		var msg struct {
			Type string `json:"type"`
			PtyReady
			Code int `json:"code"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "ready":
			if h.OnReady != nil {
				h.OnReady(msg.PtyReady)
			}
		case "exit":
			if h.OnExit != nil {
				h.OnExit(msg.Code)
			}
		}
	}
	p.mu.Lock()
	ours := p.ours
	p.mu.Unlock()
	if ours || h.OnClose == nil {
		return
	}
	if websocket.IsCloseError(rerr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		rerr = nil
	}
	h.OnClose(rerr)
}

func (p *PtyConn) write(typ int, b []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = p.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.ws.WriteMessage(typ, b)
}

// Write sends keystrokes / pasted text to the shell.
func (p *PtyConn) Write(b []byte) (int, error) {
	if err := p.write(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

// Resize tells the server the terminal size.
func (p *PtyConn) Resize(cols, rows int) error {
	b, _ := json.Marshal(map[string]any{"type": "resize", "cols": cols, "rows": rows})
	return p.write(websocket.TextMessage, b)
}

// Detach closes the socket and leaves the shell running so it can be
// reattached with its session id.
func (p *PtyConn) Detach() error {
	p.mu.Lock()
	p.ours = true
	p.mu.Unlock()
	return p.ws.Close()
}

// Terminate kills the shell server-side and closes the socket.
func (p *PtyConn) Terminate() error {
	b, _ := json.Marshal(map[string]any{"type": "close"})
	err := p.write(websocket.TextMessage, b)
	if derr := p.Detach(); err == nil {
		err = derr
	}
	return err
}

// Done is closed when the read loop ended.
func (p *PtyConn) Done() <-chan struct{} { return p.done }
