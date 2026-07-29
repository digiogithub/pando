---
created_at: 2026-07-29T14:25:33.624590754Z
updated_at: 2026-07-29T14:25:33.624590754Z
tags:
    - feature
    - config
    - "2026-07-29"
---
# Feature: local config lookup walks up parent directories (2026-07-29)

## Motivation
Pando resolved the project ("local") config only in the current working
directory, plus the global profile config ($HOME/.pando.toml,
$HOME/.config/pando/…). A user with several projects under one workspace had to
duplicate `.pando.toml` in every project directory. Request: if no valid config
exists in the current path, walk up to parent directories until the filesystem
root and use the first config found, provided the user has read AND write
permission on it.

## What changed
New file `internal/config/local_discovery.go`:
- `FindLocalConfigFile(startDir) string` — checks `.pando.toml` then
  `.pando.json` in `startDir`, then in each parent directory, bounded by
  `maxConfigParentDepth = 128` and by `filepath.Dir(dir) == dir` (root).
- Acceptance requires read *and* write access: `isReadWritable` opens the file
  `O_RDONLY` and then `O_WRONLY` (no `O_TRUNC`/write, so side-effect free). A
  read-only or unreadable candidate is skipped and the walk continues upwards —
  Pando never selects a config it could not persist changes to.
- The walk stops right *after* the home directory: `$HOME/.pando.toml` is the
  global/profile scope and is loaded separately, and anything above home is out
  of the user's scope. `sameDirectory` compares with `filepath.EvalSymlinks`.
- `FindLocalConfigDir(startDir)` convenience wrapper.
- Opt-out env var `PANDO_CONFIG_PARENT_SEARCH=false` restores the old
  working-directory-only behaviour. It is an env var, not a config key, because
  it decides *which* config file gets loaded (chicken-and-egg).

Wiring:
- `internal/config/config.go` `mergeLocalConfig(workingDir)` now resolves the
  file with `FindLocalConfigFile` and uses `local.SetConfigFile(path)` instead
  of `SetConfigName` + `AddConfigPath(workingDir)`. Returns early when nothing
  is found. Provider-account merge logic unchanged.
- `internal/config/config.go` `ResolveConfigFilePath()` (used by
  `updateCfgFile`, so it decides where writes go) now uses the same discovery
  instead of only scanning `cfg.WorkingDir`. Fallback order after that is
  unchanged: `viper.ConfigFileUsed()` → `$HOME/.pando.{toml,json}` → legacy
  `$XDG_CONFIG_HOME/pando/config.*`.
- `internal/config/init.go` `HasLocalConfigFile()` now delegates to
  `FindLocalConfigFile(cwd)`, so `ShouldGenerateLocalConfig()` (TUI first-run
  dialog, `internal/api/handlers_config_init.go`, `internal/tui/tui.go`) no
  longer offers to generate a `.pando.toml` when an inherited one already
  applies.
- `DefaultConfigTemplate` header comment documents the new resolution order and
  the opt-out env var.

`cfg.WorkingDir` is unaffected: it stays the actual cwd, only the config file
location moves.

## Files touched
- internal/config/local_discovery.go (new)
- internal/config/local_discovery_test.go (new)
- internal/config/config.go (mergeLocalConfig, ResolveConfigFilePath)
- internal/config/init.go (HasLocalConfigFile, ShouldGenerateLocalConfig doc,
  DefaultConfigTemplate header)

## Verification
- `go build ./...` — OK.
- `go test ./internal/config/... ./internal/api/...` — all pass.
- New tests: working-dir wins over ancestor; ancestor found when working dir has
  none; `.toml` preferred over `.json`; read-only ancestor candidate skipped in
  favour of a writable one further up (skipped on Windows/root); search stops at
  `$HOME` so a config above home is ignored;
  `PANDO_CONFIG_PARENT_SEARCH=false` disables the ascent; end-to-end `Load()`
  from a nested dir picks up the parent config and `ResolveConfigFilePath()`
  returns it.

Related: [[project_symlink_path_duplicate]], [[fix_config_tests_home_leakage]]
