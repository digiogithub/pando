package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// Per-connection pragmas and pool sizes for each role. Shared by the Connect*
// constructors and by PromoteToPrimaryPool/DemoteToSecondaryPool, so a pool
// promoted in place ends up configured exactly like one opened by Connect.
const (
	primaryPerConnPragmas   = "PRAGMA foreign_keys = ON; PRAGMA synchronous = NORMAL; PRAGMA cache_size = -8000;"
	secondaryPerConnPragmas = "PRAGMA synchronous = NORMAL; PRAGMA foreign_keys = ON;"

	primaryMaxOpenConns   = 8
	primaryMaxIdleConns   = 4
	secondaryMaxOpenConns = 1
)

// poolState holds the per-connection settings a pool's init callback reads
// every time database/sql opens a new physical connection. They are atomics,
// not values captured by the callback, so a live pool can be reconfigured in
// place (see reconfigurePool): connections opened after the switch read the
// new values directly, and reconfigurePool re-applies them to the connections
// that already exist.
type poolState struct {
	busyTimeout    atomic.Int64 // a time.Duration
	perConnPragmas atomic.Pointer[string]
}

func (s *poolState) load() (time.Duration, string) {
	pragmas := ""
	if p := s.perConnPragmas.Load(); p != nil {
		pragmas = *p
	}
	return time.Duration(s.busyTimeout.Load()), pragmas
}

func (s *poolState) store(busyTimeout time.Duration, pragmas string) {
	s.busyTimeout.Store(int64(busyTimeout))
	s.perConnPragmas.Store(&pragmas)
}

// pools maps every *sql.DB opened by openPool to its poolState, so the
// promote/demote helpers can find the settings the pool's init callback reads.
// Entries are never removed: a process opens only a handful of pools, and a
// stale entry for a closed pool is harmless.
var pools sync.Map // *sql.DB -> *poolState

// applyConnSettings applies busyTimeout and pragmas to one physical connection.
func applyConnSettings(c *sqlite3.Conn, busyTimeout time.Duration, pragmas string) error {
	if busyTimeout > 0 {
		if err := c.BusyTimeout(busyTimeout); err != nil {
			return fmt.Errorf("busy_timeout: %w", err)
		}
	}
	if pragmas != "" {
		if err := c.Exec(pragmas); err != nil {
			return fmt.Errorf("per-connection pragmas: %w", err)
		}
	}
	return nil
}

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

	state := &poolState{}
	state.store(opts.busyTimeout, opts.perConnPragmas)

	sqlDB, err := sqlite3driver.Open(dsn, func(c *sqlite3.Conn) error {
		busyTimeout, pragmas := state.load()
		return applyConnSettings(c, busyTimeout, pragmas)
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
	pools.Store(sqlDB, state)
	return sqlDB, nil
}

// reconfigurePool switches a live pool to new per-connection settings, so that
// afterwards NO connection of the pool keeps the old ones:
//
//  1. The new values are stored in the pool's poolState first, so any
//     connection database/sql opens from now on gets them from the init
//     callback.
//  2. Then the helper checks out MaxOpenConnections connections at once and
//     re-applies the settings to each one through (*sql.Conn).Raw. A pool can
//     never hold more physical connections than MaxOpenConnections, so holding
//     that many means holding every connection the pool has: existing ones are
//     reconfigured here, and any it had to open to reach the count were already
//     configured by step 1. Checking out a connection another goroutine is
//     using waits until that goroutine returns it (bounded by ctx), so a
//     connection in use is reconfigured as soon as it is released rather than
//     skipped.
//
// This is why the pool size is only raised AFTER this call (promotion) or only
// lowered after it (demotion): the number of connections to hold is the
// current, smaller or equal, bound. Recycling idle connections instead
// (SetMaxIdleConns(0)) cannot give the same guarantee: a connection that is in
// use at that moment is returned to the pool later, still carrying the old
// busy_timeout.
func reconfigurePool(ctx context.Context, sqlDB *sql.DB, busyTimeout time.Duration, pragmas string) error {
	v, ok := pools.Load(sqlDB)
	if !ok {
		return fmt.Errorf("db: reconfigure pool: pool was not opened by openPool")
	}
	state := v.(*poolState)

	n := sqlDB.Stats().MaxOpenConnections
	if n <= 0 {
		// An unbounded pool has no count that guarantees reaching every
		// connection; every pool openPool creates is bounded.
		return fmt.Errorf("db: reconfigure pool: pool has no MaxOpenConns bound")
	}

	state.store(busyTimeout, pragmas)

	held := make([]*sql.Conn, 0, n)
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()
	for range n {
		c, err := sqlDB.Conn(ctx)
		if err != nil {
			return fmt.Errorf("db: reconfigure pool: check out connection: %w", err)
		}
		held = append(held, c)
		if err := c.Raw(func(driverConn any) error {
			rc, ok := driverConn.(interface{ Raw() *sqlite3.Conn })
			if !ok {
				return fmt.Errorf("unexpected driver connection type %T", driverConn)
			}
			return applyConnSettings(rc.Raw(), busyTimeout, pragmas)
		}); err != nil {
			return fmt.Errorf("db: reconfigure pool: apply settings: %w", err)
		}
	}
	return nil
}

// PromoteToPrimaryPool turns, in place, a pool opened by ConnectRWSecondary
// (1 connection, 200 ms busy_timeout, no migrations) into the primary's
// configuration: primary busy_timeout and pragmas on every connection
// (reconfigurePool), primaryMaxOpenConns/primaryMaxIdleConns, and the goose
// migrations Connect runs.
//
// The *sql.DB itself is kept — failover promotion must not close or reopen it,
// because the session/message queriers, history, project, the remembrances
// stores and every other service built by app.New hold this exact pool.
func PromoteToPrimaryPool(ctx context.Context, sqlDB *sql.DB) error {
	if err := reconfigurePool(ctx, sqlDB, primaryBusyTimeout, primaryPerConnPragmas); err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(primaryMaxOpenConns)
	sqlDB.SetMaxIdleConns(primaryMaxIdleConns)

	if err := runMigrations(sqlDB); err != nil {
		return err
	}
	return nil
}

// DemoteToSecondaryPool reverts PromoteToPrimaryPool's pool settings. It exists
// so a promotion that fails after the pool was upgraded can leave the process
// as a well-behaved secondary (short busy_timeout, single connection) again.
// Migrations that already ran are not reverted; they are forward-compatible.
func DemoteToSecondaryPool(ctx context.Context, sqlDB *sql.DB) error {
	if err := reconfigurePool(ctx, sqlDB, secondaryBusyTimeout, secondaryPerConnPragmas); err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(secondaryMaxOpenConns)
	return nil
}

// runMigrations applies the embedded goose migrations to sqlDB.
func runMigrations(sqlDB *sql.DB) error {
	goose.SetBaseFS(FS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		logging.Error("Failed to set dialect", "error", err)
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	if err := goose.Up(sqlDB, "migrations"); err != nil {
		logging.Error("Failed to apply migrations", "error", err)
		return fmt.Errorf("failed to apply migrations: %w", err)
	}
	return nil
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
	return ConnectRWSecondaryAt(filepath.Join(dataDir, "pando.db"))
}

// ConnectRWSecondaryAt is ConnectRWSecondary for an explicit database path.
func ConnectRWSecondaryAt(dbPath string) (*sql.DB, error) {
	conn, err := openPool(dbPath, poolOptions{
		immediateWrites: true,
		busyTimeout:     secondaryBusyTimeout,
		perConnPragmas:  secondaryPerConnPragmas,
		// Limit to a single writer so concurrent secondary writes don't race
		// each other before reaching the proxy fallback.
		maxOpenConns: secondaryMaxOpenConns,
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
	return ConnectAt(filepath.Join(dataDir, "pando.db"))
}

// ConnectAt is Connect for an explicit database path (the parent directory must
// already exist): primary pool, pragmas, and migrations.
func ConnectAt(dbPath string) (*sql.DB, error) {
	// Cap the connection pool. The WASM-based SQLite driver (ncruces/go-sqlite3)
	// instantiates a separate WASM module per connection, each holding 3 real
	// file descriptors (db, wal, shm). Without a cap, concurrent workers (code
	// indexer, KB sync, etc.) can open hundreds of connections under load,
	// exhausting file descriptors and spiking CPU. SQLite in WAL mode supports
	// concurrent readers but only one writer, so a small pool is optimal.
	sqlDB, err := openPool(dbPath, poolOptions{
		immediateWrites: true,
		busyTimeout:     primaryBusyTimeout,
		perConnPragmas:  primaryPerConnPragmas,
		maxOpenConns:    primaryMaxOpenConns,
		maxIdleConns:    primaryMaxIdleConns,
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

	if err := runMigrations(sqlDB); err != nil {
		return nil, err
	}
	return sqlDB, nil
}
