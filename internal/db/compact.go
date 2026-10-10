package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

// CompactOptions controls how Compact reclaims space.
type CompactOptions struct {
	// Incremental, when true, only reclaims already-freed pages via
	// PRAGMA incremental_vacuum (cheap, no full file rewrite). It only has an
	// effect when the database is already in auto_vacuum=INCREMENTAL mode.
	Incremental bool
	// EnableAutoVacuum, when true, switches the database into
	// auto_vacuum=INCREMENTAL before running the full VACUUM so future
	// reclamation can be done incrementally. Ignored when Incremental is true.
	EnableAutoVacuum bool
}

// CompactResult reports the outcome of a Compact call. Sizes are the logical
// database size (page_count * page_size) in bytes, which matches the on-disk
// main file size after the trailing WAL checkpoint.
type CompactResult struct {
	// Mode is "full" or "incremental".
	Mode string
	// SizeBefore/SizeAfter are the database sizes in bytes.
	SizeBefore int64
	SizeAfter  int64
	// Freed is SizeBefore - SizeAfter. It can be negative in rare cases.
	Freed int64
}

// Compact reclaims unused space in the SQLite database.
//
// With opts.Incremental it runs PRAGMA incremental_vacuum (only effective when
// auto_vacuum is already INCREMENTAL); otherwise it runs a full VACUUM,
// optionally switching the database into auto_vacuum=INCREMENTAL first so
// subsequent reclamation can be incremental.
//
// VACUUM must run outside any transaction and briefly acquires an exclusive
// lock; if another process is writing it may fail with SQLITE_BUSY rather than
// corrupting anything. Callers should surface such errors to the user.
func Compact(ctx context.Context, conn *sql.DB, opts CompactOptions) (CompactResult, error) {
	res := CompactResult{}
	res.SizeBefore = dbSizeBytes(ctx, conn)

	if opts.Incremental {
		res.Mode = "incremental"
		if _, err := conn.ExecContext(ctx, "PRAGMA incremental_vacuum;"); err != nil {
			return res, fmt.Errorf("db: incremental_vacuum: %w", err)
		}
	} else {
		res.Mode = "full"
		if opts.EnableAutoVacuum {
			// The mode change only takes effect after the following VACUUM.
			if _, err := conn.ExecContext(ctx, "PRAGMA auto_vacuum = INCREMENTAL;"); err != nil {
				return res, fmt.Errorf("db: set auto_vacuum: %w", err)
			}
		}
		if _, err := conn.ExecContext(ctx, "VACUUM;"); err != nil {
			return res, fmt.Errorf("db: vacuum: %w", err)
		}
	}

	// Truncate the WAL so the reclaimed space is reflected on disk.
	_, _ = conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE);")

	res.SizeAfter = dbSizeBytes(ctx, conn)
	res.Freed = res.SizeBefore - res.SizeAfter
	return res, nil
}

// dbSizeBytes returns the logical database size (page_count * page_size).
func dbSizeBytes(ctx context.Context, conn *sql.DB) int64 {
	var pageCount, pageSize int64
	if err := conn.QueryRowContext(ctx, "PRAGMA page_count;").Scan(&pageCount); err != nil {
		return 0
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA page_size;").Scan(&pageSize); err != nil {
		return 0
	}
	return pageCount * pageSize
}

// CompactPath compacts the database file at path. VACUUM rewrites the whole
// file, which no engine can do while another process has the database open, so
// it takes the exclusive usage lock and fails with ErrDatabaseInUse when any
// other Pando process (or another Open in this one) still uses the database.
//
// Under the multiwriter engine auto_vacuum must stay NONE (the engine never
// relocates pages): EnableAutoVacuum and Incremental are rejected there.
func CompactPath(ctx context.Context, path string, opts CompactOptions) (CompactResult, error) {
	engine := SelectedEngine()
	if engine == EngineMultiwriter && (opts.Incremental || opts.EnableAutoVacuum) {
		return CompactResult{}, fmt.Errorf("db: auto_vacuum/incremental compaction is not supported by the %s engine; run a full compaction", engine)
	}
	gate, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return CompactResult{}, fmt.Errorf("db: open lock file: %w", err)
	}
	defer gate.Close()
	var res CompactResult
	err = withExclusive(gate, 2*time.Second, func() error {
		// A crashed multiwriter process can leave commits in the <db>-mw log:
		// open and close the database once through the engine so they are
		// recovered and compacted into the file before it is rewritten.
		if _, statErr := os.Stat(path + "-mw"); statErr == nil {
			conn, err := openEngine(path, EngineMultiwriter)
			if err != nil {
				return fmt.Errorf("db: recover multiwriter log: %w", err)
			}
			if err := conn.Close(); err != nil {
				return fmt.Errorf("db: recover multiwriter log: %w", err)
			}
		}
		conn, err := sql.Open(DriverName, DSN(path, EngineSQLite))
		if err != nil {
			return err
		}
		defer conn.Close()
		conn.SetMaxOpenConns(1)
		res, err = Compact(ctx, conn, opts)
		return err
	})
	return res, err
}
