package editor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

type fakeFS struct {
	mu    sync.Mutex
	files map[string]string
}

func (f *fakeFS) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{
			{"name": "src", "path": "src", "isDir": true},
			{"name": "main.go", "path": "main.go", "size": 10},
		}})
	})
	mux.HandleFunc("/api/v1/terminal/exec", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"stdout": "feature-x\n"})
	})
	mux.HandleFunc("/api/v1/files/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/v1/files/")
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == http.MethodPut {
			var b struct{ Content string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			f.files[p] = b.Content
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "ok"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"path": p, "content": f.files[p]})
	})
	return mux
}

func TestEditorOpenEditSave(t *testing.T) {
	fs := &fakeFS{files: map[string]string{"main.go": "package main\n"}}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	c, _ := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	a := app.NewContext(context.Background(), c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	ed := New(a)
	tt := ui.NewTester(func(c *ui.Context) { ed.Build(c, nil) }, 1200, 800)
	wait := func(cond func() bool) {
		t.Helper()
		for i := 0; i < 200; i++ {
			q.Drain()
			tt.Frame()
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("timeout")
	}
	wait(func() bool { return tt.HasText("main.go") })
	wait(func() bool { return ed.gitBranch == "feature-x" })
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { f := ed.activeFile(); return f != nil && f.buf != nil })
	f := ed.activeFile()
	if f == nil || f.dirty() {
		t.Fatal("file not open or dirty")
	}
	f.buf.Set("package main\n// edited\n")
	tt.Frame()
	if !f.dirty() {
		t.Fatal("not dirty after edit")
	}
	ed.save(f)
	wait(func() bool { return !f.dirty() })
	fs.mu.Lock()
	got := fs.files["main.go"]
	fs.mu.Unlock()
	if got != "package main\n// edited\n" {
		t.Errorf("saved %q", got)
	}
	ed.closeFile("main.go")
	if ed.activeFile() != nil {
		t.Error("tab not closed")
	}
}
