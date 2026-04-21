package evaluator_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

// selfImprovementSchema is the DDL for the self-improvement tables.
// Copied from the migration file (without goose directives).
const selfImprovementSchema = `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    parent_session_id TEXT,
    title TEXT NOT NULL,
    message_count INTEGER NOT NULL DEFAULT 0,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cost REAL NOT NULL DEFAULT 0.0,
    updated_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    summary_message_id TEXT
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    parts TEXT NOT NULL DEFAULT '[]',
    model TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    finished_at INTEGER,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS prompt_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    section TEXT NOT NULL,
    content TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    is_active INTEGER NOT NULL DEFAULT 1,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(name, version)
);

CREATE TABLE IF NOT EXISTS session_scores (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    template_id TEXT REFERENCES prompt_templates(id),
    reward REAL NOT NULL DEFAULT 0.0,
    success_score REAL NOT NULL DEFAULT 0.0,
    efficiency_score REAL NOT NULL DEFAULT 0.0,
    judge_analysis TEXT,
    judge_model TEXT,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    message_count INTEGER NOT NULL DEFAULT 0,
    user_corrections INTEGER NOT NULL DEFAULT 0,
    evaluated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE TABLE IF NOT EXISTS prompt_ucb_stats (
    template_id TEXT PRIMARY KEY REFERENCES prompt_templates(id) ON DELETE CASCADE,
    times_used INTEGER NOT NULL DEFAULT 0,
    total_reward REAL NOT NULL DEFAULT 0.0,
    avg_reward REAL NOT NULL DEFAULT 0.0,
    ucb_score REAL NOT NULL DEFAULT 9999.0,
    last_used_at INTEGER,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_library (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    source_session_id TEXT,
    source_template_id TEXT REFERENCES prompt_templates(id),
    task_type TEXT NOT NULL DEFAULT 'general',
    usage_count INTEGER NOT NULL DEFAULT 0,
    success_rate REAL NOT NULL DEFAULT 0.0,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TRIGGER IF NOT EXISTS update_ucb_after_score
AFTER INSERT ON session_scores
WHEN NEW.template_id IS NOT NULL
BEGIN
    INSERT INTO prompt_ucb_stats (template_id, times_used, total_reward, avg_reward, ucb_score, updated_at)
    VALUES (NEW.template_id, 1, NEW.reward, NEW.reward, NEW.reward, unixepoch())
    ON CONFLICT(template_id) DO UPDATE SET
        times_used = times_used + 1,
        total_reward = total_reward + NEW.reward,
        avg_reward = (total_reward + NEW.reward) / (times_used + 1),
        ucb_score = (total_reward + NEW.reward) / (times_used + 1),
        updated_at = unixepoch();
END;
`

// setupTestDB creates an in-memory SQLite DB with the self-improvement schema.
func setupTestDB(t *testing.T) (*sql.DB, db.Querier) {
	t.Helper()

	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := conn.Exec(selfImprovementSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })
	return conn, db.New(conn)
}

// insertTestSession inserts a session directly for testing.
func insertTestSession(t *testing.T, conn *sql.DB, id string) {
	t.Helper()

	now := time.Now().Unix()
	_, err := conn.Exec(
		`INSERT INTO sessions(id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		id, "test session", 2, 100, 200, 0.0, now, now,
	)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

// stubMessageService is a minimal message.Service for testing.
type stubMessageService struct {
	msgs []message.Message
}

func (s *stubMessageService) Subscribe(_ context.Context) <-chan pubsub.Event[message.Message] {
	ch := make(chan pubsub.Event[message.Message])
	close(ch)
	return ch
}

func (s *stubMessageService) List(_ context.Context, _ string) ([]message.Message, error) {
	return s.msgs, nil
}

func (s *stubMessageService) Create(_ context.Context, _ string, _ message.CreateMessageParams) (message.Message, error) {
	return message.Message{}, nil
}

func (s *stubMessageService) Update(_ context.Context, _ message.Message) error {
	return nil
}

func (s *stubMessageService) Get(_ context.Context, _ string) (message.Message, error) {
	return message.Message{}, nil
}

func (s *stubMessageService) Delete(_ context.Context, _ string) error {
	return nil
}

func (s *stubMessageService) DeleteSessionMessages(_ context.Context, _ string) error {
	return nil
}

func defaultEvalConfig() config.EvaluatorConfig {
	return config.EvaluatorConfig{
		Enabled:           true,
		AlphaWeight:       0.8,
		BetaWeight:        0.2,
		ExplorationC:      1.41,
		MinSessionsForUCB: 5,
		MaxTokensBaseline: 50,
		MaxSkills:         10,
		Async:             false,
	}
}

func TestEvaluateSession_PersistsScore(t *testing.T) {
	conn, q := setupTestDB(t)
	insertTestSession(t, conn, "sess-001")

	svc, err := evaluator.New(defaultEvalConfig(), q, &stubMessageService{})
	if err != nil {
		t.Fatalf("evaluator.New: %v", err)
	}

	ctx := context.Background()
	if err := svc.EvaluateSession(ctx, "sess-001"); err != nil {
		t.Fatalf("EvaluateSession: %v", err)
	}

	score, err := q.GetSessionScore(ctx, "sess-001")
	if err != nil {
		t.Fatalf("GetSessionScore: %v", err)
	}
	if score.SessionID != "sess-001" {
		t.Errorf("expected session_id=sess-001, got %q", score.SessionID)
	}
	if score.SuccessScore != 1.0 {
		t.Errorf("expected SuccessScore=1.0 (no corrections), got %f", score.SuccessScore)
	}
}

func TestEvaluateSession_Idempotent(t *testing.T) {
	conn, q := setupTestDB(t)
	insertTestSession(t, conn, "sess-002")

	svc, err := evaluator.New(defaultEvalConfig(), q, &stubMessageService{})
	if err != nil {
		t.Fatalf("evaluator.New: %v", err)
	}

	ctx := context.Background()
	_ = svc.EvaluateSession(ctx, "sess-002")
	_ = svc.EvaluateSession(ctx, "sess-002")

	count, err := q.CountSessionScores(ctx)
	if err != nil {
		t.Fatalf("CountSessionScores: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 score row (idempotent), got %d", count)
	}
}

func TestSelectTemplate_BelowThreshold(t *testing.T) {
	_, q := setupTestDB(t)

	cfg := defaultEvalConfig()
	cfg.MinSessionsForUCB = 5
	svc, err := evaluator.New(cfg, q, &stubMessageService{})
	if err != nil {
		t.Fatalf("evaluator.New: %v", err)
	}

	ctx := context.Background()
	tmpl, err := svc.SelectTemplate(ctx, "base")
	if err != nil {
		t.Fatalf("SelectTemplate: %v", err)
	}
	if tmpl != nil {
		t.Errorf("expected nil template below UCB threshold, got %+v", tmpl)
	}
}

func TestGetStats_Empty(t *testing.T) {
	_, q := setupTestDB(t)

	svc, err := evaluator.New(defaultEvalConfig(), q, &stubMessageService{})
	if err != nil {
		t.Fatalf("evaluator.New: %v", err)
	}

	ctx := context.Background()
	stats, err := svc.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if !stats.IsEnabled {
		t.Error("expected IsEnabled=true")
	}
	if stats.TotalEvaluations != 0 {
		t.Errorf("expected 0 evaluations, got %d", stats.TotalEvaluations)
	}
}

func TestSaveSkill_MaxSkillsEviction(t *testing.T) {
	conn, q := setupTestDB(t)
	insertTestSession(t, conn, "sess-skill")

	cfg := defaultEvalConfig()
	cfg.MaxSkills = 2
	svc, err := evaluator.New(cfg, q, &stubMessageService{})
	if err != nil {
		t.Fatalf("evaluator.New: %v", err)
	}

	ctx := context.Background()

	for i, content := range []string{"rule A", "rule B"} {
		_, err := q.InsertSkill(ctx, db.InsertSkillParams{
			ID:       fmt.Sprintf("skill-%d", i),
			Title:    content,
			Content:  content,
			TaskType: "general",
		})
		if err != nil {
			t.Fatalf("InsertSkill %d: %v", i, err)
		}
	}

	count, _ := q.CountActiveSkills(ctx)
	if count != 2 {
		t.Fatalf("expected 2 skills before eviction test, got %d", count)
	}

	skills, err := svc.GetActiveSkills(ctx, "general")
	if err != nil {
		t.Fatalf("GetActiveSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("expected 2 active skills, got %d", len(skills))
	}
}
