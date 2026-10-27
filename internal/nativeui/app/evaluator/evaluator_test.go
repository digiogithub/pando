package evaluator

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
)

func fakeServer(t *testing.T, reviewed *string) *client.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/evaluator/metrics":
			w.Write([]byte(`{"total_sessions":7,"total_templates":2,"avg_reward":0.61,"judge_calls":3,"daily":[
			{"day":"2026-10-25","evaluations":2,"avg_reward":0.5},{"day":"2026-10-26","evaluations":5,"avg_reward":0.7},{"day":"2026-10-27","evaluations":0,"avg_reward":0}]}`))
		case r.URL.Path == "/api/v1/evaluator/templates":
			w.Write([]byte(`{"sections":[{"section":"intro","variants":[{"id":"1","name":"default","is_default":true,"times_used":4,"avg_reward":0.5,"ucb_score":1.2},{"id":"2","name":"terse","path":"x.tpl","times_used":9,"avg_reward":0.8,"ucb_score":1.9}]}]}`))
		case r.URL.Path == "/api/v1/evaluator/skills":
			w.Write([]byte(`{"skills":[{"id":"s1","name":"use-ripgrep","description":"prefer rg","status":"pending","uses":0}]}`))
		case r.URL.Path == "/api/v1/evaluator/sessions":
			w.Write([]byte(`{"sessions":[{"id":"e1","session_id":"sess1","title":"Fix the bug","reward":0.82,"message_count":12,"corrections":1,"evaluated_at":1790000000,"components":{"components":{"success":0.9},"weights":{"success":0.5}},"variants":[],"skills":[],"pattern_hits":[]}]}`))
		case r.URL.Path == "/api/v1/evaluator/doctor":
			w.Write([]byte(`{"problem":"nothing evaluated recently","text":"report body","report":{"enabled":true}}`))
		case strings.HasSuffix(r.URL.Path, "/approve"):
			*reviewed = r.URL.Path
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, _ := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	return c
}

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

func TestEvaluatorView(t *testing.T) {
	var reviewed string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, fakeServer(t, &reviewed))
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	ev := New(a)
	tt := ui.NewTester(func(c *ui.Context) { ev.Build(c, nil) }, 1300, 1000)

	waitFor(t, tt, q, func() bool { return tt.HasText("Sessions Evaluated") })
	for _, s := range []string{"7", "0.61", "nothing evaluated recently", "Prompt Variants — UCB Ranking", "terse", "use-ripgrep", "Evaluations per day"} {
		if !tt.HasText(s) && !strings.Contains(strings.Join(tt.Texts(), "\n"), s) {
			t.Errorf("missing %q in %q", s, tt.Texts())
		}
	}
	// UCB order: terse (1.9) before default (1.2).
	joined := strings.Join(tt.Texts(), "\n")
	if strings.Index(joined, "terse") > strings.Index(joined, "default") {
		t.Errorf("variants not sorted by UCB score")
	}
	if err := tt.Click("Approve"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return reviewed == "/api/v1/evaluator/skills/s1/approve" })

	// Sessions tab.
	if err := tt.Click("Sessions (1)"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return tt.HasText("Fix the bug") })
	if err := tt.Click("Fix the bug"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return strings.Contains(strings.Join(tt.Texts(), "\n"), "success 0.90 x 0.50") })
}
