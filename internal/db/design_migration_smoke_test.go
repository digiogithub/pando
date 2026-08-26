package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/pressly/goose/v3"
)

// TestMigrationsIncludeDesignSchema runs the real migration chain and checks the
// design tables land, so a broken design migration fails here instead of at a
// user's first start.
func TestMigrationsIncludeDesignSchema(t *testing.T) {
	conn, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	goose.SetBaseFS(FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	for _, table := range []string{"design_artifacts", "design_versions", "design_nodes", "design_critiques"} {
		var name string
		err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing after migrations: %v", table, err)
		}
	}
}
