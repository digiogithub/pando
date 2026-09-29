package evaluator_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/message"
)

func TestFeedback_RescoresInPlaceWithoutDoubleCountingUCB(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "fb1", "", 2, time.Hour)
	if _, err := conn.Exec(`INSERT INTO prompt_templates(id,name,section,content,created_at,updated_at) VALUES ('t1','tpl','s','c',1,1)`); err != nil {
		t.Fatal(err)
	}
	svc := newSvc(t, q, twoTurns(), nil)
	svc.RecordTemplateSelection(context.Background(), "fb1", "t1")
	ctx := context.Background()

	res, err := svc.EvaluateNow(ctx, "fb1", evaluator.EvaluateOptions{Force: true, SkipJudge: true})
	if err != nil || res.Skipped != "" {
		t.Fatalf("initial evaluation: %v %q", err, res.Skipped)
	}
	first := res.Reward.Total
	if first < 0.5 {
		t.Fatalf("clean session should score well, got %f", first)
	}

	res, err = svc.RecordFeedback(ctx, "fb1", "bad", "wrong approach")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reward.Total >= 0.3 {
		t.Errorf("bad feedback total = %f, want < 0.3", res.Reward.Total)
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM session_scores WHERE session_id='fb1'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("expected exactly one score row, got %d (%v)", n, err)
	}
	score, _ := q.GetSessionScore(ctx, "fb1")
	if score.Reward >= 0.3 {
		t.Errorf("persisted reward = %f, want < 0.3", score.Reward)
	}
	var comp struct {
		Feedback     string `json:"feedback"`
		FeedbackNote string `json:"feedbackNote"`
	}
	if err := json.Unmarshal([]byte(score.Components), &comp); err != nil || comp.Feedback != "bad" || comp.FeedbackNote != "wrong approach" {
		t.Errorf("components not persisted correctly: %q (%v)", score.Components, err)
	}

	// UCB stats: still one use, total follows the new reward.
	var times int64
	var total, avg float64
	if err := conn.QueryRow(`SELECT times_used, total_reward, avg_reward FROM prompt_ucb_stats WHERE template_id='t1'`).Scan(&times, &total, &avg); err != nil {
		t.Fatal(err)
	}
	if times != 1 || abs(total-score.Reward) > 1e-9 || abs(avg-score.Reward) > 1e-9 {
		t.Errorf("UCB stats times=%d total=%f avg=%f, want 1 use at reward %f", times, total, avg, score.Reward)
	}

	// Later good feedback supersedes.
	res, err = svc.RecordFeedback(ctx, "fb1", "good", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reward.Total <= 0.8 {
		t.Errorf("good feedback total = %f, want > 0.8", res.Reward.Total)
	}
	if err := conn.QueryRow(`SELECT times_used FROM prompt_ucb_stats WHERE template_id='t1'`).Scan(&times); err != nil || times != 1 {
		t.Errorf("times_used = %d after second re-score, want 1", times)
	}
}

func TestFeedback_ScoresUnscoredSessionAndValidates(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "fb2", "", 1, time.Hour)
	svc := newSvc(t, q, twoTurns()[:2], nil) // single user turn: guard bypassed by feedback
	ctx := context.Background()

	if _, err := svc.RecordFeedback(ctx, "fb2", "meh", ""); err == nil {
		t.Error("invalid rating must be rejected")
	}
	res, err := svc.RecordFeedback(ctx, "fb2", "good", "nice")
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != "" || res.Reward.Total <= 0.8 {
		t.Errorf("unexpected result: skipped=%q total=%f", res.Skipped, res.Reward.Total)
	}
	if !scored(t, q, "fb2") {
		t.Error("feedback must score an unscored session")
	}
}

func TestEvaluate_TokensBaselineFromSessionsTable(t *testing.T) {
	conn, q := setupTestDB(t)
	now := time.Now().Unix()
	// Historical sessions with token usage but no session_scores rows.
	for i, id := range []string{"h1", "h2", "h3"} {
		if _, err := conn.Exec(
			`INSERT INTO sessions(id,title,message_count,prompt_tokens,completion_tokens,updated_at,created_at) VALUES (?,?,?,?,?,?,?)`,
			id, "h", 6, 5000, 5000, now-int64(100+i), now-int64(100+i)); err != nil {
			t.Fatal(err)
		}
	}
	// Current session uses far fewer tokens than the baseline (10000).
	addSession(t, conn, "cur", "", 2, time.Hour)
	if _, err := conn.Exec(`UPDATE sessions SET prompt_tokens=1000, completion_tokens=1000 WHERE id='cur'`); err != nil {
		t.Fatal(err)
	}
	svc := newSvc(t, q, twoTurns(), nil)
	res, err := svc.EvaluateNow(context.Background(), "cur", evaluator.EvaluateOptions{Force: true, SkipJudge: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reward.EfficiencyScore <= 0.5 || res.Reward.Breakdown.Baseline != 10000 {
		t.Errorf("efficiency=%f baseline=%f, want > 0.5 against baseline 10000", res.Reward.EfficiencyScore, res.Reward.Breakdown.Baseline)
	}
}

func TestEvaluate_PersistsComponents(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "c1", "", 2, time.Hour)
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do it"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "1", Name: "bash", Input: "ls"}, message.Finish{Reason: message.FinishReasonToolUse}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "1", Name: "bash", Content: "boom", IsError: true}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "retry"}, message.Finish{Reason: message.FinishReasonEndTurn}}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "that's wrong, undo that"}}},
	}
	svc := newSvc(t, q, msgs, func(c *config.EvaluatorConfig) { c.CorrectionsPatterns = config.DefaultCorrectionsPatterns() })
	if _, err := svc.EvaluateNow(context.Background(), "c1", evaluator.EvaluateOptions{Force: true, SkipJudge: true}); err != nil {
		t.Fatal(err)
	}
	score, _ := q.GetSessionScore(context.Background(), "c1")
	var bd evaluator.Breakdown
	if err := json.Unmarshal([]byte(score.Components), &bd); err != nil {
		t.Fatal(err)
	}
	if len(bd.PatternHits) != 1 || bd.ToolErrors != 1 || bd.Components["toolErrors"] != 0 {
		t.Errorf("unexpected breakdown: %s", score.Components)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
