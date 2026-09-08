---
created_at: 2026-09-10T20:28:09.623081496Z
updated_at: 2026-09-10T20:28:09.623081496Z
tags:
    - feature
    - telemetry
    - config
    - build
---
# Feature: opt-in remote logs/telemetry to Better Stack — Phase 0 + Phase 1 implementation log

Plan: [[remote_telemetry_betterstack_plan]] (`pando/plans/remote_telemetry_betterstack_plan.md`).
Sibling entry: [[remote_telemetry_betterstack]] (`pando/features/remote_telemetry_betterstack.md`) —
covers **Phase 2** (redaction package + `internal/telemetry` Record/Options/Shipper), implemented
concurrently by another agent. This entry covers **Phase 0** (build-time secret plumbing) and
**Phase 1** (config model + global-only persistence), implemented together in one pass.

## Phase 0: build-time secret plumbing

### What changed
- **New `internal/telemetry/build.go`**: unexported ldflag-settable vars `sourceToken` (default
  `""`) and `ingestHost` (default `s2751484.us-west-2a.betterstackdata.com`), plus exported
  `Token() string` (env `PANDO_TELEMETRY_TOKEN` overrides `sourceToken`), `Endpoint() string`
  (env `PANDO_TELEMETRY_ENDPOINT` overrides `"https://"+ingestHost`, allowing plain `http://` for
  a local mock), and `Available() bool` (`Token() != ""`).
- **New `internal/telemetry/debugid.go`**: `NewDebugID() (string, error)` — 16 decimal digits via
  `crypto/rand.Int` (unbiased by construction, no manual rejection sampling needed), first digit
  drawn from `[1,9]`; `ValidDebugID(id string) bool` — exactly 16 digits; `FormatDebugID(id string)
  string` — groups as `1234-5678-9012-3456`, returns the input unchanged if not exactly 16 digits.
- **`Makefile`**: added `TELEMETRY_LDFLAGS` (only set via `ifneq` when `PANDO_BETTERSTACK_TOKEN`
  is non-empty), appended to `LDFLAGS` (~line 29-38, used by `build`/`release-*`) and to the
  `build-fast` target's ldflags expression (~line 129-131, previously only `VARIANT_LDFLAGS`).
  Documented the local-dev command (`kvage get pando_betterstack_token`, name only, never the
  value) in a comment.
- **`.goreleaser.yml`**: appended
  `-X github.com/digiogithub/pando/internal/telemetry.sourceToken={{ envOrDefault
  "PANDO_BETTERSTACK_TOKEN" "" }}` to the single `ldflags` entry — `envOrDefault` never errors when
  the env var is absent, unlike `.Env.X`.
- **`.github/workflows/release.yml`**: added `env: PANDO_BETTERSTACK_TOKEN: ${{
  secrets.PANDO_BETTERSTACK_TOKEN }}` to the "Build release archives" step (linux/windows,
  `make release-linux-amd64 release-linux-arm64 release-windows-amd64`) and the "Build and sign the
  CLI release archives" step (darwin, `make release-darwin-arm64 release-darwin-amd64`). Also added
  a short doc paragraph next to the existing `MACOS_SIGNING_BUNDLE` note documenting the new secret.
- **`.github/workflows/desktop-build.yml`**: added `env: PANDO_BETTERSTACK_TOKEN: ${{
  startsWith(github.ref, 'refs/tags/') && secrets.PANDO_BETTERSTACK_TOKEN || '' }}` to the "Build
  desktop app" step, so a `workflow_dispatch` dev build never carries the real secret. **Deviation/
  open issue**: `make desktop-build` invokes `wails build` directly and does not currently forward
  `LDFLAGS`/`TELEMETRY_LDFLAGS` to it at all (unlike `make build`/`release-*`), so this env var is
  forward-looking plumbing only — wiring `wails build -ldflags ...` (or a `wails.json` `ldflags`
  entry) to actually consume it is out of scope for Phase 0 and left as a follow-up.
- **`.github/workflows/build-matrix.yml`**: intentionally left untouched (PR builds must not carry
  the token). No `digiogithub/ci-actions` composite action wraps `go build` anywhere in these
  workflows, so no other file needed the env var.

### Repo secret
`PANDO_BETTERSTACK_TOKEN` must still be created manually in the GitHub repo settings by someone
with admin access — this agent has no ability to do that and never read/printed/fetched any token
value per the hard constraint.

### Verification
- `go build ./internal/telemetry/...`, `go vet ./internal/telemetry/...`,
  `go test ./internal/telemetry/...` — all clean/PASS in isolation, and again after the concurrent
  Phase 2 agent added `options.go`/`record.go`/`shipper.go` to the same package.
- `make -n build` vs `PANDO_BETTERSTACK_TOKEN=dummy make -n build` — ldflags differ only by the
  added `-X .../telemetry.sourceToken=dummy` (dummy value only, confirmed never the real secret).
- `make -n build-fast` vs `PANDO_BETTERSTACK_TOKEN=dummy make -n build-fast` — same, confirms the
  empty-token case adds no `-ldflags` at all (`go build   -o pando .`).
- `goreleaser check` — valid (only pre-existing, unrelated deprecation warnings about
  `snapshot.name_template`/`archives.format`).
- `goreleaser build --single-target --snapshot --skip=validate` — template rendered correctly and
  reached the Go compile stage; failed there on a pre-existing, unrelated `go-tree-sitter`/
  `CGO_ENABLED=0` build-constraint issue in that dependency, not caused by this change.
- `actionlint .github/workflows/release.yml .github/workflows/desktop-build.yml
  .github/workflows/build-matrix.yml` — no findings.

## Phase 1: config model + global-only persistence

### What changed
- **New `internal/config/telemetry.go`**:
  - `TelemetryConfig struct { Enabled bool; DebugID string; MinLevel string }` with
    `toml:"Enabled"/"DebugID"/"MinLevel"` tags matching the existing `OpenLitConfig` casing
    convention, and `json:"enabled"/"debugId,omitempty"/"minLevel"`.
  - Constants `TelemetryLevelDebug/Info/Warn/Error`.
  - `normalizeTelemetryDefaults()` — lowercases `MinLevel`, defaults empty to `"info"`; called from
    `applyDefaultValues()`.
  - `validateTelemetryConfig(t TelemetryConfig) error` — `DebugID` must be empty or
    `telemetry.ValidDebugID`; `MinLevel` must be `""` (tolerated — means "not yet normalized",
    needed so `Validate()` called directly on a hand-built `Config{}` in pre-existing unit tests
    doesn't regress) or one of the 4 allowed levels. Called from `Validate()`.
  - `TelemetryDebugIDDisplay() string` — `telemetry.FormatDebugID(cfg.Telemetry.DebugID)`.
  - `UpdateTelemetry(enabled bool) (debugID string, err error)` — generates a new ID via
    `telemetry.NewDebugID()` only when enabling for the first time (`DebugID == ""`); persists via
    `updateGlobalCfgFile`; rolls back the in-memory value on error; template copied from
    `UpdateDebug`/`UpdateOpenLit`.
  - `RegenerateTelemetryID() (string, error)` — always issues a fresh id, independent of `Enabled`.
  - `UpdateTelemetryMinLevel(level string) error` — trims/lowercases, rejects empty explicitly
    (distinct from `validateTelemetryConfig`'s tolerance of empty, since an explicit "set the level
    to nothing" call is a caller bug, not a not-yet-normalized value).
- **`internal/config/config.go`**:
  - Added `Telemetry TelemetryConfig` field to `Config` (`json:"telemetry,omitempty"
    toml:"Telemetry"`), right after `Design`.
  - `setDefaults`: `viper.SetDefault("telemetry.enabled", false)` and
    `viper.SetDefault("telemetry.minLevel", "info")`.
  - `applyDefaultValues`: added `normalizeTelemetryDefaults()` to the list of `normalize*Defaults`
    calls.
  - `Validate`: added `validateTelemetryConfig(cfg.Telemetry)` check.
  - **Global-only enforcement in `Load`**: right after `readGlobalConfig()` succeeds (before
    `mergeLocalConfig`/`applyOverlayProviders` layer project-local or overlay values into the same
    package-level viper instance), a snapshot `var globalTelemetry TelemetryConfig;
    viper.UnmarshalKey("telemetry", &globalTelemetry)` is taken. Later, right before
    `applyDefaultValues()` (after the main `viper.Unmarshal(cfg)` + provider migration/decrypt
    steps), `cfg.Telemetry = globalTelemetry` re-pins it, discarding whatever a project-local
    `.pando.toml`/`.pando.json` or an overlay provider tried to set. This is the **only** mechanism
    guarding telemetry; it is a snapshot-and-reapply around the *existing* merge pipeline rather
    than a new merge-time filter, so it needed no change to `mergeLocalConfig`/
    `applyOverlayProviders` themselves.
  - **`updateCfgFile` refactor**: the original body is now `updateConfigFileAt(resolvePath func()
    (string, error), updateCfg func(*Config)) error`, taking the path resolver as a parameter.
    `updateCfgFile` = `updateConfigFileAt(ResolveConfigFilePath, ...)` (local-preferring, unchanged
    behavior — verified byte-for-byte identical body). New `updateGlobalCfgFile` =
    `updateConfigFileAt(resolveGlobalConfigFilePath, ...)`.
  - **New `resolveGlobalConfigFilePath()`**: extracted from the tail of `ResolveConfigFilePath`
    (everything after the "prefer local config" step): `viper.ConfigFileUsed()` (which — verified
    by reading `mergeLocalConfig` — always names the global file after `Load`, never a
    project-local one, since the local file is merged into the shared viper instance via a
    throwaway `viper.New()` that never touches `ConfigFileUsed`), then `$HOME/.pando.toml`/`.json`,
    then the legacy `~/.config/pando/config.*` path, else `""`. `ResolveConfigFilePath` now just
    checks `FindLocalConfigFile` first and delegates the rest to this new function — no behavior
    change for existing callers (confirmed by the full existing `internal/config` test suite
    staying green).
- Regenerated `pando-schema.json` (`go run cmd/schema/main.go > pando-schema.json`) to check for a
  drift: the diff was 100% non-deterministic Go map-iteration reordering of model enums / MCP field
  ordering, unrelated to telemetry. **Deliberately did not commit the regenerated file** — the
  hand-curated schema generator has no entry for `OpenLit`/`InternalTools`/`Remembrances`/etc.
  either (confirmed precedent in `.kb/pando/changes/uiauto_tools_phase1.md`: "pando-schema.json has
  no InternalTools/Browser section at all — checked, nothing to add"), so `telemetry` is
  consistently omitted too; regenerating would only add ordering-noise to the diff.
- New tests: `internal/telemetry/build_test.go`, `internal/telemetry/debugid_test.go`,
  `internal/config/telemetry_test.go` (10 tests: viper defaults; `UpdateTelemetry` writes the
  GLOBAL file even with an active project-local config, and leaves that local file's *own*
  post-`Load` content byte-identical — the baseline had to be taken *after* `Load`, not before,
  because `Load` itself can already rewrite whichever config file `ResolveConfigFilePath` picks via
  the pre-existing, unrelated `ensureEvaluatorDefaultModel` auto-persist; debug id stable across
  disable/enable; `RegenerateTelemetryID` changes and persists it; a project-local `[Telemetry]`
  section is fully ignored; `UpdateTelemetryMinLevel` validates + rolls back on failure;
  `validateTelemetryConfig` table test; `Load` rejects an invalid `MinLevel` found in the *global*
  file but tolerates (ignores) one in a *local* file).

### Public API added
`internal/telemetry`: `Token() string`, `Endpoint() string`, `Available() bool`, `NewDebugID()
(string, error)`, `ValidDebugID(string) bool`, `FormatDebugID(string) string`.
`internal/config`: `TelemetryConfig` struct + `TelemetryLevelDebug/Info/Warn/Error` constants,
`TelemetryDebugIDDisplay() string`, `UpdateTelemetry(bool) (string, error)`,
`RegenerateTelemetryID() (string, error)`, `UpdateTelemetryMinLevel(string) error`.

### Package dependency rule
`internal/telemetry` imports only stdlib (confirmed: `build.go` imports `os`; `debugid.go` imports
`crypto/rand`, `fmt`, `math/big`, `strings`) — no `internal/config`, no `internal/logging`.
`internal/config/telemetry.go` imports `internal/telemetry`. No cycle.

### Verification
- `go build ./...` — clean.
- `go vet ./internal/telemetry ./internal/config` and `go vet ./...` — clean.
- `go test ./internal/telemetry/... ./internal/config/... -count=1` — both `ok`.
- `go test ./internal/llm/agent ./internal/api` (CLAUDE.md's verified command) — `internal/api`
  PASS; `internal/llm/agent` has 4 pre-existing failures in files this change never touched
  (`caveman_session_test.go`, `extension_tools_test.go`); root-caused
  `TestSetAndGetCavemanMode`'s failure to the developer's real `~/.pando.toml` having
  `[Caveman] DefaultMode = 'lite'` — the test never isolates `HOME` (same class of bug as
  [[fix_config_tests_home_leakage_and_template_drift]]/[[pando_repo_pitfalls]]), confirmed
  unrelated to Phase 0/1 and not investigated further (out of this task's scope).

### Deviations / open issues
1. `desktop-build.yml`'s new `PANDO_BETTERSTACK_TOKEN` env var is currently a no-op because
   `make desktop-build` never passes ldflags to `wails build` (see Phase 0 section above) — needs a
   Makefile/`wails.json` change in a later phase to actually take effect.
2. `validateTelemetryConfig` tolerates an empty `MinLevel` (treats it as "not yet normalized")
   rather than hard-rejecting it as the plan's validation bullet literally reads, because several
   pre-existing `internal/config` tests (`TestValidateAllowsOllamaWithoutAPIKey`,
   `TestValidateDoesNotWriteMissingAPIKeyWarningToStdout`,
   `TestValidateRejectsInvalidGoalDuration`, `TestValidateDisablesEvaluatorWithoutModel`,
   `TestValidateAgentWithoutProviderDoesNotError`) construct a `Config{}` by hand and call
   `Validate()` directly, bypassing `applyDefaultValues`/`normalizeTelemetryDefaults`. The explicit
   user-facing setter `UpdateTelemetryMinLevel("")` still rejects empty directly, so no real
   user-visible laxity was introduced.
3. `pando-schema.json` was not regenerated/committed (see above) — telemetry has no entry there,
   consistent with several other nested config sections.
4. Repo secret `PANDO_BETTERSTACK_TOKEN` still needs to be created manually in GitHub by someone
   with repo admin access.

Related: [[remote_telemetry_betterstack_plan]], [[remote_telemetry_betterstack]],
[[fix_config_tests_home_leakage_and_template_drift]], [[fix_supported_models_copy_on_write]]