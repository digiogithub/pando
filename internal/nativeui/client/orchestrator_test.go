package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOrchestratorAndLogsEndpoints(t *testing.T) {
	var gotQuery string
	var created map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orchestrator/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"t2","name":"x","status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"tasks":[{"id":"t1","name":"a","status":"running","progress":40,"tokens":12}],"total":1}`))
	})
	mux.HandleFunc("/api/v1/logs", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"logs":[{"id":"1","level":"warn","source":"s","message":"m"}],"total":1}`))
	})
	mux.HandleFunc("/api/v1/cronjobs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jobs":[{"name":"j","schedule":"* * * * *","enabled":true}],"enabled":true}`))
	})
	mux.HandleFunc("/api/v1/cronjobs/j/run", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"taskId":"t9"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := New(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tasks, err := c.ListOrchestratorTasks(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].Progress != 40 {
		t.Fatalf("tasks %v %v", tasks, err)
	}
	if _, err := c.CreateOrchestratorTask(ctx, "do it", "m", ""); err != nil || created["prompt"] != "do it" || created["model"] != "m" {
		t.Fatalf("create %v %v", created, err)
	}
	p, err := c.ListLogs(ctx, LogQuery{Level: "warn", Search: "a b"})
	if err != nil || len(p.Logs) != 1 || gotQuery != "level=warn&limit=200&search=a+b" {
		t.Fatalf("logs %v %v %q", p, err, gotQuery)
	}
	jobs, en, err := c.ListCronJobs(ctx)
	if err != nil || !en || len(jobs) != 1 {
		t.Fatal(jobs, en, err)
	}
	if id, err := c.RunCronJob(ctx, "j"); err != nil || id != "t9" {
		t.Fatal(id, err)
	}
}
