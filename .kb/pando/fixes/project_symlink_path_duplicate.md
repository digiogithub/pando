---
created_at: 2026-06-19T11:15:50.626108704Z
updated_at: 2026-06-19T11:15:50.626108704Z
tags:
    - fix
    - projects
    - symlink
    - config
---
# Fix: project registered twice when path is under a symlink (2026-06-19)

## Problem
When a project lived under a path whose directory (or a parent directory) was a
symlink, Pando could register the same physical directory twice: once with the
symlink path and once with the resolved (real) path. They were treated as
distinct projects in the local DB and in the global registry.

## Root cause
Paths entered the system through several points that normalised them
inconsistently:
- `internal/project/service.go` `Create`/`GetByPath` used only `filepath.Abs`
  (no symlink resolution).
- `internal/config/global_projects.go` `RegisterSelfAsGlobalProject` (the
  auto-registration of the running instance's cwd at startup, called from
  `internal/app/app.go`) stored the cwd as-is.
- `SeedFromGlobal` then seeded those raw entries.
- Only `Manager.Register` (TUI/API path) resolved symlinks via the package-local
  `resolvePath` (abs + `filepath.EvalSymlinks`).

So an instance started from a symlinked cwd registered the link path, while the
same project registered via TUI/API stored the resolved path → duplicate.

## Fix
Introduced a single source of truth for project path canonicalisation and used
it at every entry point.

- `internal/config/global_projects.go`: new exported
  `CanonicalProjectPath(p string) string` — expands leading `~/`, `filepath.Abs`,
  then `filepath.EvalSymlinks` (falls back to abs when the path does not exist
  yet). Applied in `RegisterSelfAsGlobalProject` (canonicalise + canonical
  dedup compare) and `UpdateGlobalProjectName` (canonical match).
- `internal/project/service.go`: `Create` and `GetByPath` now canonicalise via
  `config.CanonicalProjectPath` instead of `filepath.Abs`. (Added `config`
  import; no import cycle — config does not import project.)
- `internal/project/manager.go`: `resolvePath` now delegates to
  `config.CanonicalProjectPath`; removed the now-unused `strings` import and the
  duplicated symlink logic.

## Files touched
- internal/config/global_projects.go
- internal/project/service.go
- internal/project/manager.go
- internal/project/service_test.go (new TestGetByPathResolvesSymlink)

## Verification
- `go build ./...` — OK.
- `go test ./internal/project/... ./internal/config/...` — all pass.
- New test `TestGetByPathResolvesSymlink`: creating via the real target then
  looking up / re-creating via a symlink resolves to the same project (unique
  path constraint hit), leaving exactly one row.
