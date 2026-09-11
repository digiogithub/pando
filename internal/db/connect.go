package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	sqlite3 "github.com/ncruces/go-sqlite3"
	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"

	"github.com/pressly/goose/v3"
)

// Busy timeouts applied to every pooled connection via openPool's init
// callback (see poolOptions.busyTimeout). Chosen so that:
//   - The primary can afford to wait out a normal-sized write (index
//     replace, KB write, ...) instead of failing instantly.
//   - A secondary keeps failing fast on its single direct-write connection
//     and falls back to the IPC proxy, as it always has.
const (
	primaryBusyTimeout   = 10 * time.Second
	secondaryBusyTimeout = 200 * time.Millisecond
)

// buildDSN turns a plain SQLite filesystem path into a "file:" URI DSN
// carrying extra as query parameters. Building it with net/url ensures a
// path containing spaces or other special characters is escaped correctly.
//
// The path is resolved to an absolute one first. SQLite's URI filename
// parsing expects the path component to be absolute; unlike a plain
// (non-URI) filename — which SQLite resolves against the process's current
// directory exactly like any other relative file access — a *relative*
// "file:" URI path is not reliably supported and fails to open ("unable to
// open database file"). config.Get().Data.Directory defaults to the
// relative ".pando", so this matters for every real caller, not just tests.
//
// ":memory:" and the empty path are returned unchanged: SQLite's plain
// (non-URI) in-memory filename does not accept query parameters (e.g.
// "_txlock"), so a connection opened this way keeps the driver's defaults
// (DEFERRED transactions, 1-minute busy_timeout). No production caller of
// Connect/ConnectRWSecondary/ConnectReadOnly ever passes ":memory:" — only
// tests do, and a test that needs IMMEDIATE-transaction or
// per-connection-pragma behavior should use a temp-file DB instead (see
// internal/db/connect_test.go).
func buildDSN(path string, extra url.Values) string {
	if path == "" || path == ":memory:" {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	u := url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		RawQuery: extra.Encode(),
	}
	return u.String()
}

// poolOptions configures a pooled SQLite connection opened via openPool.
type poolOptions struct {
	// busyTimeout is applied to every pooled connection through the driver's
	// init callback (see openPool). It runs AFTER the ncruces driver's own
	// 1-minute default BusyTimeout call (that default only fires when the
	// DSN carries no "_pragma" parameter, which ours never does — see
	// driver.go's connector.Connect), so it overrides that default rather
	// than being overridden by it.
	busyTimeout time.Duration
	// immediateWrites adds "_txlock=immediate" to the DSN, so a
	// db.BeginTx(ctx, nil) (default isolation, not explicitly read-only)
	// issues "BEGIN IMMEDIATE" instead of "BEGIN DEFERRED". This is what
	// makes busy_timeout apply to a read-then-write transaction: SQLite
	// never invokes the busy handler when a DEFERRED transaction upgrades
	// from a read lock to a write lock mid-transaction (ncruces
	// driver.go:BeginTx only consults c.txLock — the DSN's _txlock — for the
	// default isolation level), so any commit by another connection between
	// the read and the write fails instantly with SQLITE_BUSY /
	// SQLITE_BUSY_SNAPSHOT instead of waiting out busy_timeout. IMMEDIATE
	// takes the write lock at BEGIN, before any read happens, so the
	// upgrade never occurs.
	//
	// A transaction opened with &sql.TxOptions{ReadOnly: true} always stays
	// DEFERRED regardless of this setting (same BeginTx switch: the
	// ReadOnly branch never consults c.txLock), so pure-read transactions
	// are unaffected and do not need to opt out.
	immediateWrites bool
	// perConnPragmas are PRAGMA statements applied to every pooled
	// connection through the init callback. Unlike a one-off db.Exec (which,
	// under database/sql's pool, only ever reaches whichever single
	// connection happens to be idle at that moment), these apply uniformly —
	// including to a connection database/sql opens later to replace one it
	// closed. Only include pragmas that do NOT persist in the database file
	// itself (journal_mode and page_size do persist; foreign_keys,
	// synchronous and cache_size do not and must be set per-connection).
	perConnPragmas string
	readOnly       bool
	maxOpenConns   int
	maxIdleConns   int
}

// openPool opens dbPath through the ncruces sqlite3 driver's Open(dsn, init)
// entry point, which — unlike sql.Open plus a later db.Exec — runs init on
// EVERY connection the pool creates, now and later. See poolOptions for what
// each field controls.
func openPool(dbPath string, opts poolOptions) (*sql.DB, error) {
	query := url.Values{}
	if opts.readOnly {
		query.Set("mode", "ro")
	} else if opts.immediateWrites {
		query.Set("_txlock", "immediate")
	}
	dsn := buildDSN(dbPath, query)

	sqlDB, err := sqlite3driver.Open(dsn, func(c *sqlite3.Conn) error {
		if opts.busyTimeout > 0 {
			if err := c.BusyTimeout(opts.busyTimeout); err != nil {
				return fmt.Errorf("busy_timeout: %w", err)
			}
		}
		if opts.perConnPragmas != "" {
			if err := c.Exec(opts.perConnPragmas); err != nil {
				return fmt.Errorf("per-connection pragmas: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, err
	}

	if opts.maxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(opts.maxOpenConns)
	}
	if opts.maxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(opts.maxIdleConns)
	}
	return sqlDB, nil
}

// ConnectReadOnly opens the existing SQLite database in read-only mode.
// Secondary (non-primary) Pando instances use this so they never write
// directly; all writes are proxied through the primary via ZMQ RPC.
// The caller must NOT run migrations — the primary is responsible for that.
func ConnectReadOnly() (*sql.DB, error) {
	dataDir := config.Get().Data.Directory
	if dataDir == "" {
		return nil, fmt.Errorf("data.dir is not set")
	}
	dbPath := filepath.Join(dataDir, "pando.db")

	// mode=ro never writes, so there is no IMMEDIATE/DEFERRED distinction to
	// make and no foreign_keys/synchronous/cache_size pragma to set; only a
	// busy_timeout, for the rare case a reader briefly waits behind a
	// checkpoint.
	conn, err := openPool(dbPath, poolOptions{
		readOnly:     true,
		busyTimeout:  primaryBusyTimeout,
		maxOpenConns: 4,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect read-only database: %w", err)
	}
	return conn, nil
}

// ConnectForRole opens the database in read-write mode when isPrimary is true,
// or via ConnectRWSecondary when false. This avoids duplicating the role branch
// in callers.
func ConnectForRole(isPrimary bool) (*sql.DB, error) {
	if isPrimary {
		return Connect()
	}
	return ConnectRWSecondary()
}

// ConnectRWSecondary opens the SQLite database in read-write mode with WAL and
// a short busy_timeout (200 ms). It does NOT run migrations — the primary owns
// schema management. Secondary instances use this connection to attempt direct
// WAL writes; if they get SQLITE_BUSY/LOCKED they fall back to the IPC proxy.
func ConnectRWSecondary() (*sql.DB, error) {
	dataDir := config.Get().Data.Directory
	if dataDir == "" {
		return nil, fmt.Errorf("data.dir is not set")
	}
	dbPath := filepath.Join(dataDir, "pando.db")

	conn, err := openPool(dbPath, poolOptions{
		immediateWrites: true,
		busyTimeout:     secondaryBusyTimeout,
		perConnPragmas:  "PRAGMA synchronous = NORMAL; PRAGMA foreign_keys = ON;",
		// Limit to a single writer so concurrent secondary writes don't race
		// each other before reaching the proxy fallback.
		maxOpenConns: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect secondary RW database: %w", err)
	}

	// journal_mode persists in the database file header, so it only needs
	// to be set once — it is not lost when the pool opens further
	// connections, unlike the per-connection pragmas above.
	if _, err = conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set journal_mode on secondary RW connection: %w", err)
	}
	return conn, nil
}

func Connect() (*sql.DB, error) {
	dataDir := config.Get().Data.Directory
	if dataDir == "" {
		return nil, fmt.Errorf("data.dir is not set")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	dbPath := filepath.Join(dataDir, "pando.db")

	// Cap the connection pool. The WASM-based SQLite driver (ncruces/go-sqlite3)
	// instantiates a separate WASM module per connection, each holding 3 real
	// file descriptors (db, wal, shm). Without a cap, concurrent workers (code
	// indexer, KB sync, etc.) can open hundreds of connections under load,
	// exhausting file descriptors and spiking CPU. SQLite in WAL mode supports
	// concurrent readers but only one writer, so a small pool is optimal.
	sqlDB, err := openPool(dbPath, poolOptions{
		immediateWrites: true,
		busyTimeout:     primaryBusyTimeout,
		perConnPragmas:  "PRAGMA foreign_keys = ON; PRAGMA synchronous = NORMAL; PRAGMA cache_size = -8000;",
		maxOpenConns:    8,
		maxIdleConns:    4,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// journal_mode and page_size persist in the database file header, so
	// (unlike foreign_keys/synchronous/cache_size above) they only need to
	// be set once, on any one connection, rather than through the
	// per-connection init callback.
	onceOffPragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA page_size = 4096;",
	}
	for _, pragma := range onceOffPragmas {
		if _, err = sqlDB.Exec(pragma); err != nil {
			logging.Error("Failed to set pragma", pragma, err)
		} else {
			logging.Debug("Set pragma", "pragma", pragma)
		}
	}

	goose.SetBaseFS(FS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		logging.Error("Failed to set dialect", "error", err)
		return nil, fmt.Errorf("failed to set dialect: %w", err)
	}

	if err := goose.Up(sqlDB, "migrations"); err != nil {
		logging.Error("Failed to apply migrations", "error", err)
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}
	return sqlDB, nil
}
