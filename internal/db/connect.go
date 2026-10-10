package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	multiwriter "github.com/digiogithub/go-sqlite-multiwriter"
	"github.com/pressly/goose/v3"
	"modernc.org/sqlite"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

// Engine names the storage engine behind pando.db.
type Engine string

const (
	// EngineMultiwriter is go-sqlite-multiwriter in multi-process mode: every
	// process (and every connection) writes the database concurrently; commits
	// that collide are refused with SQLITE_BUSY_SNAPSHOT and retried. Unix only.
	EngineMultiwriter Engine = "multiwriter"
	// EngineSQLite is stock SQLite in WAL mode with BEGIN IMMEDIATE and a busy
	// timeout. It is the engine on Windows, where the multiwriter's
	// multi-process mode is not available, and the emergency fallback elsewhere
	// (PANDO_DB_ENGINE=sqlite).
	EngineSQLite Engine = "sqlite"
)

// EngineEnv selects the engine explicitly. Every process that opens a given
// database must use the same engine: a stock opener of a database the
// multiwriter engine holds would miss the commits still in its log.
const EngineEnv = "PANDO_DB_ENGINE"

// busyTimeout is how long a connection waits for a lock held by another
// process (log publication, checkpoints, stock-SQLite writers).
const busyTimeout = 10 * time.Second

// ErrDatabaseInUse is returned by maintenance operations that need every other
// process to have closed the database.
var ErrDatabaseInUse = errors.New("the database is open in another Pando process; close every other Pando instance using it and retry")

var errLockBusy = errors.New("db: lock held by another process")

// SelectedEngine returns the engine this process uses for pando.db.
func SelectedEngine() Engine {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EngineEnv))) {
	case string(EngineSQLite):
		return EngineSQLite
	case string(EngineMultiwriter):
		if runtime.GOOS != "windows" {
			return EngineMultiwriter
		}
	}
	if runtime.GOOS == "windows" {
		return EngineSQLite
	}
	return EngineMultiwriter
}

// DSN returns the data source name for path under engine.
func DSN(path string, engine Engine) string {
	extra := url.Values{}
	extra.Add("_pragma", "foreign_keys(1)")
	extra.Add("_pragma", "cache_size(-8000)")
	// Expressions over timestamp columns (MAX(created_at), COALESCE(...)) have
	// no declared type; decode them like the declared ones, as ncruces did.
	extra.Set("_texttotime", "1")
	if engine == EngineMultiwriter {
		return multiwriter.DSN(path, multiwriter.Options{
			MultiProcess: true,
			BusyTimeout:  busyTimeout,
		}) + "&" + extra.Encode()
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	extra.Add("_pragma", "journal_mode(WAL)")
	extra.Add("_pragma", "synchronous(NORMAL)")
	extra.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	extra.Set("_txlock", "immediate")
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + extra.Encode()
}

// DBPath returns the absolute path of the main SQLite database file.
func DBPath() (string, error) {
	dataDir := config.Get().Data.Directory
	if dataDir == "" {
		return "", fmt.Errorf("data.dir is not set")
	}
	return filepath.Join(dataDir, "pando.db"), nil
}

// Connect opens <data.directory>/pando.db, applying pending migrations. Every
// Pando process calls it — there is no primary/secondary distinction for the
// database: all of them read and write it directly through the engine.
func Connect() (*sql.DB, error) {
	path, err := DBPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	return Open(path)
}

// Open opens the database at path through the engine and applies pending
// migrations. Use Close to release it; a plain (*sql.DB).Close also works but
// keeps the process registered as a user of the database until it exits.
func Open(path string) (*sql.DB, error) {
	return open(path, SelectedEngine(), true)
}

// OpenNoMigrate is Open without running migrations (tests that apply their own).
func OpenNoMigrate(path string) (*sql.DB, error) {
	return open(path, SelectedEngine(), false)
}

// usage tracks the shared "in use" lock each open database holds on
// <db>.lock, so maintenance (VACUUM, auto_vacuum conversion) can tell whether
// any other process still has the database open.
var usage = struct {
	sync.Mutex
	byDB map[*sql.DB]*os.File
}{byDB: map[*sql.DB]*os.File{}}

func open(path string, engine Engine, migrate bool) (*sql.DB, error) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	gate, err := acquireUsage(path, engine)
	if err != nil {
		return nil, err
	}
	// Opening is serialised across processes: the first opener of a new
	// database converts it to WAL and initialises the multiwriter's shared
	// header and log, and the migrations must run once. Holding the lock for
	// the whole sequence keeps concurrent first starts out of each other's way;
	// it costs a few milliseconds per process start.
	conn, err := withOpenLock(path, func() (*sql.DB, error) {
		conn, err := openEngine(path, engine)
		if err != nil {
			return nil, err
		}
		if migrate {
			if err := runMigrations(conn); err != nil {
				_ = conn.Close()
				return nil, err
			}
		}
		return conn, nil
	})
	if err != nil {
		_ = gate.Close()
		return nil, err
	}
	usage.Lock()
	usage.byDB[conn] = gate
	usage.Unlock()
	return conn, nil
}

// withOpenLock runs fn holding the exclusive lock on <db>.migrate.lock.
func withOpenLock(path string, fn func() (*sql.DB, error)) (*sql.DB, error) {
	lf, err := os.OpenFile(path+".migrate.lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("db: open lock: %w", err)
	}
	defer lf.Close()
	if err := lockFile(lf, true, true); err != nil {
		return nil, fmt.Errorf("db: open lock: %w", err)
	}
	defer func() { _ = unlockFile(lf) }()
	return fn()
}

// Close closes conn and releases this process's claim on the database file.
func Close(conn *sql.DB) error {
	if conn == nil {
		return nil
	}
	err := conn.Close()
	usage.Lock()
	gate := usage.byDB[conn]
	delete(usage.byDB, conn)
	usage.Unlock()
	if gate != nil {
		_ = gate.Close()
	}
	return err
}

// openEngine opens the connection pool without taking the usage lock.
func openEngine(path string, engine Engine) (*sql.DB, error) {
	if engine == EngineSQLite {
		if err := refuseIfMultiwriterOpen(path); err != nil {
			return nil, err
		}
	}
	conn, err := sql.Open(DriverName, DSN(path, engine))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err := pingWithRetry(conn); err != nil {
		_ = conn.Close()
		if engine == EngineMultiwriter && strings.Contains(err.Error(), "unable to open") {
			return nil, fmt.Errorf("failed to connect to database (engine %s): %w; if another program has %s open without the multiwriter engine (an older Pando, a sqlite3 shell), close it and retry", engine, err, path)
		}
		return nil, fmt.Errorf("failed to connect to database (engine %s): %w", engine, err)
	}
	// Every pooled connection is an independent writer under the multiwriter
	// engine; a handful covers Pando's concurrency (agent, indexers, UI).
	conn.SetMaxOpenConns(8)
	conn.SetMaxIdleConns(4)
	// An idle connection pins nothing, but closing the extra ones lets the
	// engine reclaim per-lane memory after a burst.
	conn.SetConnMaxIdleTime(5 * time.Minute)
	logging.Debug("database opened", "path", path, "engine", engine)
	return conn, nil
}

// pingWithRetry opens the first connection. Besides lost-commit errors it
// retries SQLITE_CANTOPEN for a few seconds: the multiwriter engine reports it
// while another process is still initialising the shared header or log of the
// database (an opener that is not Pando's, or one started before this
// version's open lock existed).
func pingWithRetry(conn *sql.DB) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := retryLoop(context.Background(), func() error { return conn.Ping() })
		if err == nil || !isCantOpen(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func isCantOpen(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()&0xff == 14 // SQLITE_CANTOPEN
	}
	return strings.Contains(err.Error(), "unable to open database file")
}

// refuseIfMultiwriterOpen stops a stock-SQLite opener from touching a database
// whose commits may still live in a multiwriter log (another process has it
// open in multiwriter mode, or one crashed without compacting).
func refuseIfMultiwriterOpen(path string) error {
	if _, err := os.Stat(path + "-mw"); err == nil {
		return fmt.Errorf("db: %s is open with the multiwriter engine in another process (or one crashed): open it with %s=%s (the default) instead", path, EngineEnv, EngineMultiwriter)
	}
	return nil
}

// acquireUsage takes the shared usage lock, first converting a database the
// multiwriter engine cannot open (auto_vacuum != NONE) under the exclusive lock.
func acquireUsage(path string, engine Engine) (*os.File, error) {
	gate, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("db: open lock file: %w", err)
	}
	if engine == EngineMultiwriter && needsAutoVacuumConversion(path) {
		if err := withExclusive(gate, 5*time.Minute, func() error {
			if !needsAutoVacuumConversion(path) { // another process converted it meanwhile
				return nil
			}
			return convertAutoVacuum(path)
		}); err != nil {
			_ = gate.Close()
			return nil, err
		}
	}
	if err := lockFile(gate, false, false); err != nil {
		if !errors.Is(err, errLockBusy) {
			_ = gate.Close()
			return nil, fmt.Errorf("db: lock %s: %w", gate.Name(), err)
		}
		logging.Info("database maintenance in progress in another process; waiting", "path", path)
		if err := lockFile(gate, false, true); err != nil {
			_ = gate.Close()
			return nil, fmt.Errorf("db: lock %s: %w", gate.Name(), err)
		}
	}
	return gate, nil
}

// withExclusive runs fn holding the exclusive usage lock on gate, waiting up to
// wait for other processes to release theirs, then releases it.
func withExclusive(gate *os.File, wait time.Duration, fn func() error) error {
	deadline := time.Now().Add(wait)
	logged := false
	for {
		err := lockFile(gate, true, false)
		if err == nil {
			break
		}
		if !errors.Is(err, errLockBusy) {
			return fmt.Errorf("db: lock %s: %w", gate.Name(), err)
		}
		if time.Now().After(deadline) {
			return ErrDatabaseInUse
		}
		if !logged {
			logging.Info("waiting for other Pando processes to release the database", "lock", gate.Name())
			logged = true
		}
		time.Sleep(250 * time.Millisecond)
	}
	defer func() { _ = unlockFile(gate) }()
	return fn()
}

// needsAutoVacuumConversion reports whether the file header enables
// auto_vacuum/incremental_vacuum (bytes 52..55), which the multiwriter engine
// cannot open: it never relocates pages.
func needsAutoVacuumConversion(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := make([]byte, 100)
	if n, _ := f.ReadAt(h, 0); n < 100 || string(h[:15]) != "SQLite format 3" {
		return false
	}
	return h[52] != 0 || h[53] != 0 || h[54] != 0 || h[55] != 0
}

// convertAutoVacuum rewrites the database with auto_vacuum=NONE. The caller
// holds the exclusive usage lock, so no other Pando process has it open.
func convertAutoVacuum(path string) error {
	start := time.Now()
	// Logs usually go to a file; say it on stderr too so a TUI/CLI start that
	// pauses for a minute on a multi-GB database does not look hung.
	fmt.Fprintf(os.Stderr, "pando: one-time database upgrade for the multi-writer engine (VACUUM of %s); this can take a minute on large databases...\n", path)
	logging.Info("converting the database to auto_vacuum=NONE for the multiwriter engine (one-time VACUUM; may take a while on large databases)", "path", path)
	conn, err := sql.Open(DriverName, DSN(path, EngineSQLite))
	if err != nil {
		return fmt.Errorf("db: open for auto_vacuum conversion: %w", err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	ctx := context.Background()
	for _, stmt := range []string{"PRAGMA auto_vacuum = NONE", "VACUUM", "PRAGMA wal_checkpoint(TRUNCATE)"} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db: auto_vacuum conversion (%s): %w", stmt, err)
		}
	}
	logging.Info("database converted to auto_vacuum=NONE", "path", path, "duration", time.Since(start).Round(time.Millisecond))
	return nil
}

// runMigrations applies pending goose migrations. The caller holds the open
// lock, so concurrent first starts apply them once; a migration that loses a
// commit race against a writer that is already running is applied again
// (goose resumes from the last applied version).
func runMigrations(conn *sql.DB) error {
	goose.SetBaseFS(FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}
	if err := retryLoop(context.Background(), func() error { return goose.Up(conn, "migrations") }); err != nil {
		logging.Error("Failed to apply migrations", "error", err)
		return fmt.Errorf("failed to apply migrations: %w", err)
	}
	return nil
}
