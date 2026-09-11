---
created_at: 2026-09-11T22:11:33.742947223Z
updated_at: 2026-09-11T22:11:33.742947223Z
tags:
    - fix
    - config
    - ipc
    - persistence
---
# Fix: config Update* saves stamped zero values over the whole .pando.toml (2026-09-12)

Found by the P4 isolated smoke test ([[pando/changes/ipc_other_entrypoints_p4.md]], "Follow-ups and risks").

## Bug
- Every save through the `config.Update*` functions rewrote the WHOLE target config file. `UpdateCronJobs` from the cron REST handlers is one example; the global-file writes behave the same way.
- The rewrite serialised a zero value for every key the file did not have. Examples:
  - `[Data] Directory = ''` blanked the default data dir, so every later process failed with `data.dir is not set`.
  - `Enabled = false` shadowed default-on features (`llmCache`, `skills`, `lspAutoActivate`, ...).
  - `''` shadowed values that come from the global config.
- Keys unknown to the running binary were dropped.
- On top of that, an IPC secondary whose DB failed to open got a `BootstrapResult` with `SQLDB == nil` and a nil error, and `app.New` then panicked. P4 guarded only `agui-serve` and `cronjob run`.

## Root cause
`updateConfigFileAt` (in `internal/config/config.go`, the single funnel behind `updateCfgFile` and `updateGlobalCfgFile`) did this:
1. Decoded the file into a fresh zero `Config`.
2. Ran the mutator.
3. Called `toml.Marshal` / `json.MarshalIndent` on the whole struct.

The TOML tags have no `omitempty`, and even with it go-toml/v2 emits every struct table. So every absent key came back as an explicit zero, and viper then preferred that zero over its default on the next load.

## Fix
### 1. The write funnel is now a patch (new file `internal/config/config_patch.go`)
- `rawTree`: the file parsed into a generic map (`parseConfigFileTree`). This is what gets written back, so unknown keys survive.
- `fillAbsentScalars(userCfg, effectiveLayeredConfig(), rawTree, format)`:
  - It seeds every scalar field the file does NOT set with its effective layered value, decoded from viper (defaults, global, local, overlays, env).
  - It walks plain struct fields only. Map entries and list elements are never invented, so mutators that look up accounts or servers by name see the file's own collections.
  - It skips `Telemetry` (global-only; viper can hold a project value that Load discards).
  - It skips paths covered by a runtime override (`--model`, `--log-file`), which are process-local.
  - Why the seeding is needed: when a mutator sets an absent key to zero over a non-zero default (e.g. `UpdateLLMCache(false)`), it must register as a change (true -> false). An untouched seeded key stays equal and is never written.
- `beforeFileTree` is taken after the fill and before the legacy providers migration, so the migration is still persisted, as it was before.
- The lock snapshot, `ensureNoLockedChange` and `encryptSensitiveConfigFields` are unchanged.
- `mergeConfigChanges(raw, before, after, persisted)`:
  - It walks tables key by key and compares lists whole.
  - It writes only the leaves that differ between `before` and `after`, taking the value from the encrypted rendering (`persisted`).
  - It deletes keys the mutation removed.
  - It never writes a new zero value.
  - It re-writes an unchanged leaf only when the file already holds it and its encrypted form differs, so plaintext secrets already in the file are still encrypted at rest.
  - Key lookups are case-insensitive (`setInsensitive` / `deleteInsensitive`), so the file's own spelling is kept and no duplicate keys appear.
- For a non-global file the telemetry key is deleted from the tree (any spelling). This replaces `stripTelemetryKey`, which was removed.
- Output: go-toml/json map marshal with sorted keys. This was already the case for project files. Comments were never preserved by this path.
- A missing file now starts as an empty tree (no more `{}`), so a new file contains only the changed keys.
- Audit: the only config-file write in `internal/config` is this funnel. `init.go` writes the templates, `global_projects.go` writes a separate registry and `agecrypto.go` writes the keys. Every `Update*` / `Set*` goes through `updateCfgFile` or `updateGlobalCfgFile`, and `TestConfigMutatorsAreLockAware` still passes.

### 2. Defence in depth (`applyDefaultValues`)
An empty or blank `Data.Directory` now falls back to `defaultDataDirectory` (`.pando`, the viper default), with a Warn log. The fallback runs before `MigrateLegacyProjectDatabase` and before any DB open, so files already damaged by the old funnel load again.

### 3. No more nil-DB panic (`internal/ipc/runtime/runtime.go`, `BootstrapWithOptions`)
- A secondary whose `db.ConnectRWSecondary()` fails now returns `nil, fmt.Errorf("ipc/runtime: open secondary DB: %w", err)` instead of a DB-less result.
- Every entrypoint already returns on a Bootstrap error: root TUI and ACP, serve, desktop, app, mcp-server, agui-serve, cronjob. So none of them can reach `app.New` with a nil pool any more.
- The P4 guards in `cmd/agui_serve.go` and `cmd/cronjob.go` are now redundant but harmless, and were kept.
- Also fixed: when the IPC client failed, the secondary path used to `Close()` the RW pool and then return that closed pool as `SQLDB`/`Querier`. It now keeps the pool open, and the cleanup closes it.

## Files and symbols
- `internal/config/config.go`:
  - `updateConfigFileAt` rewritten as read, fill, diff, patch, write.
  - `stripTelemetryKey` removed.
  - `applyDefaultValues` gains the Data.Directory fallback.
  - The `encoding/json` and go-toml imports were dropped (they now live in `config_patch.go`).
- `internal/config/config_patch.go` (new): `parseConfigFileTree`, `decodeConfigFile`, `configFileTree`, `marshalConfigFileTree`, `mergeConfigChanges`, `setInsensitive`, `deleteInsensitive`, `effectiveLayeredConfig`, `fillAbsentScalars`, `fillStructFields`, `structFieldKey`, `runtimeOverrideCovers`.
- `internal/config/config_update_patch_test.go` (new).
- `internal/ipc/runtime/runtime.go`: `BootstrapWithOptions`, secondary branch.
- No `cmd/` file changed.

## Tests (all use temp project dirs and `isolateGlobalConfig`)
- `TestUpdateCronJobsKeepsDefaultDataDirectoryInMinimalFile`:
  - Minimal file; the cron save adds only `[CronJobs]`, relative to the file as `Load` left it. `Load` itself may persist a host-detected evaluator default through `ensureEvaluatorDefaultModel`.
  - No `[Data]` table is written.
  - After a reload, the data dir is the default, the theme is kept, and llmCache and skills are still default-on.
- `TestUpdateCronJobsKeepsExplicitDataDirectory`
- `TestUpdateCronJobsDoesNotShadowGlobalDataDirectory`: the value comes only from the global `~/.pando.toml`.
- `TestUpdatePersistsExplicitZeroOverNonZeroDefault`: `UpdateLLMCache(false)`.
- `TestUpdatePreservesUnknownAndUnrelatedKeys`
- `TestUpdateEncryptsFileSecretsButNeverCopiesGlobalOnes`
- `TestLoadFallsBackToDefaultDataDirectoryWhenEmpty`
- Before the fix, the first test failed and showed the whole zero-valued struct dump.

## Verification
- Shared tree:
  - `go test -race -count=1 ./internal/config/...`: ok.
  - `go test -race -count=1 ./internal/ipc/...`: all ok.
  - `go vet ./internal/config ./internal/ipc/runtime`: ok.
  - `gofmt -l` on the changed files: clean.
- The shared tree's `go build ./...`, `./cmd` and `./internal/api` failed to build at the time only because of the parallel P5 agent's in-progress edits (`internal/app/app.go`, which calls `history.NewService` with the old signature, and `internal/design`).
- On a `git archive HEAD` export in the scratchpad, plus only these 4 files and the gitignored embed assets:
  - `go build ./...`: ok.
  - `go test -race -count=1 ./cmd/...`: ok.
  - `go test -count=1 ./internal/api/...`: ok.

## Already-damaged user files (repair note)
- Files rewritten by the old funnel contain a full zero-valued dump.
- The Data.Directory fallback makes them start again. Some zero knobs were already re-normalised on load (`normalizeInternalToolsDefaults` etc.).
- Other stamped zeros still shadow defaults, and the new funnel does not remove them: it cannot tell a stamped zero from a user-intended zero. Examples: `LLMCache.Enabled = false`, `Skills.Enabled = false`, `LSPAutoActivate = false`, `ContextPaths = []`.
- Recommended manual repair: delete the zero-valued keys or tables you never set from `.pando.toml` (or regenerate it with `pando init` and re-apply your settings).
- An automatic repair would need a heuristic (for example, drop keys whose value is zero AND equal to a full-dump signature). It was not done.

## Remaining risks
- The fill reads viper. If a key the file lacks is already provided with the same value by an overlay or an environment variable, setting it to that value is not persisted, because it is not a change from the effective value. Runtime overrides and telemetry are excluded, as described above.
- viper is read without a lock, which is the same unsynchronised global pattern as the existing HTTP-handler `Update*` calls.
- JSON files: an `omitempty` field set to zero is deleted from the file, so its default wins. This is identical to the previous JSON behaviour.

Related: [[pando/changes/ipc_other_entrypoints_p4.md]], [[pando/plans/cronjob_feature_plan.md]], [[pando/changes/caveman_phase2_3_config_and_injection.md]], [[plans/age-config-encryption.md]], [[pando/plans/mcp_server_ipc_bootstrap.md]]
