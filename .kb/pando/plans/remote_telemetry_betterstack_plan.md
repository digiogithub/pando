---
created_at: 2026-09-10T19:55:02.861321932Z
updated_at: 2026-09-10T19:55:02.861321932Z
tags:
    - plan
    - telemetry
    - logging
    - settings
---
# Plan: opt-in remote logs/telemetry to Better Stack (2026-09-10)

Status: PLANNED (not started).

## Goal
Pando OSS can ship its logs and telemetry as JSON to a remote Better Stack source so users with
problems can share diagnostics. **Off by default.** Toggled from the General settings screen in
TUI and WebUI (Desktop reuses the WebUI). On first enable, an anonymous numeric **debug ID** is
generated, persisted in the global Pando config, attached to every shipped record, and displayed
next to the toggle. WebUI has a copy-to-clipboard button; TUI has a copy action.

## Constraints and key decisions
- **Secrets:** the Better Stack source token is NEVER committed and NEVER stored in user config.
  - It is injected at build time via ldflags.
  - Local dev builds read it with `kvage get pando_betterstack_token` into env `PANDO_BETTERSTACK_TOKEN`.
  - CI reads it from a GitHub secret with the same name.
- **Ingest host:** `s2751484.us-west-2a.betterstackdata.com`. It is not secret, so it is a
  package default constant, overridable via ldflags or env for self-hosters.
- **Protocol** (https://betterstack.com/docs/logs/ingesting-data/http/logs.md):
  - Request: `POST https://$INGESTING_HOST` with headers `Authorization: Bearer $SOURCE_TOKEN` and
    `Content-Type: application/json`.
  - Body: a single object, or a JSON array for batches.
  - Timestamp field: `dt` (RFC3339 or unix).
  - Limits: 10 MiB per request, keep each record under 100 KiB.
  - Responses: 202 ok; 402 quota exceeded; 403 bad token; 406 bad payload; 413 too large.
- **Builds without a token** (`go install`, `make build-fast`, forks) show the setting as
  "unavailable in this build" with the toggle disabled. An env override
  (`PANDO_TELEMETRY_TOKEN`, `PANDO_TELEMETRY_ENDPOINT`) is allowed for dev and self-hosting.
- **Debug ID format:** 16 random digits (crypto/rand), displayed grouped as `1234-5678-9012-3456`.
  - Numeric because it is easy to dictate and paste into issues. ~53 bits of entropy is enough for anonymous correlation.
  - Stored without dashes.
  - (Alternative: standard UUIDv4 via `github.com/google/uuid`, already in go.mod.)
- **ID lifecycle:** generated on first enable. It is kept on disable, so re-enabling reuses the same ID. A "Regenerate ID" action is provided.
- **Persistence is GLOBAL only** (`~/.pando` / XDG).
  - `updateCfgFile` → `ResolveConfigFilePath()` prefers the project `.toml`, so a new global-only writer is required.
  - A project/overlay config must not be able to enable telemetry.
- **Anonymity:**
  - No username/hostname.
  - `$HOME` in paths is rewritten to `~`.
  - Working dir is sent as a short hash only.
  - Secrets are redacted (see P2).
  - Per-session request/response dumps (`logging.MessageDir` / `Write*`) are NEVER shipped.
- **Logging never blocks:** bounded queue, drop on full, drop counter reported in the next batch.
- **Security note:** a token embedded in a public binary can be extracted. Better Stack source
  tokens are ingest-only (no read access). The risk is spam or quota abuse.
  - Mitigations: spending cap/rate limit on the source; rotate the token per release if abused.
  - Optional future step: a thin relay endpoint.

## Existing code anchors (from exploration)
- **Config:**
  - `internal/config/config.go` has `Config` struct :1004, `setDefaults` :2079, and slog handler setup in `Load` :1841-1907.
  - Also: `updateCfgFile` :3745, `ResolveConfigFilePath` :3853, `UpdateDebug` :4519 (template), `UpdateGeneral` :5119.
  - `Reload` :3907 → `config.Bus` (`eventbus.go`).
  - `GlobalConfigDir()` is in `global_projects.go:39`.
  - Schema: `cmd/schema/main.go` → `pando-schema.json`.
- **Logging:**
  - `internal/logging` is thin on slog. `writer.go` = logfmt → pubsub; `RecoverPanic` is `logger.go:68`.
  - The handler is chosen in 3 branches: LogFile, `PANDO_DEV_DEBUG`, or default pubsub writer.
  - `Reload()` calls `slog.SetDefault` again, so the remote sink must be wired inside those branches and survive reloads.
- **Lifecycle precedent:** `observability.Init(cfg.OpenLit, ...)` in `internal/app/app.go:322` with a shutdown func.
- **Build:**
  - Makefile `LDFLAGS` :29, used by `build` :121 and the release macro :196. `build-fast` :129 has no LDFLAGS.
  - `.goreleaser.yml:15`.
  - Workflows: `release.yml` make steps :93/:248; `build-matrix.yml` :51; `desktop-build.yml` :68.
  - `internal/version` has `Version`/`Variant`.
- **TUI:**
  - `internal/tui/page/settings.go`: `buildGeneralSection` :1054, Debug toggle :1146, `persistSetting` switch :3546.
  - Fields in `components/settings/field.go` (FieldToggle/FieldText ReadOnly/FieldAction/Hint).
  - Clipboard helper `copyToClipboard` (atotto + OSC52) is unexported in `tui/components/chat/list.go:47`.
- **WebUI:**
  - `web-ui/src/components/settings/GeneralSettings.tsx` (Debug toggle :175), `Toggle` in `shared/FormInput.tsx:158`.
  - Store `packages/pando-client/src/stores/settingsStore.ts` (GET/PUT `/api/v1/settings`), type `SettingsConfig` in `types/index.ts:541`.
  - `CopyButton` in `chat/MessageBubble.tsx:553`.
  - i18n `web-ui/src/i18n/locales/*.json`.
- **API:** `internal/api/handlers_settings.go`: `SettingsResponse` :17, `SettingsUpdateRequest` :86 (pointer fields), `handlePutSettings` :293.
- **Redaction to generalize:** `pando_setup.go` `isSetupSecretKey`/`redactSetupValue`/`maskSetupSecret`, and `debug_transport.go` `sanitizeHeaders`.

## Phase 0: Build-time secret plumbing
1. New package `internal/telemetry` with ldflag-settable vars:
   - `sourceToken` (default empty)
   - `ingestHost` (default `s2751484.us-west-2a.betterstackdata.com`)
2. Add `Available() bool`. It is true when the token is non-empty, either from ldflags or the `PANDO_TELEMETRY_TOKEN` env.
3. Makefile:
   - `TELEMETRY_LDFLAGS := -X github.com/digiogithub/pando/internal/telemetry.sourceToken=$(PANDO_BETTERSTACK_TOKEN)`, appended to `LDFLAGS`. Only add it when the variable is non-empty, so no empty `-X` is noise.
   - Also add it to the `build-fast` / enterprise target.
4. Add the same flag to `.goreleaser.yml` ldflags.
5. CI:
   - Create repo secret `PANDO_BETTERSTACK_TOKEN`.
   - Pass it as `env:` on the make steps in `release.yml` (linux/windows :93, darwin :248) and `desktop-build.yml`.
   - `build-matrix.yml` (PR builds) stays WITHOUT the token.
   - Check the shared `digiogithub/ci-actions@v1` actions if they wrap the build.
6. Document local use: `PANDO_BETTERSTACK_TOKEN=$(kvage get pando_betterstack_token) make build`.

Verify: `go tool nm`, or `strings`, on the built binary; `telemetry.Available()` is true with the token and false without it.

## Phase 1: Config model and global persistence
1. Add `TelemetryConfig` to `Config`, with `json:"telemetry,omitempty" toml:"Telemetry"`:
   - `Enabled bool` (default false)
   - `DebugID string`
   - `MinLevel string` (default `info`; `debug` records only when `cfg.Debug` is also on)
2. Add defaults in `setDefaults`, and normalize/validate in `applyDefaultValues` / `Validate`. Validation: DebugID is 16 digits or empty; MinLevel is in the allowed set.
3. Global-only writer `updateGlobalCfgFile(func(*Config))`:
   - Always targets the global config file, creating it if missing.
   - Keeps format, lock checks and encryption consistent with `updateCfgFile`.
4. Ignore `telemetry.*` coming from project-local or overlay configs, since only global is authoritative. Do this at merge time or by re-reading the global value.
5. Add API functions:
   - `config.UpdateTelemetry(enabled bool) (debugID string, err error)`: generates the ID on first enable, persists, rolls back on error.
   - `config.RegenerateTelemetryID()`.
   - `config.TelemetryDebugIDDisplay()`, which returns the grouped format.
6. ID generator: `telemetry.NewDebugID()` using crypto/rand, 16 digits, first digit non-zero.
7. Regenerate `pando-schema.json`.

Tests:
- Enabling writes to global even when a project `.toml` exists.
- The ID is stable across disable/enable.
- Regenerate changes it.
- A project config cannot enable telemetry.
- Use `isolateGlobalConfig(t)` (see the config tests HOME leakage fix).

## Phase 2: Telemetry shipper and redaction (`internal/telemetry`)
1. `Record` payload:
   - `dt` (RFC3339Nano), `level`, `message`, `debug_id`.
   - `app{version, variant, os, arch, go, mode}`, where mode = tui|serve|desktop|acp|cli.
   - `source`, `session_id` (optional random UUID), `attrs` (map, redacted), `dropped` (count since last batch).
2. `Shipper`:
   - Bounded channel (1000). Background goroutine flushes every 5s or at 100 records / 1 MiB.
   - POSTs a JSON array. Gzip is optional: verify that Better Stack accepts `Content-Encoding: gzip` before enabling it.
   - Use a dedicated `http.Client` with a 10s timeout.
   - Retry 5xx and network errors with exponential backoff (max 3), then drop.
   - 403 or 402: stop shipping until restart or re-toggle, and log locally ONCE, marked so it is not re-shipped (avoid feedback loops).
   - 413 or 406: split the batch once, else drop.
   - `Start(ctx)`, `Stop(ctx)`: `Stop` flushes with a 2s deadline. `Enqueue` never blocks.
3. Redactor `internal/redact`, generalized from `pando_setup` + `debug_transport`:
   - Key-based masking on the suffixes key/token/secret/password/authorization/cookie/api_key.
   - Value patterns: `Bearer …`, `sk-…`, `ghp_/gho_/github_pat_…`, `AGE-SECRET-KEY-…`, `xox?-…`, JWT-like strings, URLs with userinfo.
   - Rewrite `$HOME` to `~`.
   - Truncate message/attrs to 8 KiB each.
   - Refactor `pando_setup.go` to use it (no behavior change).
4. Loop guard: the shipper's own logs carry an attr that the handler skips.

Tests: `httptest.Server` covering batching, 202 path, 403 disable, 5xx retry, drop-on-full counter, Stop flush, redaction table tests.

## Phase 3: Logging integration and lifecycle
1. `logging.NewTeeHandler(primary slog.Handler)`:
   - Forwards to primary, and to the process-global `telemetry.Sink` (atomic pointer) when that is set and the level is ≥ MinLevel.
   - Implements `WithAttrs`/`WithGroup` correctly.
2. Wrap all 3 handler branches in `config.Load` (:1868/:1897/:1903) with it. The sink is global and atomic, so the repeated `slog.SetDefault` in `Reload()` keeps shipping.
3. Lifecycle in `internal/app/app.go`, next to `observability.Init`:
   - `telemetry.Init(cfg.Telemetry, version, mode)` starts the shipper if `Enabled && Available()`.
   - The shutdown func is stored and called on app shutdown.
   - Wire the same into `cmd/serve`, desktop and `acp` entry points if they bypass `app.New`.
4. Hot toggle: subscribe to `config.Bus`. On change of `telemetry.*`, start or stop the shipper live, with no restart. Update the debug_id in records.
5. Events besides logs:
   - `telemetry.enabled` (start) and `app.shutdown`.
   - Panic: extend `logging.RecoverPanic` to enqueue a `panic` record with the redacted stack and flush synchronously (short deadline) before exit.
6. Also fix, in passing, `UpdateDebug` not changing the live slog level, via a `slog.LevelVar`. This matters so debug logs can be shipped on demand.

Tests: tee handler unit tests; reload keeps the sink; toggle start/stop via Bus.

## Phase 4: REST API and WebUI (covers Desktop)
1. `handlers_settings.go`:
   - `SettingsResponse` gets `telemetry_enabled`, `telemetry_debug_id` (display format), `telemetry_available`.
   - `SettingsUpdateRequest` gets `telemetry_enabled *bool` and `telemetry_regenerate_id *bool`.
   - `handlePutSettings` calls `config.UpdateTelemetry` / `RegenerateTelemetryID`.
2. pando-client: `SettingsConfig` type fields, and store round-trip in `settingsStore.ts`.
3. `GeneralSettings.tsx`, a new "Diagnostics / Remote telemetry" block:
   - `Toggle`, disabled with a hint when `!telemetry_available`.
   - Description of what is sent / not sent, with a link to docs.
   - When enabled (or the ID exists): monospace debug ID, a Copy button, and a "Regenerate" button.
4. Extract `CopyButton` from `MessageBubble.tsx` to `components/shared/CopyButton.tsx`. Add a fallback (`document.execCommand('copy')` via a hidden textarea) for the Wails webview / non-secure contexts (LAN http via external access toggle).
5. i18n keys in `en.json`, then translate the other locales (delegate to a cheap model).

Tests: Go handler test for GET/PUT; frontend build/typecheck; manual check in browser and Desktop.

## Phase 5: TUI
1. Move `copyToClipboard` (atotto + OSC52 + tmux) from `tui/components/chat/list.go` to a shared `internal/tui/util/clipboard` package. The chat keeps using it.
2. `buildGeneralSection`, next to the Debug toggle:
   - `telemetry.enabled`: FieldToggle, `Disabled` + Hint when unavailable.
   - `telemetry.debug_id`: FieldText `ReadOnly`, grouped display, and a Hint saying what is shared.
   - `telemetry.copy_id`: FieldAction, copies and shows a toast "Debug ID copied".
   - `telemetry.regenerate_id`: FieldAction.
3. `persistSetting` cases call the config API. The page refreshes via the existing `config.Bus` subscription, so the ID appears immediately after enabling.

Tests: settings page unit tests for toggle → ID field populated; manual TUI run.

## Phase 6: CLI, agent tool, docs, E2E verification
1. Optional `pando telemetry [status|enable|disable|id|regenerate]` subcommand for headless/ACP users. Also expose `telemetry.enabled` in the `pando_setup` tool (read ID, toggle).
2. Docs: README / docs privacy section listing exactly which fields are sent, how to enable it, and how to share the debug ID in an issue. Add a GitHub issue template field for the "Debug ID".
3. Python E2E tests in `tests/` (per repo convention):
   - Run Pando against a local mock ingest server (`PANDO_TELEMETRY_ENDPOINT`).
   - Assert the payload shape, `debug_id`, redaction, and nothing sent when disabled.
4. Real ingest check:
   - Build locally with the token from `kvage`.
   - Enable telemetry and verify records arrive in the Better Stack source, filtering by `debug_id`.
5. `go test ./internal/telemetry ./internal/redact ./internal/logging ./internal/config ./internal/api ./internal/tui/...`, then `go build ./...`, then the web-ui build.
6. KB: record the implementation summary per phase under `pando/features/remote_telemetry_betterstack.md`.

## Out of scope / later
- Product analytics (feature-usage counters).
- A relay proxy to hide the token.
- A "send last N in-memory logs now" button that backfills from `LogData`. Cheap follow-up after P3.
- Metrics/traces (OpenLit already covers OTLP).

Related: [[fix_config_tests_home_leakage]], [[feature_external_access_footer_toggle]], [[pando_setup_tool]]
