package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/pressly/goose/v3"
)

// TestMigrationRemovesStaleSeededTemplates upgrades a database that holds the
// seeded prompt_templates rows and their per-template UCB stats (PANDO-US-0072)
// and checks they are gone, the old UCB triggers are dropped and the new
// selection/statistics tables exist.
func TestMigrationRemovesStaleSeededTemplates(t *testing.T) {
	const preMigrationVersion = 20260929000002

	conn, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	goose.SetBaseFS(FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.UpTo(conn, "migrations", preMigrationVersion); err != nil {
		t.Fatalf("goose up to pre-migration: %v", err)
	}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, n := range []string{"base/identity", "base/workflow", "agents/coder", "base/environment"} {
		exec(`INSERT INTO prompt_templates(id,name,section,content,created_at,updated_at) VALUES (?,?,?,?,1,1)`, "id-"+n, n, "base", "snapshot")
	}
	exec(`INSERT INTO prompt_ucb_stats(template_id, times_used, total_reward, avg_reward, ucb_score, updated_at) VALUES ('id-base/identity', 3, 2.0, 0.66, 0.66, 1)`)
	exec(`INSERT INTO skill_library(id,title,content,source_template_id,created_at,updated_at) VALUES ('sk','t','c','id-base/identity',1,1)`)

	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	count := func(q string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM prompt_templates`); n != 0 {
		t.Errorf("prompt_templates rows = %d, want 0", n)
	}
	if n := count(`SELECT COUNT(*) FROM prompt_ucb_stats`); n != 0 {
		t.Errorf("prompt_ucb_stats rows = %d, want 0", n)
	}
	if n := count(`SELECT COUNT(*) FROM skill_library WHERE source_template_id IS NOT NULL`); n != 0 {
		t.Errorf("skill_library still references a template")
	}
	if n := count(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name IN ('update_ucb_after_score','update_ucb_after_rescore')`); n != 0 {
		t.Errorf("old UCB triggers still present")
	}
	if n := count(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('session_template_selections','prompt_variant_stats')`); n != 2 {
		t.Errorf("new variant tables missing")
	}
}
