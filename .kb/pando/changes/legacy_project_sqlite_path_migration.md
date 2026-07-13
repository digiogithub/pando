---
created_at: 2026-07-13T16:19:44.743675951Z
updated_at: 2026-07-13T16:19:44.743675951Z
tags:
    - change
    - database
    - sqlite
    - migration
    - startup
    - config
---
# Legacy Project SQLite Database Path Migration — Implementation

Implements the plan `pando/plans/legacy-project-sqlite-path-migration.md` (2026-07-13). Status: **COMPLETE (Phases 1-4)**.

## What was changed

Startup now reconciles the obsolete project-local database `<workdir>/.pando/pando.db` with the
current configured path `<Data.Directory>/pando.db` (normally `<workdir>/.pando/data/pando.db`),
before any SQLite connection is opened.

Behaviour:
- Legacy-only project → the main DB and its `-wal`, `-shm`, `-journal` sidecars are **moved** to the
  current data directory (sidecars first, main file last, so a failure leaves the legacy DB usable).
- Current DB exists → it is **authoritative**: never modified; every legacy artifact is **deleted**.
- Neither exists → no-op; no data directory and no empty DB are created (`db.Connect` still owns that).
- Legacy path == current path (setups that keep `Data.Directory = ".pando"`) → no-op.
- Any failure (target conflict, unexpected directory at an artifact path, copy/remove error) aborts
  `config.Load` with an error instead of silently opening a fresh empty database. Nothing is ever
  overwritten.
- Idempotent: safe on every startup.

## Files / symbols touched

- **`internal/config/legacy_database.go` (new)**
  - `LegacyDatabaseMigrationResult{Migrated, Cleaned}`
  - `MigrateLegacyProjectDatabase(workingDir, dataDirectory string)` — public entry point. Lives in
    `config` (not `db`) to avoid a config→db import cycle and to keep temp-dir testing direct. It never
    opens SQLite, never checkpoints WAL, never touches the schema.
  - Helpers: `databaseArtifactPaths` (main + `-wal`/`-shm`/`-journal`), `regularFileExists` (a
    non-regular entry at an artifact path is an error, never removed), `moveDatabaseArtifact`
    (rename, falling back to copy-then-remove for cross-filesystem data dirs; never overwrites),
    `copyDatabaseArtifact` (O_EXCL create + fsync).
  - Constants `databaseFileName = "pando.db"`, `databaseSidecarSuffixes`.
- **`internal/config/config.go`** — `Load()` calls `MigrateLegacyProjectDatabase(cfg.WorkingDir,
  cfg.Data.Directory)` right after `applyDefaultValues()` (post-unmarshal, post-WorkingDir restore),
  which precedes `ipcruntime.Bootstrap` and every `db.Connect` / `ConnectReadOnly` /
  `ConnectRWSecondary`. Failure returns a wrapped error. The outcome is logged (`logging.Info`,
  "Migrated legacy project database" / "Removed obsolete project database") after the logger is
  configured.
- **`internal/config/legacy_database_test.go` (new)** — the 8 cases required by the plan.
- **`test/integration/single_writer/helpers_test.go`** — `newTestEnv` now writes a minimal
  `.pando.toml` with `[Data] Directory = './.pando/data'` (what a real initialized project gets from
  `DefaultConfigTemplate`), `waitForDB` waits on `.pando/data/pando.db` instead of the obsolete
  `.pando/pando.db`, and the dead `PANDO_DATA_DIR` env var (never bound by viper) was dropped. The
  IPC lock stays at `.pando/ipc.lock` (independent of `Data.Directory`).
- **`README.md`** — data-directory examples updated to `.pando/data`, plus a new section
  "Data Directory and Legacy Database Migration" documenting old vs current path, current-DB
  precedence, sidecar handling and safe startup failure.

## Notes / decisions

- `defaultDataDirectory` was deliberately left as `.pando`. Changing it would also relocate
  `commands/`, `skills/`, `snapshots/`, `tls/` and the `init` flag for default-config users — out of
  the plan's scope. Such setups hit the "legacy == current path" no-op branch.
- Relative `Data.Directory` values are resolved against `workingDir` for path comparison.

## Verification

- `go test ./internal/config ./internal/db ./internal/llm/agent ./internal/api` — PASS.
- `go build ./... && go test ./...` — PASS except 3 pre-existing failures in `internal/mcpgateway`
  (`catalog_pagination_test.go`, "toml: expected character ="), reproduced identically on a clean
  HEAD worktree; unrelated to this change.
- Single-writer integration suite (`PANDO_BINARY=<abs> go test -tags=integration
  ./test/integration/single_writer/...`): the DB-path tests pass against `.pando/data/pando.db`.
  Two tests (`TestSingleWriterServeModeSecondInstanceDoesNotCrash`, `TestCrossEntrypointSingleWriter`,
  "secondary stole the lock") fail identically on an unmodified HEAD binary → pre-existing.
  Note: the `make test-integration` target is broken independently of this change — it passes
  `PANDO_BINARY=./pando`, which `go test` resolves against the test package directory, not the repo
  root; use an absolute path.
- In the Pando repo itself both paths exist (`.pando/pando.db` from 2026-03-28 and the current
  `.pando/data/pando.db`), so the next startup takes the cleanup branch and deletes the obsolete file.
