package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestFilesAndVCS(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/files", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("hidden") != "1" || r.URL.Query().Get("path") != "src" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []FileEntry{{Name: ".a", Path: "src/.a", Size: 1}}})
	})
	mux.HandleFunc("/api/v1/files/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/a b/c.go" {
			t.Errorf("path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": "x"})
	})
	mux.HandleFunc("/api/v1/agentvcs/blobs/h1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"a":1}`)) // valid JSON must stay text
	})
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"token":"t"}`)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Token: "t"})
	hidden := true
	fs, err := c.ListFiles(context.Background(), "src", &hidden)
	if err != nil || len(fs) != 1 || fs[0].Name != ".a" {
		t.Fatalf("%v %v", fs, err)
	}
	if s, err := c.ReadFile(context.Background(), "a b/c.go"); err != nil || s != "x" {
		t.Fatalf("%q %v", s, err)
	}
	if s, err := c.VCSBlob(context.Background(), "h1"); err != nil || s != `{"a":1}` {
		t.Fatalf("%q %v", s, err)
	}
}

func TestPtyConn(t *testing.T) {
	up := websocket.Upgrader{}
	got := make(chan string, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/terminal/pty", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "tok" || r.URL.Query().Get("cols") != "100" {
			http.Error(w, "bad", 400)
			return
		}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"ready","session_id":"s1","shell":"sh"}`))
		_ = ws.WriteMessage(websocket.BinaryMessage, []byte("hi"))
		for {
			typ, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if typ == websocket.BinaryMessage {
				got <- "in:" + string(data)
				_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"exit","code":3}`))
			} else {
				got <- "ctl:" + string(data)
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Token: "tok"})
	data := make(chan string, 4)
	ready := make(chan PtyReady, 1)
	exit := make(chan int, 1)
	pc, err := c.ConnectPty(context.Background(), "", 100, 30, PtyHandlers{
		OnData:  func(b []byte) { data <- string(b) },
		OnReady: func(r PtyReady) { ready <- r },
		OnExit:  func(c int) { exit <- c },
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := <-ready; r.SessionID != "s1" {
		t.Errorf("ready %+v", r)
	}
	if d := <-data; d != "hi" {
		t.Errorf("data %q", d)
	}
	_ = pc.Resize(90, 20)
	if m := <-got; !strings.Contains(m, `"resize"`) {
		t.Errorf("ctl %s", m)
	}
	_, _ = pc.Write([]byte("ls\n"))
	if m := <-got; m != "in:ls\n" {
		t.Errorf("in %q", m)
	}
	select {
	case code := <-exit:
		if code != 3 {
			t.Errorf("exit %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no exit")
	}
	_ = pc.Detach()
	<-pc.Done()
}
