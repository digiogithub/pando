package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/pressly/goose/v3"
)

// TestMigrationReviewableSkills upgrades a database holding an unreviewed,
// active skill and checks it is deactivated as legacy, that the queries match
// the migrated schema, and that the migration rolls back.
func TestMigrationReviewableSkills(t *testing.T) {
	conn, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	goose.SetBaseFS(FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, "migrations", 20260929000003); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO skill_library(id,title,content,usage_count,success_rate,created_at,updated_at) VALUES ('old','code skill','Keep diffs small.',40,0.0,1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, "migrations", 20260929000004); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	q := New(conn)
	old, err := q.GetSkill(ctx, "old")
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != "legacy" || old.IsActive != 0 || old.UsageCount != 0 {
		t.Fatalf("legacy row not neutralised: %+v", old)
	}
	if active, err := q.ListAllActiveSkills(ctx); err != nil || len(active) != 0 {
		t.Fatalf("active skills = %v (%v)", active, err)
	}

	if err := q.UpsertSkillMirror(ctx, UpsertSkillMirrorParams{ID: "a", Title: "A", Content: "rule a", TaskType: "general", IsActive: 1, Status: "approved", Confidence: 0.9, CreatedAt: 5}); err != nil {
		t.Fatal(err)
	}
	if got, err := q.ListActiveSkillsByType(ctx, ListActiveSkillsByTypeParams{TaskType: "code", Limit: 10}); err != nil || len(got) != 1 || got[0].Status != "approved" {
		t.Fatalf("ListActiveSkillsByType = %v (%v)", got, err)
	}
	if err := q.InsertSessionSkillInjection(ctx, InsertSessionSkillInjectionParams{SessionID: "s", SkillID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := q.ApplySessionRewardToSkillStats(ctx, ApplySessionRewardToSkillStatsParams{Reward: 0.5, Reward2: 0.5, SessionID: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := q.ApplySessionRewardToSkillStats(ctx, ApplySessionRewardToSkillStatsParams{Reward: 1, Reward2: 1, SessionID: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := q.ApplyRewardDeltaToSkillStats(ctx, ApplyRewardDeltaToSkillStatsParams{Delta: -0.5, Delta2: -0.5, SessionID: "s"}); err != nil {
		t.Fatal(err)
	}
	a, _ := q.GetSkill(ctx, "a")
	if a.EvalCount != 2 || a.SuccessRate != 0.5 { // (0.5 + 1 - 0.5) / 2
		t.Fatalf("stats = %+v", a)
	}
	if inj, err := q.ListSessionInjectedSkills(ctx, "s"); err != nil || len(inj) != 1 {
		t.Fatalf("injected = %v (%v)", inj, err)
	}
	if n, _ := q.CountSessionSkillInjections(ctx, "s"); n != 1 {
		t.Fatalf("count = %d", n)
	}

	if err := goose.DownTo(conn, "migrations", 20260929000003); err != nil {
		t.Fatalf("down: %v", err)
	}
}
