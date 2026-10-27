package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEvaluatorEndpoints(t *testing.T) {
	var reviewed string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/evaluator/metrics":
			w.Write([]byte(`{"total_sessions":3,"avg_reward":0.5,"daily":[{"day":"2026-10-01","evaluations":2,"avg_reward":0.4}]}`))
		case r.URL.Path == "/api/v1/evaluator/templates":
			w.Write([]byte(`{"sections":[{"section":"intro","variants":[{"id":"a","name":"default","is_default":true,"times_used":2,"ucb_score":1.5}]}]}`))
		case r.URL.Path == "/api/v1/evaluator/skills":
			w.Write([]byte(`{"skills":[{"id":"s1","name":"x","status":"pending"}]}`))
		case r.URL.Path == "/api/v1/evaluator/sessions":
			w.Write([]byte(`{"sessions":[{"id":"1","session_id":"s","reward":0.9,"variants":[{"section":"a","variant":"b"}],"judge_analysis":null}]}`))
		case strings.HasSuffix(r.URL.Path, "/approve"):
			reviewed = r.URL.Path
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	ctx := context.Background()
	m, err := c.EvaluatorMetrics(ctx)
	if err != nil || m.TotalSessions != 3 || len(m.Daily) != 1 {
		t.Fatalf("metrics %+v %v", m, err)
	}
	tpl, _ := c.EvaluatorTemplates(ctx)
	if len(tpl) != 1 || tpl[0].Variants[0].UCBScore != 1.5 {
		t.Fatalf("templates %+v", tpl)
	}
	sk, _ := c.EvaluatorSkills(ctx)
	if len(sk) != 1 || sk[0].Status != "pending" {
		t.Fatalf("skills %+v", sk)
	}
	ss, _ := c.EvaluatorSessions(ctx)
	if len(ss) != 1 || ss[0].Variants[0].Variant != "b" {
		t.Fatalf("sessions %+v", ss)
	}
	if err := c.ReviewSkill(ctx, "s1", "approve"); err != nil || reviewed != "/api/v1/evaluator/skills/s1/approve" {
		t.Fatalf("review %q %v", reviewed, err)
	}
}

func TestInstancesEndpoints(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/instances":
			w.Write([]byte(`{"instances":[{"instance_id":"abcdef123","path":"/p","pid":4,"mode":"tui","is_primary":true}]}`))
		case "/api/v1/instances/i1/sessions":
			if r.URL.Query().Get("limit") != "100" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"sessions":[{"id":"s1","title":"T","message_count":2}],"has_more":true}`))
		case "/api/v1/instances/i1/sessions/s1/messages":
			w.Write([]byte(`{"messages":[{"id":"m","role":"user","content":"hi"}]}`))
		case "/api/v1/instances/i1/sessions/s1/message":
			w.Write([]byte(`{}`))
		case "/api/v1/instances/i1/sessions/s1/cancel":
			if r.Method != http.MethodDelete {
				t.Errorf("method %s", r.Method)
			}
			w.Write([]byte(`{}`))
		case "/api/v1/instances/i1/sessions/s1/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("data: {\"topic\":\"llm.token\",\"payload\":{\"token\":\"a\"}}\n\n: ka\n\ndata: {\"topic\":\"instance.heartbeat\",\"payload\":{}}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	ctx := context.Background()
	in, err := c.Instances(ctx)
	if err != nil || len(in) != 1 || !in[0].IsPrimary {
		t.Fatalf("instances %+v %v", in, err)
	}
	pg, err := c.RemoteSessions(ctx, "i1", 100, 0)
	if err != nil || !pg.HasMore || pg.Total != 1 {
		t.Fatalf("page %+v %v", pg, err)
	}
	ms, _ := c.RemoteMessages(ctx, "i1", "s1")
	if len(ms) != 1 || ms[0].Content != "hi" {
		t.Fatalf("msgs %+v", ms)
	}
	if err := c.SendRemoteMessage(ctx, "i1", "s1", "x"); err != nil {
		t.Fatal(err)
	}
	if err := c.CancelRemote(ctx, "i1", "s1"); err != nil {
		t.Fatal(err)
	}
	var topics []string
	if err := c.FollowRemoteSession(ctx, "i1", "s1", func(e InstanceEvent) { topics = append(topics, e.Topic) }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(topics, ",") != "llm.token,instance.heartbeat" {
		t.Fatalf("topics %v", topics)
	}
}
