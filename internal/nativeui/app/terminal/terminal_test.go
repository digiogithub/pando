package terminal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
	"github.com/gorilla/websocket"
)

// The view shows the server's PTY output through libghostty-vt and sends
// what is typed back as binary frames.
func TestTerminalOverPTYWebsocket(t *testing.T) {
	up := websocket.Upgrader{}
	typed := make(chan string, 8)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/terminal/pty", func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"ready","session_id":"s1"}`))
		_ = ws.WriteMessage(websocket.BinaryMessage, []byte("hello \x1b[31mred\x1b[0m world\r\n$ "))
		for {
			typ, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if typ == websocket.BinaryMessage {
				typed <- string(data)
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	a := app.NewContext(context.Background(), c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	v := New(a)
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, nil) }, 900, 500)
	tt.Frame()
	if v.libErr != "" {
		t.Skipf("libghostty-vt unavailable: %s", v.libErr)
	}
	defer v.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		q.Drain()
		tt.Frame()
		if len(v.tabs) == 1 && strings.Contains(v.tabs[0].term.Text(), "hello red world") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := v.tabs[0].term.Text(); !strings.Contains(got, "hello red world") {
		t.Fatalf("screen %q", got)
	}
	v.tabs[0].term.Send([]byte("ls\r"))
	select {
	case s := <-typed:
		if s != "ls\r" {
			t.Errorf("typed %q", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("input not forwarded")
	}
}
