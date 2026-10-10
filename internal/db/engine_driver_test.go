package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	multiwriter "github.com/digiogithub/go-sqlite-multiwriter"
)

func TestEngineIsWriteStatement(t *testing.T) {
	cases := []struct {
		q     string
		write bool
	}{
		{"SELECT 1", false},
		{"  select * from t", false},
		{"VALUES (1)", false},
		{"EXPLAIN QUERY PLAN SELECT 1", false},
		{"(SELECT 1)", false},
		{"WITH x AS (SELECT 1) SELECT * FROM x", false},
		{"WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x", true},
		{"with x as (select 1) update t set a = 1", true},
		{"WITH x AS (SELECT 1) DELETE FROM t WHERE a IN x", true},
		{"WITH x AS (SELECT 1) REPLACE INTO t SELECT * FROM x", true},
		// Identifiers that merely contain a keyword are not writes.
		{"WITH updates AS (SELECT inserted_at FROM t) SELECT * FROM updates", false},
		{"-- comment\nSELECT 1", false},
		{"/* c */ SELECT 1", false},
		{"-- a\n/* b */\n  -- c\nINSERT INTO t VALUES (1)", true},
		// No leading keyword at all: not clearly a read, so a write (safe default).
		{"-- only a comment", true},
		{"/* unterminated", true},
		{"INSERT INTO t VALUES (1)", true},
		{"insert into t values (1) returning id", true},
		{"UPDATE t SET a = 1", true},
		{"DELETE FROM t", true},
		{"REPLACE INTO t VALUES (1)", true},
		{"CREATE TABLE t (a)", true},
		{"DROP TABLE t", true},
		{"ALTER TABLE t ADD COLUMN b", true},
		{"VACUUM", true},
		{"PRAGMA user_version", false},
		{"PRAGMA foreign_keys = ON", false},
		{"BEGIN", false},
		{"BEGIN IMMEDIATE", false},
		{"COMMIT", false},
		{"END", false},
		{"ROLLBACK", false},
		{"SAVEPOINT s", false},
		{"RELEASE s", false},
	}
	for _, c := range cases {
		if got := isWriteStatement(c.q); got != c.write {
			t.Errorf("isWriteStatement(%q) = %v, want %v", c.q, got, c.write)
		}
	}
	// An empty query is not clearly a read: classified as a write (safe default).
	if !isWriteStatement("") {
		t.Errorf("isWriteStatement(\"\") = false, want true (unknown = write)")
	}
}

func TestEngineNormalizeArgs(t *testing.T) {
	ts := time.Date(2026, 10, 10, 12, 34, 56, 789000000, time.FixedZone("x", 2*3600))
	in := []driver.NamedValue{
		{Ordinal: 1, Value: int64(7)},
		{Ordinal: 2, Value: ts},
		{Ordinal: 3, Value: "s"},
	}
	out := normalizeArgs(in)
	if got := out[1].Value; got != "2026-10-10T12:34:56.789+02:00" {
		t.Fatalf("time bound as %#v", got)
	}
	if out[0].Value != int64(7) || out[2].Value != "s" || out[1].Ordinal != 2 {
		t.Fatalf("other args changed: %#v", out)
	}
	if _, ok := in[1].Value.(time.Time); !ok {
		t.Fatalf("normalizeArgs mutated the caller's slice")
	}
	plain := []driver.NamedValue{{Ordinal: 1, Value: "a"}}
	if got := normalizeArgs(plain); &got[0] != &plain[0] {
		t.Fatalf("args without time.Time should be returned as is")
	}

	// End to end: a time.Time argument is stored as RFC 3339 text and read back.
	conn := openEngineT(t, filepath.Join(t.TempDir(), "t.db"))
	mustExecT(t, conn, "CREATE TABLE tt (v)")
	utc := time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC)
	mustExecT(t, conn, "INSERT INTO tt VALUES (?)", utc)
	var typ, txt string
	if err := conn.QueryRow("SELECT typeof(v), CAST(v AS TEXT) FROM tt").Scan(&typ, &txt); err != nil {
		t.Fatal(err)
	}
	if typ != "text" || txt != "2026-01-02T03:04:05.000006Z" {
		t.Fatalf("stored %s %q", typ, txt)
	}
}

func TestEngineIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Fatal("nil is retryable")
	}
	if IsRetryable(errors.New("boom")) {
		t.Fatal("generic error is retryable")
	}
	if !IsRetryable(errors.New("sqlite: database is locked (5) (SQLITE_BUSY)")) {
		t.Fatal("stringly busy error not retryable")
	}

	dir := t.TempDir()

	// Stock modernc SQLITE_BUSY: one connection holds the write lock, another
	// with busy_timeout(0) tries to write.
	stockPath := filepath.Join(dir, "stock.db")
	a, err := sql.Open("sqlite", stockPath+"?_pragma=busy_timeout(0)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sql.Open("sqlite", stockPath+"?_pragma=busy_timeout(0)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	mustExecT(t, a, "CREATE TABLE x (v)")
	ctx := context.Background()
	ca, err := a.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ca.Close()
	if _, err := ca.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	_, busyErr := b.Exec("INSERT INTO x VALUES (1)")
	_, _ = ca.ExecContext(ctx, "ROLLBACK")
	if busyErr == nil {
		t.Fatal("expected SQLITE_BUSY")
	}
	if !IsRetryable(busyErr) {
		t.Fatalf("modernc busy error not retryable: %v", busyErr)
	}
	if !IsRetryable(fmt.Errorf("wrapped: %w", busyErr)) {
		t.Fatal("%w-wrapped busy error not retryable")
	}
	if !IsRetryable(fmt.Errorf("flattened: %v", busyErr)) {
		t.Fatalf("%%v-flattened busy error not retryable: %v", busyErr)
	}

	// A constraint violation is a real modernc error that must not be retried.
	mustExecT(t, a, "CREATE TABLE u (v UNIQUE)")
	mustExecT(t, a, "INSERT INTO u VALUES (1)")
	if _, err := a.Exec("INSERT INTO u VALUES (1)"); err == nil || IsRetryable(err) {
		t.Fatalf("constraint error: %v (retryable=%v)", err, IsRetryable(err))
	}

	// Multiwriter SQLITE_BUSY_SNAPSHOT: a transaction whose read set was
	// changed by another writer's commit loses its own commit.
	mwPath := filepath.Join(dir, "mw.db")
	d1 := openEngineT(t, mwPath)
	d2 := openEngineT(t, mwPath)
	mustExecT(t, d1, "CREATE TABLE c (id INTEGER PRIMARY KEY, n INT)")
	mustExecT(t, d1, "INSERT INTO c VALUES (1, 0)")
	snapErr := conflictOnce(t, d1, d2)
	if snapErr == nil {
		t.Fatal("expected the stale transaction to lose its commit")
	}
	if !multiwriter.IsBusySnapshot(snapErr) || !IsRetryable(snapErr) {
		t.Fatalf("lost commit error not recognised: %v", snapErr)
	}
}

// conflictOnce runs a transaction on a that reads row 1 of c, lets b commit an
// update to it, then writes it. It returns the error of the losing statement or
// commit (nil if the transaction unexpectedly succeeded).
func conflictOnce(t *testing.T, a, b *sql.DB) error {
	t.Helper()
	tx, err := a.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow("SELECT n FROM c WHERE id = 1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	mustExecT(t, b, "UPDATE c SET n = n + 100 WHERE id = 1")
	if _, err := tx.Exec("UPDATE c SET n = ? WHERE id = 1", n+1); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TestEngineRunTxRetriesLostCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtx.db")
	a := openEngineT(t, path)
	b := openEngineT(t, path)
	mustExecT(t, a, "CREATE TABLE c (id INTEGER PRIMARY KEY, n INT)")
	mustExecT(t, a, "INSERT INTO c VALUES (1, 0)")

	attempts := 0
	err := RunTx(context.Background(), a, func(tx *sql.Tx) error {
		attempts++
		var n int
		if err := tx.QueryRow("SELECT n FROM c WHERE id = 1").Scan(&n); err != nil {
			return err
		}
		if attempts == 1 {
			// Another writer commits to the row this transaction just read.
			if _, err := b.Exec("UPDATE c SET n = n + 1 WHERE id = 1"); err != nil {
				return fmt.Errorf("interfering writer: %w", err)
			}
		}
		_, err := tx.Exec("UPDATE c SET n = ? WHERE id = 1", n+1)
		return err
	})
	if err != nil {
		t.Fatalf("RunTx: %v", err)
	}
	if attempts < 2 {
		t.Fatalf("RunTx ran fn %d time(s); the conflicting transaction must have been retried", attempts)
	}
	var n int
	if err := b.QueryRow("SELECT n FROM c WHERE id = 1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2 (one interfering + one retried increment, no lost update)", n)
	}

	// A non-retryable error from fn is returned at once, without retry.
	calls := 0
	sentinel := errors.New("stop")
	if err := RunTx(context.Background(), a, func(*sql.Tx) error { calls++; return sentinel }); !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("RunTx(non-retryable) = %v after %d calls", err, calls)
	}
}

func TestEngineRunTxConcurrentReadModifyWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rmw.db")
	dbs := []*sql.DB{openEngineT(t, path), openEngineT(t, path)}
	mustExecT(t, dbs[0], "CREATE TABLE c (id INTEGER PRIMARY KEY, n INT)")
	mustExecT(t, dbs[0], "INSERT INTO c VALUES (1, 0)")
	const workers, per = 6, 25
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(conn *sql.DB) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				err := RunTx(context.Background(), conn, func(tx *sql.Tx) error {
					var n int
					if err := tx.QueryRow("SELECT n FROM c WHERE id = 1").Scan(&n); err != nil {
						return err
					}
					_, err := tx.Exec("UPDATE c SET n = ? WHERE id = 1", n+1)
					return err
				})
				if err != nil {
					errs <- err
					return
				}
			}
		}(dbs[w%2])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("RunTx: %v", err)
	}
	var n int
	if err := dbs[1].QueryRow("SELECT n FROM c WHERE id = 1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != workers*per {
		t.Fatalf("n = %d, want %d (lost updates)", n, workers*per)
	}
}

func TestEngineAutocommitReturningUnderContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ret.db")
	dbs := []*sql.DB{openEngineT(t, path), openEngineT(t, path)}
	mustExecT(t, dbs[0], "CREATE TABLE c (id INTEGER PRIMARY KEY, n INT)")
	mustExecT(t, dbs[0], "INSERT INTO c VALUES (1, 0)")
	const workers, per = 8, 40
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen []int
	)
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(conn *sql.DB) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				var n int
				if err := conn.QueryRow("UPDATE c SET n = n + 1 WHERE id = 1 RETURNING n").Scan(&n); err != nil {
					errs <- err
					return
				}
				mu.Lock()
				seen = append(seen, n)
				mu.Unlock()
			}
		}(dbs[w%2])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("UPDATE ... RETURNING: %v", err)
	}
	var n int
	if err := dbs[0].QueryRow("SELECT n FROM c WHERE id = 1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != workers*per {
		t.Fatalf("counter = %d, want %d", n, workers*per)
	}
	// Every returned value belongs to a committed increment: exactly 1..N.
	sort.Ints(seen)
	for i, v := range seen {
		if v != i+1 {
			t.Fatalf("returned values are not 1..%d: position %d holds %d", workers*per, i, v)
		}
	}
}

// openEngineT opens path through OpenNoMigrate (the driver tests bring their
// own tables) and closes it at the end of the test.
func openEngineT(t testing.TB, path string) *sql.DB {
	t.Helper()
	conn, err := OpenNoMigrate(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = Close(conn) })
	return conn
}

func mustExecT(t testing.TB, conn *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}
