package db

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// TestBuildDSN covers the DSN construction used by openPool (and therefore
// Connect / ConnectRWSecondary / ConnectReadOnly): a "file:" URI carrying
// extra query parameters, with paths escaped via net/url, and ":memory:" /
// the empty path passed through unchanged because SQLite's plain
// (non-URI) in-memory filename does not accept query parameters.
func TestBuildDSN(t *testing.T) {
	t.Run("memory and empty paths are returned unchanged", func(t *testing.T) {
		for _, path := range []string{":memory:", ""} {
			got := buildDSN(path, url.Values{"_txlock": {"immediate"}})
			if got != path {
				t.Fatalf("buildDSN(%q) = %q, want %q unchanged (no query string can be attached)", path, got, path)
			}
		}
	})

	t.Run("real paths become a file: URI with the requested query", func(t *testing.T) {
		dsn := buildDSN("/tmp/pando/pando.db", url.Values{"_txlock": {"immediate"}})
		if !strings.HasPrefix(dsn, "file:") {
			t.Fatalf("buildDSN() = %q, want a file: URI", dsn)
		}
		if !strings.Contains(dsn, "_txlock=immediate") {
			t.Fatalf("buildDSN() = %q, want _txlock=immediate in the query", dsn)
		}
	})

	t.Run("spaces and special characters in the path are escaped and the DB actually opens there", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pando data (test) & more.db")
		dsn := buildDSN(path, url.Values{"_txlock": {"immediate"}})

		sqlDB, err := sqlite3driver.Open(dsn)
		if err != nil {
			t.Fatalf("opening the escaped DSN %q failed: %v", dsn, err)
		}
		defer sqlDB.Close()

		if _, err := sqlDB.Exec(`CREATE TABLE t(x)`); err != nil {
			t.Fatalf("create table via escaped-path DSN: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected the sqlite file to exist at the exact requested path %q: %v", path, err)
		}
	})

	// Regression test: config.Get().Data.Directory defaults to the relative
	// path ".pando" (see internal/config/config.go's defaultDataDirectory),
	// so Connect() routinely builds a DSN from a path relative to the
	// process's current directory, not just from absolute ones. A plain
	// (non-URI) relative filename opens fine — SQLite resolves it against
	// the current directory like any other relative file access — but a
	// *relative* "file:" URI path does not: SQLite's URI filename parsing
	// expects the path component to be absolute, and a first version of
	// buildDSN that just wrapped the input path without resolving it broke
	// every relative-path caller with "sqlite3: unable to open database
	// file" (caught by cmd's TestDesignSystemExtractRejectsUnknownSource,
	// which relies on config.Load's default relative data directory).
	t.Run("relative paths resolve against the process cwd and still open", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		dsn := buildDSN(filepath.Join(".pando", "relative.db"), url.Values{"_txlock": {"immediate"}})

		sqlDB, err := sqlite3driver.Open(dsn)
		if err != nil {
			t.Fatalf("opening the relative-path DSN %q failed: %v", dsn, err)
		}
		defer sqlDB.Close()

		if err := os.MkdirAll(filepath.Join(dir, ".pando"), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if _, err := sqlDB.Exec(`CREATE TABLE t(x)`); err != nil {
			t.Fatalf("create table via relative-path DSN: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".pando", "relative.db")); err != nil {
			t.Fatalf("expected the sqlite file to exist under the resolved absolute path: %v", err)
		}
	})
}

// TestOpenPoolAppliesPragmasToEveryConnection is the regression test for the
// bug this fix removes: the old code ran PRAGMA statements via a single
// db.Exec call after sql.Open, which under database/sql's connection pool
// only ever reached whichever one connection happened to be idle at that
// moment — leaving the rest of the pool (7 of 8 connections, in production)
// with the driver's defaults (foreign_keys OFF, the 1-minute default busy
// timeout). openPool instead configures every connection through the
// driver's init callback (driver.Open(dsn, init)), which SQLite invokes once
// per physical connection. This test opens several concurrent connections
// (forcing the pool to create more than one physical connection) and checks
// every one of them independently.
func TestOpenPoolAppliesPragmasToEveryConnection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pool.db")
	sqlDB, err := openPool(dbPath, poolOptions{
		immediateWrites: true,
		busyTimeout:     primaryBusyTimeout,
		perConnPragmas:  "PRAGMA foreign_keys = ON; PRAGMA synchronous = NORMAL; PRAGMA cache_size = -8000;",
		maxOpenConns:    8,
		maxIdleConns:    4,
	})
	if err != nil {
		t.Fatalf("openPool() error = %v", err)
	}
	defer sqlDB.Close()

	const n = 5
	ctx := context.Background()
	conns := make([]*sql.Conn, n)
	for i := range conns {
		c, err := sqlDB.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn(%d) error = %v", i, err)
		}
		conns[i] = c
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	wantBusyMS := int(primaryBusyTimeout / time.Millisecond)

	var wg sync.WaitGroup
	for i, c := range conns {
		wg.Add(1)
		go func(i int, c *sql.Conn) {
			defer wg.Done()

			var fk int
			if err := c.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
				t.Errorf("conn %d: PRAGMA foreign_keys: %v", i, err)
				return
			}
			if fk != 1 {
				t.Errorf("conn %d: foreign_keys = %d, want 1 (ON)", i, fk)
			}

			var busyMS int
			if err := c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyMS); err != nil {
				t.Errorf("conn %d: PRAGMA busy_timeout: %v", i, err)
				return
			}
			if busyMS != wantBusyMS {
				t.Errorf("conn %d: busy_timeout = %d ms, want %d ms", i, busyMS, wantBusyMS)
			}

			var synchronous int
			if err := c.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&synchronous); err != nil {
				t.Errorf("conn %d: PRAGMA synchronous: %v", i, err)
				return
			}
			if synchronous != 1 { // 1 == NORMAL
				t.Errorf("conn %d: synchronous = %d, want 1 (NORMAL)", i, synchronous)
			}
		}(i, c)
	}
	wg.Wait()
}

// TestOpenPoolSecondaryLikePragmas mirrors ConnectRWSecondary's options: a
// single connection with a short (200ms) busy_timeout, so a secondary
// instance still fails fast and falls back to the IPC proxy instead of
// blocking for the primary's full busy_timeout.
func TestOpenPoolSecondaryLikePragmas(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "secondary.db")
	sqlDB, err := openPool(dbPath, poolOptions{
		immediateWrites: true,
		busyTimeout:     secondaryBusyTimeout,
		perConnPragmas:  "PRAGMA synchronous = NORMAL; PRAGMA foreign_keys = ON;",
		maxOpenConns:    1,
	})
	if err != nil {
		t.Fatalf("openPool() error = %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()

	var fk int
	if err := sqlDB.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1 (ON)", fk)
	}

	var busyMS int
	if err := sqlDB.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyMS); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if want := int(secondaryBusyTimeout / time.Millisecond); busyMS != want {
		t.Fatalf("busy_timeout = %d ms, want %d ms", busyMS, want)
	}
}

// TestOpenPoolImmediateWritesUsesBeginImmediate confirms the mechanism the
// whole fix relies on: with immediateWrites (the DSN's "_txlock=immediate"),
// a plain db.BeginTx(ctx, nil) issues BEGIN IMMEDIATE — so it takes the
// write lock up front — while an explicit read-only transaction
// (&sql.TxOptions{ReadOnly: true}) still stays DEFERRED. Both behaviors are
// asserted indirectly: an IMMEDIATE transaction left open blocks a second
// writer's BEGIN IMMEDIATE (via the short busy_timeout below), while a
// read-only transaction never blocks anything and a second read-only
// transaction can be opened concurrently without error.
func TestOpenPoolImmediateWritesUsesBeginImmediate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "immediate.db")
	sqlDB, err := openPool(dbPath, poolOptions{
		immediateWrites: true,
		busyTimeout:     50 * time.Millisecond,
		maxOpenConns:    4,
	})
	if err != nil {
		t.Fatalf("openPool() error = %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE t(x INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// A plain BeginTx takes the write lock immediately, before any
	// statement runs.
	tx1, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx #1: %v", err)
	}
	defer tx1.Rollback() //nolint:errcheck

	// A second writer's plain BeginTx must now fail fast (busy_timeout is
	// only 50ms) instead of silently succeeding as a second DEFERRED
	// transaction would (which only fails, if at all, on its first write).
	tx2, err := sqlDB.BeginTx(ctx, nil)
	if err == nil {
		tx2.Rollback() //nolint:errcheck
		t.Fatal("BeginTx #2 succeeded while #1 held an IMMEDIATE transaction open; want SQLITE_BUSY")
	}

	// A read-only transaction, in contrast, never takes the write lock and
	// must succeed even while tx1 is open.
	roTx, err := sqlDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("read-only BeginTx while a write tx is open: %v", err)
	}
	if err := roTx.Rollback(); err != nil {
		t.Fatalf("rollback read-only tx: %v", err)
	}

	if err := tx1.Rollback(); err != nil {
		t.Fatalf("rollback tx1: %v", err)
	}
}
