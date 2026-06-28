package app

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// TestCompactDatabaseLocalPath exercises the primary-local branch of the
// CompactDatabase funnel: with no IPC client set, it must VACUUM the writer
// connection directly and report a "full" compaction.
func TestCompactDatabaseLocalPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pando.db")
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY, blob TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	// Churn rows then delete most of them to leave free pages for VACUUM to reclaim.
	for i := 0; i < 2000; i++ {
		if _, err := conn.ExecContext(ctx, `INSERT INTO t (blob) VALUES (?)`, fmt.Sprintf("%01024d", i)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM t WHERE id % 10 != 0`); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// App with only the writer connection wired (no IPC) takes the local branch.
	a := &App{rwConn: conn}
	res, err := a.CompactDatabase(ctx, false, true)
	if err != nil {
		t.Fatalf("CompactDatabase: %v", err)
	}
	if res.Mode != "full" {
		t.Fatalf("expected mode full, got %q", res.Mode)
	}
	if res.SizeAfter >= res.SizeBefore {
		t.Fatalf("expected VACUUM to shrink db: before=%d after=%d", res.SizeBefore, res.SizeAfter)
	}
	if res.Freed <= 0 {
		t.Fatalf("expected freed > 0, got %d", res.Freed)
	}

	// auto_vacuum must now be INCREMENTAL (2) so a follow-up incremental works.
	var mode int
	if err := conn.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		t.Fatalf("pragma auto_vacuum: %v", err)
	}
	if mode != 2 {
		t.Fatalf("expected auto_vacuum=2 (incremental), got %d", mode)
	}
	if _, err := a.CompactDatabase(ctx, true, false); err != nil {
		t.Fatalf("incremental CompactDatabase: %v", err)
	}
}

// TestCompactDatabaseNoConnection verifies the funnel errors clearly when neither
// a writer connection nor an IPC client is available.
func TestCompactDatabaseNoConnection(t *testing.T) {
	a := &App{}
	if _, err := a.CompactDatabase(context.Background(), false, true); err == nil {
		t.Fatal("expected error when no writable connection is available")
	}
}
