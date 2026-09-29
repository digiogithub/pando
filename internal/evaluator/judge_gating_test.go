package evaluator_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/skills"
)

// capturingJudge records every prompt it receives.
type capturingJudge struct {
	mu      sync.Mutex
	prompts []string
}

func (c *capturingJudge) SendMessages(_ context.Context, msgs []message.Message, _ []tools.BaseTool) (*provider.ProviderResponse, error) {
	c.mu.Lock()
	c.prompts = append(c.prompts, msgs[0].Content().Text)
	c.mu.Unlock()
	return &provider.ProviderResponse{
		Content: `{"reasoning":"solid work","key_points":["a","b"],"new_skill":"","task_type":"code","confidence":0.4}`,
		Usage:   provider.TokenUsage{InputTokens: 900, OutputTokens: 100},
	}, nil
}

func (c *capturingJudge) StreamResponse(context.Context, []message.Message, []tools.BaseTool) <-chan provider.ProviderEvent {
	return nil
}
func (c *capturingJudge) Model() models.Model { return models.Model{ID: "fake-judge"} }
func (c *capturingJudge) calls() int          { c.mu.Lock(); defer c.mu.Unlock(); return len(c.prompts) }

func setJudge(s *evaluator.EvaluatorService, p provider.Provider) { evaluator.SetJudgeProvider(s, p) }
func evalOpts() evaluator.EvaluateOptions                         { return evaluator.EvaluateOptions{} }

func turns(n int) []message.Message {
	var out []message.Message
	for i := 0; i < n; i++ {
		out = append(out,
			message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("request %d", i)}}},
			message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("answer %d", i)}}},
		)
	}
	return out
}

// judgeAlways makes every session decisive (reward >= high) so only turns and
// budget gate the judge.
func judgeAlways(c *config.EvaluatorConfig) {
	c.Judge = config.JudgeConfig{HighReward: 0.01, LowReward: 0.0001, MinTurns: 4, MaxTranscriptTokens: 6000}
}

func TestJudge_MinTurnsGate(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "short", "", 3, time.Hour)
	addSession(t, conn, "long", "", 6, time.Hour)
	fake := &capturingJudge{}

	short := newSvc(t, q, turns(3), judgeAlways)
	setJudge(short, fake)
	if _, err := short.EvaluateNow(context.Background(), "short", evalOpts()); err != nil {
		t.Fatal(err)
	}
	if fake.calls() != 0 {
		t.Fatalf("3-turn session must not call the judge, got %d", fake.calls())
	}

	long := newSvc(t, q, turns(6), judgeAlways)
	setJudge(long, fake)
	res, err := long.EvaluateNow(context.Background(), "long", evalOpts())
	if err != nil || !res.Judged || fake.calls() != 1 {
		t.Fatalf("6-turn decisive session must call the judge once: res=%+v err=%v calls=%d", res, err, fake.calls())
	}

	// Judge output is persisted on the score row.
	row, err := q.GetSessionScore(context.Background(), "long")
	if err != nil {
		t.Fatal(err)
	}
	if !row.JudgeModel.Valid || row.JudgeModel.String != "fake-judge" {
		t.Errorf("judge_model = %+v", row.JudgeModel)
	}
	if !row.JudgeAnalysis.Valid || !strings.Contains(row.JudgeAnalysis.String, "solid work") {
		t.Errorf("judge_analysis = %+v", row.JudgeAnalysis)
	}
	if row.JudgePromptTokens != 900 || row.JudgeCompletionTokens != 100 {
		t.Errorf("judge tokens = %d/%d", row.JudgePromptTokens, row.JudgeCompletionTokens)
	}
}

func TestJudge_MidBandSkipped(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "s", "", 6, time.Hour)
	fake := &capturingJudge{}
	// Bands that leave every reward in the dead zone.
	svc := newSvc(t, q, turns(6), func(c *config.EvaluatorConfig) {
		c.Judge = config.JudgeConfig{HighReward: 2, LowReward: -1, MinTurns: 4}
	})
	setJudge(svc, fake)
	if _, err := svc.EvaluateNow(context.Background(), "s", evalOpts()); err != nil {
		t.Fatal(err)
	}
	if fake.calls() != 0 {
		t.Fatalf("non-decisive reward must skip the judge, got %d calls", fake.calls())
	}
}

func TestJudge_DailyBudgetPersistsAcrossRestart(t *testing.T) {
	conn, q := setupTestDB(t)
	for _, id := range []string{"a", "b", "c"} {
		addSession(t, conn, id, "", 6, time.Hour)
	}
	fake := &capturingJudge{}
	mutate := func(c *config.EvaluatorConfig) {
		judgeAlways(c)
		c.Judge.DailyCalls = 1
	}
	first := newSvc(t, q, turns(6), mutate)
	setJudge(first, fake)
	if _, err := first.EvaluateNow(context.Background(), "a", evalOpts()); err != nil {
		t.Fatal(err)
	}
	if _, err := first.EvaluateNow(context.Background(), "b", evalOpts()); err != nil {
		t.Fatal(err)
	}
	if fake.calls() != 1 {
		t.Fatalf("second evaluation must be over budget, judge calls = %d", fake.calls())
	}
	if !scored(t, q, "b") {
		t.Fatal("over-budget session must still be scored")
	}

	// A fresh service (process restart) reads the spent budget from the DB.
	second := newSvc(t, q, turns(6), mutate)
	setJudge(second, fake)
	if _, err := second.EvaluateNow(context.Background(), "c", evalOpts()); err != nil {
		t.Fatal(err)
	}
	if fake.calls() != 1 {
		t.Fatalf("budget must survive a restart, judge calls = %d", fake.calls())
	}
}

func TestJudge_DailyTokenBudget(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "a", "", 6, time.Hour)
	addSession(t, conn, "b", "", 6, time.Hour)
	fake := &capturingJudge{}
	svc := newSvc(t, q, turns(6), func(c *config.EvaluatorConfig) {
		judgeAlways(c)
		c.Judge.DailyTokens = 1000 // one call spends 1000 (900+100)
	})
	setJudge(svc, fake)
	for _, id := range []string{"a", "b"} {
		if _, err := svc.EvaluateNow(context.Background(), id, evalOpts()); err != nil {
			t.Fatal(err)
		}
	}
	if fake.calls() != 1 {
		t.Fatalf("token budget must stop the second call, got %d", fake.calls())
	}
}

func TestJudge_TranscriptCappedWithMarker(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "big", "", 100, time.Hour)
	fake := &capturingJudge{}
	// Long messages so the transcript clearly exceeds the cap.
	msgs := turns(100)
	for i := range msgs {
		msgs[i].Parts = []message.ContentPart{message.TextContent{Text: strings.Repeat("word ", 60) + fmt.Sprint(i)}}
	}
	svc := newSvc(t, q, msgs, func(c *config.EvaluatorConfig) {
		judgeAlways(c)
		c.Judge.MaxTranscriptTokens = 1500
	})
	setJudge(svc, fake)
	if _, err := svc.EvaluateNow(context.Background(), "big", evalOpts()); err != nil {
		t.Fatal(err)
	}
	if fake.calls() != 1 {
		t.Fatalf("expected one judge call, got %d", fake.calls())
	}
	prompt := fake.prompts[0]
	if got := skills.EstimateTokens(prompt); got > 1500 {
		t.Errorf("prompt is %d tokens, cap is 1500", got)
	}
	if !strings.Contains(prompt, "messages elided ...]") {
		t.Error("expected elision marker in the capped transcript")
	}
	if !strings.Contains(prompt, "request") && !strings.Contains(prompt, "word") {
		t.Error("expected head content kept")
	}
	if !strings.Contains(prompt, "99") {
		t.Error("expected tail content kept")
	}
}

func TestLastJudgeError(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "s", "", 6, time.Hour)
	svc := newSvc(t, q, turns(6), judgeAlways)
	if svc.LastJudgeError() != nil {
		t.Fatal("no error expected initially")
	}
	setJudge(svc, &failingJudge{})
	if _, err := svc.EvaluateNow(context.Background(), "s", evalOpts()); err != nil {
		t.Fatal(err)
	}
	e := svc.LastJudgeError()
	if e == nil || !strings.Contains(e.Message, "boom") || e.At.IsZero() {
		t.Fatalf("LastJudgeError = %+v", e)
	}
}

type failingJudge struct{ capturingJudge }

func (f *failingJudge) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	return nil, fmt.Errorf("boom")
}
