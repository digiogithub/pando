package db

import (
	"embed"
	"io/fs"

	"github.com/digiogithub/pando/rag/migrate"
)

//go:embed migrations/*.sql
var pandoMigrations embed.FS

// FS is the migration filesystem goose runs from: Pando's own migrations
// plus the knowledge-base and events schema, which lives in the rag module
// (github.com/digiogithub/pando/rag/migrate). The rag files keep the names
// and versions Pando always shipped them under, so both sets form one goose
// history in goose_db_version and an existing database sees no new version.
var FS fs.FS = mustMergeMigrations()

func mustMergeMigrations() fs.FS {
	merged, err := migrate.Merge(pandoMigrations, "migrations")
	if err != nil {
		// A rag migration sharing a name or version with one of Pando's is a
		// build-time mistake; failing at start-up beats goose skipping one.
		panic(err)
	}
	return merged
}
