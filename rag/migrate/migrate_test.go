package migrate

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestUpCreatesSchemaInOwnTable(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)

	if n, err := Pending(ctx, db); err != nil || n == 0 {
		t.Fatalf("Pending on empty db = %d, %v; want > 0", n, err)
	}
	if err := Up(ctx, db); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := Up(ctx, db); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if n, err := Pending(ctx, db); err != nil || n != 0 {
		t.Fatalf("Pending after Up = %d, %v; want 0", n, err)
	}
	for _, table := range []string{"kb_documents", "kb_chunks", "kb_fts", "kb_links", "kb_state", "kb_doc_fields_fts", TableName} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, table).Scan(&name); err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
	var goose int
	_ = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'goose_db_version'`).Scan(&goose)
	if goose != 0 {
		t.Errorf("Up wrote goose_db_version; it must only use %s", TableName)
	}
}

func TestMergeListsBothSetsInOrder(t *testing.T) {
	base := fstest.MapFS{
		"migrations/20250101000000_first.sql": {Data: []byte("-- +goose Up\n")},
		"migrations/20990101000000_last.sql":  {Data: []byte("-- +goose Up\n")},
	}
	merged, err := Merge(base, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.Glob(merged, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "migrations/20250101000000_first.sql" || got[len(got)-1] != "migrations/20990101000000_last.sql" {
		t.Fatalf("unexpected order: %v", got)
	}
	if len(got) < 3 {
		t.Fatalf("rag migrations missing: %v", got)
	}
	if err := fstest.TestFS(merged, "migrations/20250101000000_first.sql", "migrations/20260311000001_add_kb.sql"); err != nil {
		t.Fatal(err)
	}
}

func TestMergeRejectsCollisions(t *testing.T) {
	sameName := fstest.MapFS{"m/20260311000001_add_kb.sql": {Data: []byte("x")}}
	if _, err := Merge(sameName, "m"); err == nil || !strings.Contains(err.Error(), "exists in both") {
		t.Fatalf("same name: err = %v", err)
	}
	sameVersion := fstest.MapFS{"m/20260311000001_other.sql": {Data: []byte("x")}}
	if _, err := Merge(sameVersion, "m"); err == nil || !strings.Contains(err.Error(), "version 20260311000001") {
		t.Fatalf("same version: err = %v", err)
	}
}

func TestMemoryRowsGetMemoryKind(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if err := Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO kb_documents (file_path, content, memory_key) VALUES ('m.md', 'x', 'k'), ('d.md', 'y', '')`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT file_path, kind, status FROM kb_documents ORDER BY file_path`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var p, k, st string
		if err := rows.Scan(&p, &k, &st); err != nil {
			t.Fatal(err)
		}
		got = append(got, p+":"+k+":"+st)
	}
	if strings.Join(got, ",") != "d.md:document:active,m.md:memory:active" {
		t.Fatalf("kinds = %v", got)
	}
	// Deleting a document drops its front-matter fields row.
	if _, err := db.Exec(`INSERT INTO kb_doc_fields_fts(rowid, title, labels, aliases) SELECT id, 't', '', '' FROM kb_documents WHERE file_path = 'd.md'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM kb_documents WHERE file_path = 'd.md'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM kb_doc_fields_fts`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("fields rows after delete = %d, %v", n, err)
	}
}
