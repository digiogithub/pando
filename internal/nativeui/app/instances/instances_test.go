package instances

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func waitFor(t *testing.T, tt *ui.Tester, q *app.QueueDispatcher, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		q.Drain()
		tt.Frame()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met; texts: %q", tt.Texts())
}

var sentMsg, cancelledRun atomic.Value

func newSeeded(t *testing.T) (*Instances, *ui.Tester, *app.QueueDispatcher, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/instances":
			w.Write([]byte(`{"instances":[{"instance_id":"abcdef1234","path":"/work/alpha","pid":42,"mode":"tui","is_primary":true},{"instance_id":"zzzz9999","path":"/work/alpha","pid":43,"mode":"webui"}]}`))
		case "/api/v1/instances/abcdef1234/sessions":
			w.Write([]byte(`{"sessions":[{"id":"s1","title":"Refactor parser","message_count":3,"updated_at":"2026-10-27T10:00:00Z"}],"total":1}`))
		case "/api/v1/instances/abcdef1234/sessions/s1/messages":
			w.Write([]byte(`{"messages":[{"id":"m1","role":"user","content":"please refactor"},{"id":"m2","role":"assistant","content":"done refactoring"}]}`))
		case "/api/v1/instances/abcdef1234/sessions/s1/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fl := w.(http.Flusher)
			w.Write([]byte("data: {\"topic\":\"instance.heartbeat\",\"payload\":{}}\n\n"))
			w.Write([]byte("data: {\"topic\":\"llm.token\",\"payload\":{\"token\":\"streamed-token\"}}\n\n"))
			fl.Flush()
			<-r.Context().Done()
		case "/api/v1/instances/abcdef1234/sessions/s1/message":
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			sentMsg.Store(b["content"])
			w.Write([]byte(`{}`))
		case "/api/v1/instances/abcdef1234/sessions/s1/cancel":
			cancelledRun.Store(true)
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	c, _ := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	ctx, cancel := context.WithCancel(context.Background())
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	in := New(a)
	tt := ui.NewTester(func(c *ui.Context) { in.Build(c, nil) }, 1300, 800)
	return in, tt, q, func() { in.Close(); cancel(); srv.Close() }
}

func TestInstancesView(t *testing.T) {
	in, tt, q, closeFn := newSeeded(t)
	defer closeFn()
	_ = in
	waitFor(t, tt, q, func() bool { return tt.HasText("PRIMARY") })
	if err := tt.Click("PID 42"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return tt.HasText("Refactor parser") })
	if err := tt.Click("Refactor parser"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool {
		j := strings.Join(tt.Texts(), "\n")
		return strings.Contains(j, "done refactoring") && strings.Contains(j, "streamed-token") && tt.HasText("live")
	})
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "instance.heartbeat") {
		t.Error("heartbeat noise shown")
	}
	if err := tt.Click("Send Message"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return tt.HasText("Send") })
	tt.Type("hello remote")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, q, func() bool { return sentMsg.Load() == "hello remote" })
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return cancelledRun.Load() == true })
}
