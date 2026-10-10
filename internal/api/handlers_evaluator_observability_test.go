package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/digiogithub/pando/internal/db"
	"github.com/pressly/goose/v3"

	"github.com/digiogithub/pando/internal/db"
)

// openObservabilityDB returns a fully migrated database seeded with two
// evaluated sessions and one unevaluated eligible session.
func openObservabilityDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("pando-sqlite", filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	goose.SetBaseFS(db.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	now := time.Now().Unix()
	old := now - 7200
	for _, id := range []string{"s1", "s2", "s3"} {
		exec(`INSERT INTO sessions(id,title,message_count,prompt_tokens,completion_tokens,cost,updated_at,created_at) VALUES (?,?,0,0,0,0,?,?)`, id, "title "+id, old, old)
		for i, role := range []string{"user", "assistant", "user", "assistant"} {
			exec(`INSERT INTO messages(id,session_id,role,parts,created_at,updated_at) VALUES (?,?,?,'[]',?,?)`, id+string(rune('a'+i)), id, role, old, old)
		}
	}
	comp := `{"components":{"success":0.6,"tokens":0.5},"weights":{"success":0.7,"tokens":0.3},"patternHits":[{"index":2,"pattern":"(?i)wrong","weight":0.4,"afterAssistant":true,"snippet":"that is wrong"}],"feedback":"bad","userTurns":2}`
	exec(`INSERT INTO session_scores(id,session_id,reward,success_score,efficiency_score,message_count,user_corrections,evaluated_at,created_at,components,judge_analysis,judge_model,judge_prompt_tokens,judge_completion_tokens) VALUES ('sc1','s1',0.25,0.6,0.5,4,1,?,?,?,?,'m',100,20)`,
		now, now, comp, `{"reasoning":"missed the test","task_type":"debug","confidence":0.8}`)
	exec(`INSERT INTO session_scores(id,session_id,reward,evaluated_at,created_at) VALUES ('sc2','s2',0.75,?,?)`, now-86400, now-86400)
	exec(`INSERT INTO session_template_selections(session_id,section,variant_id,selected_at) VALUES ('s1','base/identity','base/identity#terse',?)`, now)
	exec(`INSERT INTO skill_library(id,title,content,task_type,is_active,status,created_at,updated_at) VALUES ('sk1','Verify builds','rule','general',1,'approved',?,?)`, now, now)
	exec(`INSERT INTO session_skill_injections(session_id,skill_id,position,injected_at) VALUES ('s1','sk1',0,?)`, now)
	return conn
}

func getJSON(t *testing.T, h http.HandlerFunc, path string, out any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d: %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("%s: decode: %v (%s)", path, err, rec.Body.String())
	}
}

func TestEvaluatorSessionsEndpointExplainsScores(t *testing.T) {
	s := &Server{config: ServerConfig{DB: openObservabilityDB(t)}}
	var resp struct {
		Sessions []EvaluatorSessionResponse `json:"sessions"`
	}
	getJSON(t, s.handleGetEvaluatorSessions, "/api/v1/evaluator/sessions", &resp)
	if len(resp.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(resp.Sessions))
	}
	got := resp.Sessions[0] // newest first
	if got.SessionID != "s1" || got.Title != "title s1" || got.Reward != 0.25 {
		t.Fatalf("unexpected first session: %+v", got)
	}
	if got.Corrections != 1 || len(got.PatternHits) != 1 || got.PatternHits[0].Snippet != "that is wrong" {
		t.Errorf("corrections not surfaced: %+v", got)
	}
	if got.Feedback != "bad" {
		t.Errorf("feedback = %q", got.Feedback)
	}
	if len(got.Variants) != 1 || got.Variants[0].Section != "base/identity" || got.Variants[0].Variant != "terse" {
		t.Errorf("variants = %+v", got.Variants)
	}
	if len(got.Skills) != 1 || got.Skills[0].Title != "Verify builds" {
		t.Errorf("skills = %+v", got.Skills)
	}
	if !strings.Contains(string(got.JudgeAnalysis), "missed the test") || got.JudgeModel != "m" {
		t.Errorf("judge = %s / %s", got.JudgeAnalysis, got.JudgeModel)
	}
	if !strings.Contains(string(got.Components), `"success":0.6`) {
		t.Errorf("components = %s", got.Components)
	}
	if second := resp.Sessions[1]; string(second.JudgeAnalysis) != "null" || len(second.Variants) != 0 || second.PatternHits == nil {
		t.Errorf("legacy row not normalised: %+v", second)
	}
}

func TestEvaluatorMetricsEndpointHasDailySeries(t *testing.T) {
	s := &Server{config: ServerConfig{DB: openObservabilityDB(t)}}
	var m EvaluatorMetrics
	getJSON(t, s.handleGetEvaluatorMetrics, "/api/v1/evaluator/metrics", &m)
	if m.TotalSessions != 2 {
		t.Fatalf("total = %d, want 2", m.TotalSessions)
	}
	if len(m.Daily) != 14 {
		t.Fatalf("daily = %d entries, want 14", len(m.Daily))
	}
	var evals int64
	for _, d := range m.Daily {
		evals += d.Evaluations
	}
	if evals != 2 {
		t.Errorf("daily evaluations sum = %d, want 2", evals)
	}
	if m.JudgeCalls != 1 || m.JudgePromptTokens != 100 || m.JudgeCompletionTokens != 20 {
		t.Errorf("judge usage = %d calls %d/%d tokens", m.JudgeCalls, m.JudgePromptTokens, m.JudgeCompletionTokens)
	}
}

func TestEvaluatorDoctorEndpoint(t *testing.T) {
	s := &Server{config: ServerConfig{DB: openObservabilityDB(t)}}
	var resp struct {
		Report struct {
			EligibleSessions int64 `json:"eligible_sessions"`
			NeverEvaluated   int64 `json:"never_evaluated"`
		} `json:"report"`
		Text string `json:"text"`
	}
	getJSON(t, s.handleGetEvaluatorDoctor, "/api/v1/evaluator/doctor", &resp)
	if resp.Report.EligibleSessions != 3 || resp.Report.NeverEvaluated != 1 {
		t.Errorf("report = %+v, want 3 eligible / 1 never evaluated", resp.Report)
	}
	if !strings.Contains(resp.Text, "Evaluator:") {
		t.Errorf("text report missing: %q", resp.Text)
	}
}
