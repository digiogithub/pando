package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"

	"github.com/pressly/goose/v3"
)

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
	// Use file URI with mode=ro so the driver never acquires a write lock.
	dbURI := fmt.Sprintf("file:%s?mode=ro", dbPath)
	conn, err := sql.Open("sqlite3", dbURI)
	if err != nil {
		return nil, fmt.Errorf("failed to open read-only database: %w", err)
	}
	if err = conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to connect read-only database: %w", err)
	}
	// Allow multiple concurrent readers.
	conn.SetMaxOpenConns(4)
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
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open secondary RW database: %w", err)
	}
	if err = conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to connect secondary RW database: %w", err)
	}
	// Limit to a single writer so concurrent secondary writes don't race each
	// other before reaching the proxy fallback.
	conn.SetMaxOpenConns(1)
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 200;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
	}
	for _, pragma := range pragmas {
		if _, err = conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to set pragma %q on secondary RW connection: %w", pragma, err)
		}
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
	// Open the SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Verify connection
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Set pragmas for better performance
	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA page_size = 4096;",
		"PRAGMA cache_size = -8000;",
		"PRAGMA synchronous = NORMAL;",
	}

	for _, pragma := range pragmas {
		if _, err = db.Exec(pragma); err != nil {
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

	if err := goose.Up(db, "migrations"); err != nil {
		logging.Error("Failed to apply migrations", "error", err)
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}
	return db, nil
}
