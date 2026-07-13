---
created_at: 2026-07-13T14:04:51.595301262Z
updated_at: 2026-07-13T14:04:51.595301262Z
tags:
    - plan
    - database
    - sqlite
    - migration
    - startup
    - configuration
---
# Legacy Project SQLite Database Path Migration Plan

## Goal
On startup, migrate the obsolete project-local SQLite path from <workdir>/.pando/pando.db to the configured current path <Data.Directory>/pando.db, normally <workdir>/.pando/data/pando.db.

If the current database exists, it is authoritative. Remove the obsolete database and related SQLite files instead of overwriting current data. The migration must be idempotent and complete before any SQLite connection opens.

## Current architecture
- internal/db/connect.go opens SQLite at filepath.Join(config.Get().Data.Directory, "pando.db").
- The shipped project configuration already sets Data.Directory to ./.pando/data.
- config.Load runs before internal/ipc/runtime.Bootstrap. Bootstrap is where primary and secondary SQLite connections are opened.
- SQLite runs in WAL mode and can have pando.db-wal, pando.db-shm, and pando.db-journal artifacts.
- TUI, Web UI/server, desktop, ACP-related commands, and maintenance commands all load configuration before their normal database initialization.

## Required behavior
1. Resolve legacy files from <workingDir>/.pando/pando.db and its -wal, -shm, and -journal sidecars.
2. Resolve current files from <configured Data.Directory>/pando.db and the same sidecars.
3. If the normalized legacy and current main paths are identical, do nothing. This preserves compatibility with configurations that still explicitly use .pando as Data.Directory.
4. When the current main database exists, delete all existing legacy artifacts. Never modify current main or current sidecars.
5. When current main is absent but legacy main exists:
   - create the current parent directory with the same restricted permissions used by database setup;
   - move sidecars first, then move the main database last;
   - never overwrite a target artifact;
   - prefer rename and fall back to copy-then-remove for cross-filesystem data directories.
6. When neither main database exists, do nothing and do not create an empty DB. db.Connect remains responsible for initialization.
7. If a legacy database was detected but cleanup or migration fails, fail startup before SQLite opens. Never silently create a fresh database in this case.
8. Emit a structured startup log for migrated and cleaned-legacy outcomes.

## Design
Create internal/config/legacy_database.go with this focused API:

    type LegacyDatabaseMigrationResult struct {
        Migrated bool
        Cleaned  bool
    }

    func MigrateLegacyProjectDatabase(
        workingDir string,
        dataDirectory string,
    ) (LegacyDatabaseMigrationResult, error)

Keep it in config to avoid a config-to-db import cycle and to make temporary-directory testing direct. It should use cleaned absolute paths for comparison and explicit input paths instead of global state.

Use private helpers for artifact path generation, expected-file validation, cleanup, move, and copy-then-remove. Treat an unexpected directory at a database artifact path as an error.

For a migration, process pando.db-wal, pando.db-shm, and pando.db-journal before pando.db. The final main-file move means any sidecar failure leaves the original main database intact. On retry, already-moved sidecars are accepted only if their source is absent; a source-and-target conflict is an error.

## Phases

### Phase 1: Isolated migration helper
1. Add internal/config/legacy_database.go.
2. Define the complete SQLite artifact set: main, -wal, -shm, and -journal.
3. Add path equality and legacy/current existence handling.
4. Implement current-authoritative cleanup.
5. Implement sidecar-first migration and safe non-overwriting copy fallback.
6. Add short comments documenting WAL safety and main-file-last ordering.

Exit criteria: no SQLite driver/schema dependency, no overwrite route, and repeatable no-op behavior.

### Phase 2: Startup integration
1. In internal/config/config.go Load, call the migration helper after configuration unmarshal, WorkingDir restoration, and applyDefaultValues.
2. Return a contextual error when migration fails.
3. Log the migration result after logger initialization, or through the safe default logger.
4. Confirm this precedes internal/ipc/runtime.Bootstrap and all db.Connect, db.ConnectReadOnly, and db.ConnectRWSecondary paths.

Exit criteria: every normal startup processes the migration once before opening SQLite.

### Phase 3: Tests
Create internal/config/legacy_database_test.go using temporary directories.

Required cases:
1. Legacy-only main, WAL, SHM, and journal files migrate with exact bytes and old files disappear.
2. Current main plus legacy artifacts keeps the current bytes and removes every legacy artifact.
3. A second run after migration is a no-op.
4. No legacy main creates no target directory or DB.
5. Identical legacy/current paths are a no-op.
6. Legacy main plus a target sidecar conflict returns an error and keeps the legacy main.
7. An artifact path that is a directory returns an error rather than removing it.
8. Config-load wiring migrates a legacy database before a DB connection is attempted.

Update the single-writer integration helper that currently waits for <workdir>/.pando/pando.db so it waits for <workdir>/.pando/data/pando.db, unless it is intentionally a legacy fixture.

### Phase 4: Documentation and verification
1. Document the old and current paths, current-database precedence, sidecar handling, and safe startup failure behavior.
2. Run:
    go test ./internal/config ./internal/db
    go test ./internal/llm/agent ./internal/api
    go test ./...
3. Run supported primary-writer integration tests.
4. Save implementation and verification results to Pando KB after completion.

## Scope boundaries
- This does not merge two databases: current data wins when both paths exist.
- This does not migrate arbitrary profile history; only the obsolete project-local path is in scope.
- This does not remove the .pando directory or unrelated files.
- This does not open SQLite, checkpoint WAL, or modify schema.

## Risks and mitigations
- WAL can contain committed transactions: migrate WAL and SHM with the main DB, before moving the main file.
- Cross-filesystem paths: use a copy that succeeds and closes before source deletion.
- Partial failures: retain legacy main until all sidecars have transferred.
- Concurrent startup: migration occurs before Pando IPC/SQLite initialization; document that users should not start a second instance during first migration.
- Stale tests: update expectations to the current data directory while retaining legacy-path fixtures for migration tests.

## Verification criteria
- A legacy-only project starts with database artifacts at .pando/data/pando.db and no legacy artifacts.
- A project with both paths starts against the existing current DB and removes only obsolete legacy artifacts.
- A failure neither deletes legacy data nor creates an empty current DB.
- Projects that never used the old path behave identically to today.
