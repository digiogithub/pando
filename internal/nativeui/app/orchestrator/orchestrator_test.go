package orchestrator

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
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

type fake struct {
	mu        sync.Mutex
	created   map[string]any
	cancelled []string
	deleted   []string
	enabled   map[string]bool
	ran       []string
	cronNew   map[string]any
}

func (f *fake) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/orchestrator/tasks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total":2,"tasks":[
{"id":"t1","name":"Refactor auth","prompt":"Refactor the auth module","agent":"claude","model":"sonnet","status":"running","progress":40,"tokens":1234,"created_at":"2026-10-27T10:00:00Z","current_tool":"edit","tool_calls":[{"id":"c1","name":"edit","title":"Edit file","status":"completed","arguments":{"path":"a.go"},"result":"ok"}]},
{"id":"t2","name":"Write docs","agent":"copilot","model":"gpt-5","status":"completed","progress":100,"tokens":99,"created_at":"2026-10-27T09:00:00Z","output":"all done"}]}`))
	})
	mux.HandleFunc("POST /api/v1/orchestrator/tasks", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		_ = json.NewDecoder(r.Body).Decode(&f.created)
		f.mu.Unlock()
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":"t3","name":"x","status":"pending"}`))
	})
	mux.HandleFunc("POST /api/v1/orchestrator/tasks/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.cancelled = append(f.cancelled, r.PathValue("id"))
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("DELETE /api/v1/orchestrator/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.deleted = append(f.deleted, r.PathValue("id"))
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /api/v1/orchestrator/delegation/metrics", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"warm_attempts":4,"warm_hits":3,"warm_hit_rate":0.75}`))
	})
	mux.HandleFunc("GET /api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"id":"sonnet"},{"id":"gpt-5"}]}`))
	})
	mux.HandleFunc("GET /api/v1/cronjobs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"enabled":true,"jobs":[{"name":"daily","schedule":"0 9 * * *","enabled":true,"prompt":"Summarise","nextRun":"2030-01-01T09:00:00Z"}]}`))
	})
	mux.HandleFunc("POST /api/v1/cronjobs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		_ = json.NewDecoder(r.Body).Decode(&f.cronNew)
		f.mu.Unlock()
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("POST /api/v1/cronjobs/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.ran = append(f.ran, r.PathValue("name"))
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"taskId":"t9"}`))
	})
	return mux
}

func setup(t *testing.T) (*View, *ui.Tester, *app.QueueDispatcher, *fake) {
	t.Helper()
	f := &fake{}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c, err := client.New(client.Options{BaseURL: srv.URL, Token: "x"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	v := New(a)
	tt := ui.NewTester(func(c *ui.Context) {
		kit.Apply(c, a.Theme)
		v.Build(c, nil)
		kit.ToastViewport(c)
	}, 1300, 800)
	return v, tt, q, f
}

func waitFor(t *testing.T, tt *ui.Tester, q *app.QueueDispatcher, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if q.Drain() > 0 {
			tt.Frame()
		}
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out; texts %q", tt.Texts())
}

func TestTasksListDetailCreateCancel(t *testing.T) {
	v, tt, q, f := setup(t)
	waitFor(t, tt, q, func() bool { return tt.HasText("Refactor auth") })
	if !tt.HasText("Write docs") || !tt.HasText("1 running") || !tt.HasText("75%") {
		t.Fatalf("list/metrics missing: %q", tt.Texts())
	}

	// Detail on row click, with tool calls.
	if err := tt.Click("Refactor auth"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return tt.HasText("Task Detail") })
	if !tt.HasText("Refactor the auth module") || !tt.HasText("Edit file") {
		t.Fatalf("detail missing: %q", tt.Texts())
	}
	// Expand the tool call.
	if err := tt.Click("Edit file"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Result") {
		t.Errorf("tool call not expanded: %q", tt.Texts())
	}

	// Cancel the running task.
	if err := tt.Click("Cancel task"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.cancelled) == 1 })

	// Create a task.
	if err := tt.Click("Create Task"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return tt.HasText("Create Orchestrator Task") })
	tt.Type("Fix bug")
	if v.create.name != "Fix bug" {
		t.Fatalf("typing did not reach the name field: %q", v.create.name)
	}
	v.submitCreate(v.app)
	waitFor(t, tt, q, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.created != nil })
	if p, _ := f.created["prompt"].(string); !strings.Contains(p, "Fix bug") {
		t.Errorf("created %v", f.created)
	}
}

func TestCronTab(t *testing.T) {
	v, tt, q, f := setup(t)
	waitFor(t, tt, q, func() bool { return tt.HasText("Refactor auth") })
	if err := tt.Click("CronJobs"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return tt.HasText("daily") })
	if !tt.HasText("0 9 * * *") {
		t.Errorf("schedule missing: %q", tt.Texts())
	}
	if err := tt.Click("Run now"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.ran) == 1 })

	// Create form.
	if err := tt.Click("New CronJob"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	v.cron.form.Name, v.cron.form.Schedule, v.cron.form.Prompt = "nightly", "0 2 * * *", "do it"
	tt.Frame()
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.cronNew != nil })
	if f.cronNew["name"] != "nightly" {
		t.Errorf("created %v", f.cronNew)
	}
}
