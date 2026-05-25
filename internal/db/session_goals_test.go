package db_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/db"
)

const sessionGoalsSchema = `
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

CREATE TABLE IF NOT EXISTS session_goals (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    objective TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    iteration INTEGER NOT NULL DEFAULT 0,
    max_iterations INTEGER NOT NULL DEFAULT 20,
    max_duration_seconds INTEGER NOT NULL DEFAULT 3600,
    started_at INTEGER NOT NULL,
    completed_at INTEGER,
    last_progress TEXT,
    next_step TEXT,
    blocked_reason TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_session_goals_session_id ON session_goals(session_id);
`

func setupSessionGoalsDB(t *testing.T) (*sql.DB, db.Querier) {
	t.Helper()

	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := conn.Exec(sessionGoalsSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO sessions(id, title, updated_at, created_at) VALUES ('session-1', 'test', 1, 1)`); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })
	return conn, db.New(conn)
}

func TestSessionGoalCreateAndRetrieve(t *testing.T) {
	_, q := setupSessionGoalsDB(t)
	ctx := context.Background()

	created, err := q.CreateGoal(ctx, db.CreateGoalParams{
		ID:                 "goal-1",
		SessionID:          "session-1",
		Objective:          "Add goal mode foundation",
		Status:             "running",
		MaxIterations:      3,
		MaxDurationSeconds: 600,
		StartedAt:          10,
	})
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}

	got, err := q.GetActiveGoal(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetActiveGoal: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("expected goal ID %q, got %q", created.ID, got.ID)
	}
	if got.Objective != "Add goal mode foundation" {
		t.Fatalf("unexpected objective: %q", got.Objective)
	}
}

func TestSessionGoalStatusTransitions(t *testing.T) {
	_, q := setupSessionGoalsDB(t)
	ctx := context.Background()

	_, err := q.CreateGoal(ctx, db.CreateGoalParams{
		ID:                 "goal-2",
		SessionID:          "session-1",
		Objective:          "Finish phase 1",
		Status:             "running",
		MaxIterations:      2,
		MaxDurationSeconds: 300,
		StartedAt:          20,
	})
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}

	if _, err := q.UpdateGoalStatus(ctx, db.UpdateGoalStatusParams{
		Status:        "completed",
		Iteration:     1,
		LastProgress:  sql.NullString{String: "Goal finished", Valid: true},
		NextStep:      sql.NullString{},
		BlockedReason: sql.NullString{},
		CompletedAt:   sql.NullInt64{Int64: 30, Valid: true},
		ID:            "goal-2",
	}); err != nil {
		t.Fatalf("UpdateGoalStatus: %v", err)
	}

	goal, err := q.GetGoalBySession(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetGoalBySession: %v", err)
	}
	if goal.Status != "completed" {
		t.Fatalf("expected completed status, got %q", goal.Status)
	}
	if !goal.CompletedAt.Valid || goal.CompletedAt.Int64 != 30 {
		t.Fatalf("unexpected completed_at: %+v", goal.CompletedAt)
	}
}
