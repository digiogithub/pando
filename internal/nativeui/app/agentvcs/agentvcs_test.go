package agentvcs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func TestAgentVCSBrowseAndDiff(t *testing.T) {
	mux := http.NewServeMux()
	js := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/api/v1/agentvcs/sessions", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"sessions": []client.VCSSession{{SessionID: "abcdef12345", CommitCount: 2}}})
	})
	mux.HandleFunc("/api/v1/agentvcs/sessions/abcdef12345/log", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"commits": []client.VCSCommit{
			{ID: "c0", ShortID: "c0", IsBaseline: true, Description: "Session start", CreatedAt: time.Now().Format(time.RFC3339)},
			{ID: "c1", ShortID: "c1", ParentID: "c0", Description: "Edit main", ChangedFiles: 1, CreatedAt: time.Now().Format(time.RFC3339)},
		}})
	})
	mux.HandleFunc("/api/v1/agentvcs/commits/c1", func(w http.ResponseWriter, r *http.Request) {
		js(w, client.VCSCommitDetail{Commit: client.VCSCommitInfo{ID: "c1", ShortID: "c1", Type: "delta"},
			Diff: []client.VCSDiffEntry{{Path: "main.go", Type: "modified", OldHash: "h0", NewHash: "h1"}}})
	})
	mux.HandleFunc("/api/v1/agentvcs/blobs/h0", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("a\nb\n")) })
	mux.HandleFunc("/api/v1/agentvcs/blobs/h1", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("a\nc\n")) })
	mux.HandleFunc("/api/v1/snapshots", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"snapshots": []client.Snapshot{{ID: "s1", Name: "before-refactor", SessionID: "abcdef12345", Status: "active", CreatedAt: time.Now().Format(time.RFC3339)}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	a := app.NewContext(context.Background(), c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	v := New(a)
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, nil) }, 1300, 800)
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
	wait(func() bool { return tt.HasText("2 commits") })
	if err := tt.Click("abcdef12…"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return tt.HasText("Edit main") })
	if err := tt.Click("Edit main"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return tt.HasText("main.go") })
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return v.diff != nil && v.diff.state != nil })
	if a, d := v.diff.state.Stats(); a != 1 || d != 1 {
		t.Errorf("stats +%d -%d", a, d)
	}
	v.diff = nil
	v.tab = 1
	wait(func() bool { return tt.HasText("before-refactor") })
}
