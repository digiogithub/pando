---
created_at: 2026-09-10T21:09:02.516759689Z
updated_at: 2026-09-10T21:09:02.516759689Z
tags:
    - feature
    - telemetry
    - cli
    - pando_setup
    - docs
    - tests
---
# Feature: opt-in remote logs/telemetry to Better Stack — Phase 6 (CLI, agent tool, docs, E2E)

Plan: [[remote_telemetry_betterstack_plan]]. Sibling entries: [[remote_telemetry_betterstack_phase0_1]]
(Phase 0/1: build-time secret + config model), [[remote_telemetry_betterstack]] (Phase 2: redact +
shipper), [[remote_telemetry_betterstack_phase3]] (Phase 3: logging/app lifecycle),
[[remote_telemetry_betterstack_phase4]] (Phase 4: REST/WebUI), [[remote_telemetry_betterstack_phase5_tui]]
(Phase 5: TUI). This entry covers **Phase 6**: CLI subcommand, `pando_setup` tool exposure, docs,
issue template, and Python E2E tests. Implemented concurrently with a "review-fix" pass touching
`internal/telemetry`/`internal/redact`/`internal/logging`/`internal/config`/`internal/app`/
`internal/api`/`internal/llm/agent`/`web-ui` — none of those packages' production code was
touched by this phase except the two `internal/llm/tools/pando_setup.go` additions below.

## 1. CLI: `pando telemetry` (new `cmd/telemetry.go`)

Cobra subcommand registered on `rootCmd`, loading config the lightweight way other headless
subcommands do (`config.Load(cwd, false)` via `os.Getwd()`, mirroring `cmd/cronjob.go`'s
`runCronJobList`) — no TUI/app/DB startup. `SilenceUsage` applied recursively via the existing
`silenceUsage()` helper from `cmd/design.go`.

Actions:
- `pando telemetry` / `pando telemetry status [--json]` — prints available/enabled/debug
  id/min level/endpoint host (never the token; endpoint host extracted via `net/url.Parse`).
- `pando telemetry enable` — refuses with a clear message when `!telemetry.Available()`;
  otherwise `config.UpdateTelemetry(true)` and prints the grouped debug id.
- `pando telemetry disable` — `config.UpdateTelemetry(false)`, keeps the debug id.
- `pando telemetry id` — prints only the grouped debug id; non-zero exit + empty stdout when
  none exists yet (script-friendly).
- `pando telemetry regenerate` — refused without a token (mirrors the TUI's
  `regenerateTelemetryID`); `config.RegenerateTelemetryID()`.
- `pando telemetry level <debug|info|warn|error>` — `config.UpdateTelemetryMinLevel`.

`--json` is a persistent flag on the parent command so both `pando telemetry --json` and
`pando telemetry status --json` work.

## 2. `pando_setup` tool: new `telemetry` command

`internal/llm/tools/pando_setup.go`: added a `telemetry` entry to `setupCommands()` (after
`run`) plus `runSetupTelemetry`/`renderSetupTelemetryStatus`/`yesNoSetup`. Unlike `model`
(session-scoped, goes through `SetupBridge`), telemetry is a GLOBAL setting, so — same pattern
as the existing `config`/`providers`/`models` commands — it calls straight into `config.Get()`/
`config.UpdateTelemetry`/`config.RegenerateTelemetryID`/`config.UpdateTelemetryMinLevel` and
`internal/telemetry.Available()`/`FormatDebugID`, no bridge needed.

Usage: `telemetry [status|enable|disable|regenerate|level <level>]`. `enable`/`regenerate`
refuse with an error when the build has no ingest token, matching the CLI and TUI. New import:
`github.com/digiogithub/pando/internal/telemetry`.

There was no pre-existing generic "settings get/set key" mechanism in `pando_setup` to reuse
(`config` there is read-only; the only precedent for a gettable/settable value was the
session-scoped `model` command) — this new `telemetry` command follows that same
status/action-verb shape rather than inventing a `key=value` mechanism.

Tests: new `internal/llm/tools/pando_setup_telemetry_test.go` (9 tests: status default,
enable refused without token, enable generates+shows a 16-digit id, disable keeps it, re-enable
reuses it, regenerate changes it (and is refused without a token), level updates+validates,
unknown action errors, `--help`).

## 3. Docs

- `README.md`: new `### Remote diagnostics (telemetry)` subsection under `## Configuration`
  (between MCP Server Authentication and Language Servers): off-by-default statement, the 3
  ways to enable (TUI/WebUI/CLI/agent tool), **exactly which fields are sent** per record (`dt`,
  `level`, `message`, `debug_id`, `app{version,variant,os,arch,go,mode}`, `source`,
  `session_id`, `attrs`, `dropped`), what's redacted (secret-suffixed keys → `[REDACTED]`,
  secret-shaped values scrubbed, `$HOME` → `~`), what's never sent (full session
  request/response dumps, file contents), the `pando telemetry` command block, how to share the
  debug ID in an issue, and a **Build notes** paragraph covering the release-binary vs.
  source-build (`PANDO_BETTERSTACK_TOKEN`) vs. self-hosted (`PANDO_TELEMETRY_TOKEN`/
  `PANDO_TELEMETRY_ENDPOINT`) cases.
  - Per the concurrent review-fix pass (told to me mid-task by the coordinator), the docs also
    state: (1) the shipped `debug_id` is the grouped/dashed display form; (2) setting
    `PANDO_TELEMETRY_ENDPOINT` requires `PANDO_TELEMETRY_TOKEN` explicitly (the build-time
    ldflags token only applies to the default Better Stack endpoint) and plain `http://` is only
    accepted for a loopback endpoint; (3) at the default `info` level, per-turn tool activity
    ships as a short summary rather than full input/output, and debug-level records need the
    app's own `--debug`/`Debug` flag too.
  - Also added a `pando telemetry status`/`enable` example to the `## Usage` block and a
    `telemetry [...]` row to the `pando_setup` command table (`## Agent Self-Service`).
- `.github/ISSUE_TEMPLATE/bug_report.yml` (new — no `ISSUE_TEMPLATE` directory existed before):
  YAML issue-form bug report with a required description, optional reproduction steps, optional
  log output, an **optional "Debug ID"** input field (explains where to find it: Settings →
  General → Remote Telemetry, or `pando telemetry id`), required version, install-method
  dropdown, required OS dropdown, and a free-text "anything else" field.

## 4. Python E2E tests: `tests/test_telemetry_cli.py` (new)

Follows the repo's existing `tests/test_cronjob_cli.py` / `tests/test_desktop_controller_mcp_e2e.py`
convention (unittest, builds a real binary via `go build -o <path> .` once in `setUpModule`,
drives it via `subprocess`). Deviates from those two precedents in one way per this task's
explicit instruction: **skips gracefully** (`unittest.SkipTest`) instead of raising when `go` is
not on PATH (verified by monkeypatching `shutil.which`).

Covers exactly the 4 scenarios in the task brief:
- a) `status --json`/`status` with no token → `available: no`, `enabled: false`.
- b) With `PANDO_TELEMETRY_TOKEN=test` + isolated `$HOME`/`$XDG_CONFIG_HOME`: `enable` prints a
  19-char grouped id; `id` prints the same; the write lands under the isolated HOME (a
  `.pando.*` file appears there) and never in the project cwd; `disable`→`enable` keeps the id;
  `regenerate` changes it; `enable`/`id` fail with non-zero exit and no stdout when refused/unset.
- c) Shipping: a local `http.server`-based mock ingest endpoint (own `_MockIngestServer` class,
  ephemeral loopback port) captures POST bodies + `Authorization`. `PANDO_TELEMETRY_ENDPOINT`
  points at it; `pando serve --host 127.0.0.1 --port <free>` is started as a real subprocess
  (chosen per the task's own suggestion: it calls `app.New`/`Shutdown` via `api.NewServer`,
  needs no LLM provider, and shuts down cleanly), polled for its "listening on" stdout line (with
  a timeout fallback), then sent `SIGINT` and waited on (with a `SIGKILL` fallback + a 6s-watchdog
  aware timeout). Asserts: `Authorization: Bearer test` on every request; body is a JSON array;
  records carry `dt`; `debug_id` matches the enabled id (compared with dashes stripped from both
  sides, so it accepts either the grouped or raw form — the coordinator's mid-task note said the
  shipped form is now the grouped/dashed one, confirmed by the actual test run); a non-empty
  `app.version`; at least one record mentioning a telemetry/shutdown lifecycle event (message
  text or an `attrs.event` field, matched loosely since the exact wording belongs to
  `internal/app`/`internal/logging`); the literal token string `"test"` and the isolated `$HOME`
  absolute path never appear in any raw shipped body.
- d) Same `pando serve` harness with telemetry left disabled (but a token present, so the build
  is "available") → the mock receives zero requests.

Runtime: ~10s for the whole module (well under the 60s budget), including the two `pando serve`
runs. Run with `python3 -m pytest tests/test_telemetry_cli.py -v` or
`python3 -m unittest tests.test_telemetry_cli -v` (pytest was not installed in this sandbox, so
verification here used `unittest` — both are documented in the file's own docstring, matching
the other CLI/E2E test files' convention).

## 5. Verification

- `go build ./...` — clean.
- `go vet ./cmd/... ./internal/llm/tools` — clean.
- `go test ./internal/llm/tools ./cmd/... -count=1` — `ok` (tools 61s incl. the 61s package as a
  whole with its many pre-existing slow subtests, cmd 0.6s).
- `gofmt -l` on every new/touched file — clean.
- `python3 -m unittest tests.test_telemetry_cli -v` — 7/7 pass, ~10s; confirmed graceful skip
  (0 tests run, reported as skipped) when `go` is made to look unavailable.
- Re-verified `go vet`/`go test ./internal/llm/tools` a second time after switching the two new
  test helpers (see below) from a `config.Reload()`-based reset to `config.ResetForTests()` —
  still clean.

## Small edits to test-only code (not production Phase 0-5 code)

While writing `cmd/telemetry_test.go`, a real test-isolation bug surfaced and was root-caused:
`internal/config.Load` is a **process-wide singleton loader** — `if cfg != nil { return cfg, nil
}` at the top of `Load()` (internal/config/config.go:1766) — so once any test in the same test
binary has loaded a config, every later `Load(...)` call anywhere in that binary silently
**ignores its arguments** and returns the already-cached config. My CLI test calls several
`runTelemetry*` helpers back to back, each independently calling `loadTelemetryConfig()` →
`config.Load(os.Getwd(), false)`, so a prior test (`TestDesignSystemExtractRejectsUnknownSource`,
which loads a real, non-isolated config from this real repo checkout) left a **real, non-nil**
package-level config in place — including, on this sandbox, telemetry actually enabled for real
with a real debug id — which then silently won every subsequent same-process `Load()` call,
including my own test helper's, because a bare `viper.Reset()` does **not** clear that cached
`cfg` pointer. Root cause confirmed by direct instrumentation (dumping `config.Get().Telemetry`
before/after the helper).

Fix: `cmd/telemetry_test.go`'s `withTelemetryCLIConfig` and
`internal/llm/tools/pando_setup_telemetry_test.go`'s `withTelemetrySetupConfig` both now call
`config.ResetForTests()` (an existing, exported, `internal/config`-provided test helper that
does `cfg = nil; viper.Reset(); ClearOverlayProviders(); ClearRuntimeOverrides()`) instead of a
bare `viper.Reset()` (my first draft) or the `config.Reload()`-then-`viper.Reset()` pattern
copied from Phase 4's `internal/api/handlers_settings_telemetry_test.go` (which happens to work
there only because that file calls `config.Load()` exactly once per test, so the
singleton-short-circuit on any *subsequent* call inside the same test never bites it — my CLI
test's own `runTelemetry*` calls each recurse into `config.Load()` again, which does hit it).
This is a test-only fix in files this phase owns; no production Phase 0-5 file was touched. Also
added an `os.Chdir` into an isolated project dir in the CLI test helper for extra realism/safety
(matters less now that the singleton short-circuit means later `Load()` calls just return the
already-loaded config, but keeps `os.Getwd()` consistent with what a real CLI invocation from a
fixed directory would see).

## Open items / deviations

- No `pando-schema.json` regeneration attempted (telemetry was already, per Phase 0/1's own
  note, deliberately excluded from that hand-curated generator — consistent, not a regression).
- The E2E shipping test's lifecycle-message assertion is intentionally loose (message text OR an
  `attrs.event` field, case-insensitive "shutdown"/"telemetry") specifically so it keeps passing
  through the concurrent review-fix pass's changes to `internal/app`/`internal/logging` wording,
  per the coordinator's mid-task instruction to keep the E2E tests compatible with that parallel
  work.
- Did not add a `.github/ISSUE_TEMPLATE/config.yml` (template chooser) — out of the requested
  scope (only asked to add the Debug ID field to a bug report template; none existed, so one was
  created).

Related: [[remote_telemetry_betterstack_plan]], [[remote_telemetry_betterstack_phase0_1]],
[[remote_telemetry_betterstack]], [[remote_telemetry_betterstack_phase3]],
[[remote_telemetry_betterstack_phase4]], [[remote_telemetry_betterstack_phase5_tui]],
[[pando_repo_pitfalls]], [[fix_config_tests_home_leakage_and_template_drift]]