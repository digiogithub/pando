package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// tableExists reports whether name is a table in conn's main schema.
func tableExists(t *testing.T, conn *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return n > 0
}

// TestConnectCLIAtSkipsMigrationsOnExistingDB: a CLI must never migrate a
// database that already exists (it may be a different binary than the
// instance owning the schema), and it gets the CLI pool settings: 5 s busy
// timeout and the per-connection pragmas on EVERY pooled connection.
func TestConnectCLIAtSkipsMigrationsOnExistingDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pando.db")

	// An existing database no migration has touched.
	seed, err := openPool(path, poolOptions{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE marker (x INTEGER)`); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	_ = seed.Close()

	conn, err := ConnectCLIAt(path)
	if err != nil {
		t.Fatalf("ConnectCLIAt: %v", err)
	}
	defer conn.Close()

	if tableExists(t, conn, "goose_db_version") {
		t.Fatal("ConnectCLIAt ran the migrations on an existing database")
	}
	if !tableExists(t, conn, "marker") {
		t.Fatal("existing table not visible")
	}

	if got := conn.Stats().MaxOpenConnections; got != cliMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, cliMaxOpenConns)
	}

	// Hold every connection the pool may have, so each one is checked.
	ctx := context.Background()
	held := make([]*sql.Conn, 0, cliMaxOpenConns)
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()
	for i := range cliMaxOpenConns {
		c, err := conn.Conn(ctx)
		if err != nil {
			t.Fatalf("check out connection %d: %v", i, err)
		}
		held = append(held, c)
		var busy, fk int
		if err := c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil {
			t.Fatalf("busy_timeout: %v", err)
		}
		if err := c.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatalf("foreign_keys: %v", err)
		}
		if busy != int(cliBusyTimeout.Milliseconds()) {
			t.Errorf("connection %d busy_timeout = %d ms, want %d", i, busy, cliBusyTimeout.Milliseconds())
		}
		if fk != 1 {
			t.Errorf("connection %d foreign_keys = %d, want 1", i, fk)
		}
	}
}

// TestConnectCLIAtMigratesMissingDB: with no database yet there is no owner to
// defer to, so ConnectCLI falls back to Connect (data dir + migrations).
func TestConnectCLIAtMigratesMissingDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "pando.db")

	conn, err := ConnectCLIAt(path)
	if err != nil {
		t.Fatalf("ConnectCLIAt: %v", err)
	}
	defer conn.Close()

	if !tableExists(t, conn, "goose_db_version") || !tableExists(t, conn, "sessions") {
		t.Fatal("ConnectCLIAt did not migrate a missing database")
	}
}

// TestConnectCLIAtMigratesEmptyFile: an empty file is a database no migration
// has touched (e.g. created by a crashed first start), so it is migrated too.
func TestConnectCLIAtMigratesEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pando.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	conn, err := ConnectCLIAt(path)
	if err != nil {
		t.Fatalf("ConnectCLIAt: %v", err)
	}
	defer conn.Close()

	if !tableExists(t, conn, "goose_db_version") {
		t.Fatal("ConnectCLIAt did not migrate an empty database file")
	}
}
