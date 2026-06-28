package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/db"
)

// TestCompactFreesSpace verifies that a full VACUUM reclaims pages left behind
// by churn (insert + delete), shrinking the logical database size.
func TestCompactFreesSpace(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		t.Fatalf("wal: %v", err)
	}
	if _, err := conn.Exec(`CREATE TABLE blob_data (id INTEGER PRIMARY KEY, payload TEXT);`); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Generate churn: insert a lot of rows, then delete most of them. The freed
	// pages stay in the freelist until VACUUM reclaims them.
	big := make([]byte, 2048)
	for i := range big {
		big[i] = 'x'
	}
	for i := 0; i < 5000; i++ {
		if _, err := conn.Exec("INSERT INTO blob_data(payload) VALUES (?)", string(big)); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if _, err := conn.Exec("DELETE FROM blob_data WHERE id % 10 != 0"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	ctx := context.Background()
	res, err := db.Compact(ctx, conn, db.CompactOptions{EnableAutoVacuum: true})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if res.Mode != "full" {
		t.Fatalf("expected mode=full, got %q", res.Mode)
	}
	if res.SizeAfter >= res.SizeBefore {
		t.Fatalf("expected size to shrink: before=%d after=%d", res.SizeBefore, res.SizeAfter)
	}
	if res.Freed <= 0 {
		t.Fatalf("expected freed > 0, got %d", res.Freed)
	}

	// After a full VACUUM with EnableAutoVacuum, auto_vacuum should be INCREMENTAL (2).
	var av int
	if err := conn.QueryRow("PRAGMA auto_vacuum;").Scan(&av); err != nil {
		t.Fatalf("read auto_vacuum: %v", err)
	}
	if av != 2 {
		t.Fatalf("expected auto_vacuum=2 (INCREMENTAL), got %d", av)
	}

	// A follow-up incremental compaction must succeed (no-op is fine).
	if _, err := db.Compact(ctx, conn, db.CompactOptions{Incremental: true}); err != nil {
		t.Fatalf("incremental compact: %v", err)
	}
}
