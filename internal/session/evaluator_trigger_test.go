package session_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/pressly/goose/v3"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

// Session-service level integration test: real migrations, real session and
// message services and the real evaluator; only the LLM turns are inserted
// directly (no agent/provider drive).
func newMigratedDB(t *testing.T) (*sql.DB, db.Querier) {
	t.Helper()
	conn, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	goose.SetBaseFS(db.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return conn, db.New(conn)
}

func addTurns(t *testing.T, msgs message.Service, sessionID string, userTurns int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < userTurns; i++ {
		if _, err := msgs.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "do the thing"}},
		}); err != nil {
			t.Fatalf("create user message: %v", err)
		}
		if _, err := msgs.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: "done"}},
		}); err != nil {
			t.Fatalf("create assistant message: %v", err)
		}
	}
}

func TestMarkCompletedPersistsScore(t *testing.T) {
	conn, q := newMigratedDB(t)
	msgs := message.NewService(q)
	sessions := session.NewService(q)

	cfg := config.EvaluatorWithDefaults(config.EvaluatorConfig{Enabled: true})
	cfg.Async = false
	svc, err := evaluator.New(cfg, q, msgs)
	if err != nil {
		t.Fatal(err)
	}
	session.SetEvaluator(svc)
	t.Cleanup(func() { session.SetEvaluator(nil) })

	ctx := context.Background()
	full, err := sessions.Create(ctx, "two turns")
	if err != nil {
		t.Fatal(err)
	}
	addTurns(t, msgs, full.ID, 2)
	short, err := sessions.Create(ctx, "one turn")
	if err != nil {
		t.Fatal(err)
	}
	addTurns(t, msgs, short.ID, 1)

	if err := session.MarkCompleted(ctx, short.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetSessionScore(ctx, short.ID); err == nil {
		t.Fatal("one-turn session must not be scored")
	}

	if err := session.MarkCompleted(ctx, full.ID, "test"); err != nil {
		t.Fatal(err)
	}
	score, err := q.GetSessionScore(ctx, full.ID)
	if err != nil {
		t.Fatalf("expected a persisted session_scores row: %v", err)
	}
	if score.MessageCount < 4 {
		t.Errorf("expected the score to cover the transcript, message_count=%d", score.MessageCount)
	}

	// Same trigger again stays at exactly one row.
	if err := session.MarkCompleted(ctx, full.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if n, _ := q.CountSessionScores(ctx); n != 1 {
		t.Fatalf("expected exactly one score row, got %d", n)
	}

	// The idle sweep query works against the real schema: backdate a third
	// session and let the sweeper pick it up.
	idle, err := sessions.Create(ctx, "idle")
	if err != nil {
		t.Fatal(err)
	}
	addTurns(t, msgs, idle.ID, 2)
	old := time.Now().Add(-2 * time.Hour).Unix()
	// The updated_at trigger would reset the backdated value; drop it for the test.
	if _, err := conn.Exec(`DROP TRIGGER update_sessions_updated_at`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, old, idle.ID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Sweep(ctx, evaluator.SweepOptions{IdleFor: 30 * time.Minute, Limit: 10, SkipJudge: true})
	if err != nil || n != 1 {
		t.Fatalf("sweep: evaluated=%d err=%v", n, err)
	}
	if _, err := q.GetSessionScore(ctx, idle.ID); err != nil {
		t.Fatalf("idle session not scored by sweep: %v", err)
	}
}
