// Package migrate owns the schema of the rag module: the knowledge-base
// tables (kb_documents, kb_chunks, kb_fts, kb_links) and the session events
// tables, as goose SQL migrations.
//
// There are two ways to apply them:
//
//   - A program that only uses the rag module calls Up, which records the
//     applied versions in its own table, rag_goose_version, so it never
//     collides with another goose history in the same database.
//   - Pando, whose existing databases already recorded these exact versions
//     in its own goose_db_version table, does not call Up. It merges FS into
//     its migration directory with Merge and keeps running a single goose
//     history. The file names and versions here are the ones Pando has
//     always shipped, so an existing database sees nothing new.
//
// New migrations for the rag schema are added here, with versions later than
// every existing one, and reach both kinds of consumer.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
)

// TableName is the goose version table Up uses.
const TableName = "rag_goose_version"

// Dir is the directory inside FS that holds the SQL migrations.
const Dir = "sql"

// FS holds the rag schema migrations under Dir.
//
//go:embed sql/*.sql
var FS embed.FS

// Up applies every pending rag migration to db, recording them in
// TableName. db must be a SQLite database with FTS5 available.
func Up(ctx context.Context, db *sql.DB) error {
	p, err := newProvider(db)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("rag migrate: up: %w", err)
	}
	return nil
}

// Pending reports how many rag migrations Up would still apply to db.
func Pending(ctx context.Context, db *sql.DB) (int, error) {
	p, err := newProvider(db)
	if err != nil {
		return 0, err
	}
	status, err := p.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("rag migrate: status: %w", err)
	}
	n := 0
	for _, s := range status {
		if s.State == goose.StatePending {
			n++
		}
	}
	return n, nil
}

func newProvider(db *sql.DB) (*goose.Provider, error) {
	store, err := database.NewStore(database.DialectSQLite3, TableName)
	if err != nil {
		return nil, fmt.Errorf("rag migrate: store: %w", err)
	}
	sub, err := fs.Sub(FS, Dir)
	if err != nil {
		return nil, fmt.Errorf("rag migrate: %w", err)
	}
	p, err := goose.NewProvider("", db, sub,
		goose.WithStore(store),
		// Programs that embed this module may register their own Go
		// migrations with goose's global registry; they are not ours.
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, fmt.Errorf("rag migrate: provider: %w", err)
	}
	return p, nil
}
