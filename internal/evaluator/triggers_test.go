package evaluator_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

// twoTurns returns a transcript with two user turns.
func twoTurns() []message.Message {
	return []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "add a flag"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "done"}}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "thanks, now docs"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "done"}}},
	}
}

// addSession inserts a session (and userMsgs user rows in messages) idle for idleAgo.
func addSession(t *testing.T, conn *sql.DB, id, parent string, userMsgs int, idleAgo time.Duration) {
	t.Helper()
	ts := time.Now().Add(-idleAgo).Unix()
	var p any
	if parent != "" {
		p = parent
	}
	if _, err := conn.Exec(
		`INSERT INTO sessions(id, parent_session_id, title, message_count, updated_at, created_at) VALUES (?,?,?,?,?,?)`,
		id, p, "s "+id, userMsgs*2, ts, ts,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	for i := 0; i < userMsgs; i++ {
		if _, err := conn.Exec(
			`INSERT INTO messages(id, session_id, role, created_at, updated_at) VALUES (?,?,?,?,?)`,
			fmt.Sprintf("%s-m%d", id, i), id, "user", ts, ts,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}
}

func scored(t *testing.T, q db.Querier, id string) bool {
	t.Helper()
	_, err := q.GetSessionScore(context.Background(), id)
	return err == nil
}

func newSvc(t *testing.T, q db.Querier, msgs []message.Message, mutate func(*config.EvaluatorConfig)) *evaluator.EvaluatorService {
	t.Helper()
	cfg := defaultEvalConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	svc, err := evaluator.New(cfg, q, &stubMessageService{msgs: msgs})
	if err != nil {
		t.Fatalf("evaluator.New: %v", err)
	}
	return svc
}

func TestMarkCompleted_GuardMinUserTurns(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "one-turn", "", 1, time.Hour)
	svc := newSvc(t, q, twoTurns()[:2], nil) // one user message

	if err := svc.MarkCompleted(context.Background(), "one-turn", "test"); err != nil {
		t.Fatal(err)
	}
	if scored(t, q, "one-turn") {
		t.Fatal("session with a single user turn must not be scored")
	}
}

func TestMarkCompleted_ScoresTwoTurnSession(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "s1", "", 2, time.Hour)
	svc := newSvc(t, q, twoTurns(), nil)

	if err := svc.MarkCompleted(context.Background(), "s1", "test"); err != nil {
		t.Fatal(err)
	}
	if !scored(t, q, "s1") {
		t.Fatal("expected a persisted session_scores row")
	}
}

func TestMarkCompleted_SubagentGuard(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "parent", "", 2, time.Hour)
	addSession(t, conn, "child", "parent", 2, time.Hour)

	svc := newSvc(t, q, twoTurns(), nil)
	_ = svc.MarkCompleted(context.Background(), "child", "test")
	if scored(t, q, "child") {
		t.Fatal("subagent session must be skipped by default")
	}

	svc = newSvc(t, q, twoTurns(), func(c *config.EvaluatorConfig) { c.IncludeSubagents = true })
	_ = svc.MarkCompleted(context.Background(), "child", "test")
	if !scored(t, q, "child") {
		t.Fatal("includeSubagents must allow scoring child sessions")
	}
}

func TestMarkCompleted_IdempotentAndDeduped(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "s1", "", 2, time.Hour)
	svc := newSvc(t, q, twoTurns(), nil)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.MarkCompleted(context.Background(), "s1", "test")
		}()
	}
	wg.Wait()
	if n, _ := q.CountSessionScores(context.Background()); n != 1 {
		t.Fatalf("expected exactly 1 score row, got %d", n)
	}
}

func TestMarkCompleted_AsyncFlush(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "s1", "", 2, time.Hour)
	svc := newSvc(t, q, twoTurns(), func(c *config.EvaluatorConfig) { c.Async = true })

	if err := svc.MarkCompleted(context.Background(), "s1", "test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !scored(t, q, "s1") {
		t.Fatal("Flush must wait for the async evaluation to persist its score")
	}
}

func TestSweep_SelectsOnlyIdleUnscoredEligibleSessions(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "idle", "", 2, 2*time.Hour)
	addSession(t, conn, "recent", "", 2, time.Minute)
	addSession(t, conn, "short", "", 1, 2*time.Hour)
	addSession(t, conn, "child", "idle", 2, 2*time.Hour)
	addSession(t, conn, "done", "", 2, 2*time.Hour)
	svc := newSvc(t, q, twoTurns(), nil)
	if err := svc.EvaluateSession(context.Background(), "done"); err != nil {
		t.Fatal(err)
	}

	n, err := svc.Sweep(context.Background(), evaluator.SweepOptions{IdleFor: 30 * time.Minute, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || !scored(t, q, "idle") {
		t.Fatalf("expected only 'idle' to be scored, evaluated=%d", n)
	}
	for _, id := range []string{"recent", "short", "child"} {
		if scored(t, q, id) {
			t.Errorf("%s must not be scored by the sweep", id)
		}
	}
}

func TestSweep_OldestFirstAndBounded(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "c-newest", "", 2, 2*time.Hour)
	addSession(t, conn, "a-oldest", "", 2, 5*time.Hour)
	addSession(t, conn, "b-middle", "", 2, 3*time.Hour)
	svc := newSvc(t, q, twoTurns(), nil)

	var order []string
	n, err := svc.Sweep(context.Background(), evaluator.SweepOptions{
		IdleFor:  30 * time.Minute,
		Limit:    2,
		OnResult: func(r *evaluator.Result) { order = append(order, r.SessionID) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(order) != 2 || order[0] != "a-oldest" || order[1] != "b-middle" {
		t.Fatalf("expected oldest-first, bounded to 2; got n=%d order=%v", n, order)
	}
	if scored(t, q, "c-newest") {
		t.Error("limit must leave the newest session unscored")
	}
}

type fakeJudgeProvider struct{ calls atomic.Int32 }

func (f *fakeJudgeProvider) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	f.calls.Add(1)
	return &provider.ProviderResponse{Content: `{"reasoning":"ok","key_points":[],"new_skill":"","task_type":"general","confidence":0.1}`}, nil
}

func (f *fakeJudgeProvider) StreamResponse(context.Context, []message.Message, []tools.BaseTool) <-chan provider.ProviderEvent {
	return nil
}

func (f *fakeJudgeProvider) Model() models.Model { return models.Model{} }

func TestSweep_JudgeOffWhenSkipJudge(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "s1", "", 2, time.Hour)
	addSession(t, conn, "s2", "", 2, time.Hour)
	svc := newSvc(t, q, twoTurns(), nil)
	fake := &fakeJudgeProvider{}
	evaluator.SetJudgeProvider(svc, fake)

	if _, err := svc.Sweep(context.Background(), evaluator.SweepOptions{IdleFor: time.Minute, Limit: 1, SkipJudge: true}); err != nil {
		t.Fatal(err)
	}
	if got := fake.calls.Load(); got != 0 {
		t.Fatalf("judge must not run when SkipJudge is set, got %d calls", got)
	}
	// Control: with the judge enabled the fake is called.
	if _, err := svc.Sweep(context.Background(), evaluator.SweepOptions{IdleFor: time.Minute, Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("expected the judge to run once, got %d calls", got)
	}
}

func TestEvaluateNow_ForceBypassesGuards(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "one-turn", "", 1, time.Hour)
	svc := newSvc(t, q, twoTurns()[:2], nil)

	res, err := svc.EvaluateNow(context.Background(), "one-turn", evaluator.EvaluateOptions{})
	if err != nil || res.Skipped == "" {
		t.Fatalf("guarded evaluation must skip, res=%+v err=%v", res, err)
	}
	res, err = svc.EvaluateNow(context.Background(), "one-turn", evaluator.EvaluateOptions{Force: true})
	if err != nil || res.Skipped != "" || !scored(t, q, "one-turn") {
		t.Fatalf("forced evaluation must persist a score, res=%+v err=%v", res, err)
	}
}

func TestContextTrimmer_OptInOnly(t *testing.T) {
	_, q := setupTestDB(t)
	svc := newSvc(t, q, twoTurns(), nil)
	fake := &fakeJudgeProvider{}
	evaluator.SetJudgeProvider(svc, fake)

	// Default config: evaluator enabled, trimmer flag off => no trimmer, no LLM calls.
	if ct := svc.NewContextTrimmer(); ct != nil {
		t.Fatal("trimmer must not be created unless evaluator.contextTrimmer.enabled")
	}
	if got := fake.calls.Load(); got != 0 {
		t.Fatalf("expected zero trimmer LLM calls by default, got %d", got)
	}

	// Flag on => trimmer exists and invokes the provider.
	on := newSvc(t, q, twoTurns(), func(c *config.EvaluatorConfig) { c.ContextTrimmer.Enabled = true })
	evaluator.SetJudgeProvider(on, fake)
	ct := on.NewContextTrimmer()
	if ct == nil {
		t.Fatal("trimmer must be created when the flag is on")
	}
	if _, err := ct.ProfileTask(context.Background(), "fix the bug", nil); err != nil {
		t.Fatal(err)
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("expected one trimmer LLM call, got %d", got)
	}
	if ct.MinConfidence() != 0.7 {
		t.Fatalf("default min confidence = %v", ct.MinConfidence())
	}
}
