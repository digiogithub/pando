//go:build unix

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A database created with auto_vacuum=INCREMENTAL (as `pando db compact` used to
// leave it) is converted to auto_vacuum=NONE by Open, data intact.
func TestMaintenanceAutoVacuumConversion(t *testing.T) {
	skipNonMultiwriter(t)
	path := filepath.Join(t.TempDir(), "pando.db")
	// Plain stock modernc driver, rollback journal: auto_vacuum must be set
	// before the first table is created.
	stock, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stock.SetMaxOpenConns(1)
	mustExecT(t, stock, "PRAGMA auto_vacuum = INCREMENTAL")
	mustExecT(t, stock, "CREATE TABLE legacy (id INTEGER PRIMARY KEY, v TEXT)")
	for i := 0; i < 200; i++ {
		mustExecT(t, stock, "INSERT INTO legacy (v) VALUES (?)", fmt.Sprintf("row-%d", i))
	}
	var av int
	if err := stock.QueryRow("PRAGMA auto_vacuum").Scan(&av); err != nil || av != 2 {
		t.Fatalf("setup: auto_vacuum = %d (%v), want 2", av, err)
	}
	if err := stock.Close(); err != nil {
		t.Fatal(err)
	}
	if !needsAutoVacuumConversion(path) {
		t.Fatal("setup: header does not flag auto_vacuum")
	}

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open of an auto_vacuum database: %v", err)
	}
	if err := conn.QueryRow("PRAGMA auto_vacuum").Scan(&av); err != nil || av != 0 {
		t.Fatalf("auto_vacuum after Open = %d (%v), want 0", av, err)
	}
	var n int
	var last string
	if err := conn.QueryRow("SELECT COUNT(*), MAX(v) FROM legacy").Scan(&n, &last); err != nil || n != 200 {
		t.Fatalf("legacy rows = %d (%v), want 200", n, err)
	}
	checkGoose(t, conn)
	checkIntegrity(t, conn)
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}
	if needsAutoVacuumConversion(path) {
		t.Fatal("header still flags auto_vacuum after conversion")
	}
}

// CompactPath refuses while another process has the database open, succeeds
// once it is gone, and recovers commits a killed process left in the log.
func TestMaintenanceCompactPathInUse(t *testing.T) {
	skipNonMultiwriter(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "pando.db")
	conn, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mustExecT(t, conn, "CREATE TABLE hold_rows (id INTEGER PRIMARY KEY, child INT, v INT)")
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// A holder that exits cleanly.
	holder := spawnHolder(t, path, dir, 1, 50)
	t0 := time.Now()
	if _, err := CompactPath(ctx, path, CompactOptions{}); !errors.Is(err, ErrDatabaseInUse) {
		t.Fatalf("CompactPath while open elsewhere = %v, want ErrDatabaseInUse", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stop-1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("in-use refusal after %v", time.Since(t0))
	waitChild(t, holder, "holder 1")
	t0 = time.Now()
	if _, err := CompactPath(ctx, path, CompactOptions{}); err != nil {
		t.Fatalf("CompactPath after the holder exited: %v", err)
	}

	// A holder that is SIGKILLed: its commits may only be in the log.
	holder = spawnHolder(t, path, dir, 2, 50)
	if err := holder.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	t.Logf("compact after clean exit %v", time.Since(t0))
	_ = holder.Wait()
	if _, err := os.Stat(path + "-mw"); err != nil {
		t.Fatalf("a killed multiwriter process should leave its log behind: %v", err)
	}
	t0 = time.Now()
	if _, err := CompactPath(ctx, path, CompactOptions{}); err != nil {
		t.Fatalf("CompactPath after a killed holder: %v", err)
	}
	t.Logf("compact after kill %v", time.Since(t0))
	checkNoLog(t, path)

	// Incremental / auto_vacuum compaction is refused under the multiwriter engine.
	if _, err := CompactPath(ctx, path, CompactOptions{EnableAutoVacuum: true}); err == nil {
		t.Fatal("CompactPath(EnableAutoVacuum) succeeded under the multiwriter engine")
	}

	conn, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []int{1, 2} {
		if n := countRows(t, conn, "SELECT COUNT(*) FROM hold_rows WHERE child = ?", c); n != 50 {
			t.Errorf("holder %d rows = %d, want 50", c, n)
		}
	}
	checkIntegrity(t, conn)
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}
}

// A stock-SQLite opener (PANDO_DB_ENGINE=sqlite) must refuse a database another
// process holds through the multiwriter engine.
func TestMaintenanceEngineMixGuard(t *testing.T) {
	skipNonMultiwriter(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "pando.db")
	conn, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mustExecT(t, conn, "CREATE TABLE hold_rows (id INTEGER PRIMARY KEY, child INT, v INT)")
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}

	holder := spawnHolder(t, path, dir, 1, 5)
	t.Setenv(EngineEnv, string(EngineSQLite))
	if c, err := Open(path); err == nil {
		_ = Close(c)
		t.Fatal("stock-SQLite Open succeeded while a multiwriter process holds the database")
	}
	if err := os.WriteFile(filepath.Join(dir, "stop-1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitChild(t, holder, "holder")

	// With every multiwriter user gone (log compacted away) the fallback engine
	// opens the database and sees the holder's rows.
	c, err := Open(path)
	if err != nil {
		t.Fatalf("stock-SQLite Open after the multiwriter holder closed: %v", err)
	}
	if n := countRows(t, c, "SELECT COUNT(*) FROM hold_rows"); n != 5 {
		t.Errorf("rows = %d, want 5", n)
	}
	if err := Close(c); err != nil {
		t.Fatal(err)
	}
}

// spawnHolder starts a "hold" child that writes n rows, then waits until it
// signals readiness.
func spawnHolder(t *testing.T, path, dir string, id, n int) *exec.Cmd {
	t.Helper()
	cmd := spawnChild(t, "hold", path, id, n, childSyncEnv+"="+dir)
	ready := filepath.Join(dir, fmt.Sprintf("ready-%d", id))
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(ready); err == nil {
			_ = os.Remove(ready)
			return cmd
		}
		if time.Now().After(deadline) {
			t.Fatalf("holder %d never became ready", id)
		}
	}
}
