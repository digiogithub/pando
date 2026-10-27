package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func TestLogsFilterAndDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/logs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total":3,"logs":[
{"id":"1","timestamp":"2026-10-27T10:00:00Z","level":"info","source":"api","message":"server started"},
{"id":"2","timestamp":"2026-10-27T10:00:01Z","level":"error","source":"llm","message":"provider exploded","details":"code=500 retry=no"},
{"id":"3","timestamp":"2026-10-27T10:00:02Z","level":"debug","source":"db","message":"query ok"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := client.New(client.Options{BaseURL: srv.URL, Token: "x"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	v := New(a)
	tt := ui.NewTester(func(c *ui.Context) { kit.Apply(c, a.Theme); v.Build(c, nil) }, 1100, 700)

	deadline := time.Now().Add(5 * time.Second)
	for !tt.HasText("server started") {
		if time.Now().After(deadline) {
			t.Fatalf("logs never loaded: %q", tt.Texts())
		}
		if q.Drain() > 0 {
			tt.Frame()
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !tt.HasText("provider exploded") || !tt.HasText("query ok") {
		t.Fatalf("rows missing: %q", tt.Texts())
	}
	// Level filter.
	if err := tt.Click("Error"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("server started") || !tt.HasText("provider exploded") {
		t.Errorf("error filter: %q", tt.Texts())
	}
	// Detail.
	if err := tt.Click("provider exploded"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Log Detail") || !tt.HasText("code=500 retry=no") {
		t.Errorf("detail missing: %q", tt.Texts())
	}
	// Text search across levels.
	v.level, v.search = 0, "query"
	tt.Frame()
	if tt.HasText("provider exploded") || !tt.HasText("query ok") {
		t.Errorf("search: %q", tt.Texts())
	}
}
