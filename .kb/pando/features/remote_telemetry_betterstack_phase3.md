---
created_at: 2026-09-10T20:44:37.650220509Z
updated_at: 2026-09-10T20:44:37.650220509Z
tags:
    - feature
    - telemetry
    - logging
    - app
---
Plan: [[remote_telemetry_betterstack_plan]] (`pando/plans/remote_telemetry_betterstack_plan.md`).
Sibling entries: [[remote_telemetry_betterstack_phase0_1]] (Phase 0+1: build-time secret
plumbing, config model/global persistence), [[remote_telemetry_betterstack]] (Phase 2:
`internal/redact` + `internal/telemetry` Record/Options/Shipper). This entry covers **Phase 3**:
logging integration and process lifecycle. Phases 4 (REST/WebUI) and 5 (TUI) were implemented
concurrently by other agents — see their own KB entries.

## What changed

### `internal/logging` (new files, tee handler + remote sink plumbing)
- **`remote_sink.go`**: `RemoteSink` interface (`Handle(ctx, t, level, msg, attrs)`, matching
  `*internal/telemetry.Shipper.Handle` exactly — no adapter needed), optional
  `RemoteSinkFlusher` interface (`Flush(ctx) error`, matching `Shipper.Flush` exactly),
  `SetRemoteSink(RemoteSink)` / `RemoteSinkActive() bool` (process-global `atomic.Pointer[RemoteSink]`,
  so Set/active-check/Handle need no lock), `FlushRemoteSink(ctx) error` (no-op unless the active
  sink implements `RemoteSinkFlusher`).
- **`tee_handler.go`**: `NewTeeHandler(primary slog.Handler) slog.Handler`. Forwards every record
  to `primary` unchanged (gated by `primary.Enabled`, since `teeHandler.Enabled` itself returns
  true whenever either the primary OR an active sink wants it — sink-side level filtering is left
  to the sink, e.g. `Shipper.Handle`'s `MinLevel` check). Correctly flattens `WithAttrs`/`WithGroup`
  state into dotted keys for the sink (group prefix baked into each attr's `Key` before handoff;
  `internal/telemetry.NewRecord`'s existing recursive group-flattening does the rest). A record
  carrying `telemetry.SkipAttrKey` (checked recursively through any nested groups) is never
  forwarded to the sink, only to primary — this is what lets the shipper's own diagnostic logs
  (e.g. the one-time "shipping disabled" warning) avoid a re-shipping feedback loop. Never
  blocks/panics: sink dispatch is a direct call into `Shipper.Handle`, which is itself
  non-blocking and panic-recovering.
- **`logger.go`** (`RecoverPanic`): now also, when a remote sink is active, ships a dedicated
  `event=panic` Error record (message, `panic_name`, and the full `debug.Stack()` output as a
  `stack` attr — redacted/truncated to 8 KiB automatically by `telemetry.NewRecord`, same as any
  other string attr) directly against the sink (bypassing slog, so it doesn't depend on the
  primary handler's own level gate), then calls `FlushRemoteSink` with a 2 s deadline — all before
  continuing the existing local-log/panic-file/cleanup behavior unchanged.
- Verified (Task 7 in the brief): `logging.MessageDir` and the `Write*`/`Append*Session*` helpers
  in `message.go` write full request/response/tool-result bodies directly to files via
  `os.OpenFile`, never through slog — only their *error* paths call `Error(...)`, and only with
  metadata (session id, file path, error), never body content. No `SkipAttrKey` tagging needed;
  nothing there was at risk of being shipped.

### `internal/config`
- **`config.go`**: added package-level `var slogLevel = new(slog.LevelVar)`. All 3 handler
  branches in `Load` (LogFile, `PANDO_DEV_DEBUG`, default `logging.NewWriter()`) now build
  `slog.New(logging.NewTeeHandler(slog.NewTextHandler(..., &slog.HandlerOptions{Level: slogLevel})))`
  instead of a bare `slog.NewTextHandler(..., &slog.HandlerOptions{Level: defaultLevel})` — both
  the tee wrapping (so a remote sink set once via `logging.SetRemoteSink` survives every
  `Reload()`, which rebuilds the handler and calls `slog.SetDefault` again) and the shared
  `slogLevel` pointer (so the *live* threshold can change without rebuilding the handler) land in
  the same edit. `Load` calls `slogLevel.Set(defaultLevel)` right after computing `defaultLevel`
  from `cfg.Debug`. `UpdateDebug` now also calls `slogLevel.Set(slog.LevelDebug/Info)` after a
  successful persist, so toggling Debug from the TUI/WebUI changes the live log level immediately
  instead of only taking effect after a restart.
- **`telemetry.go`** (small edit, Phase 1's file, allowed per the brief): `UpdateTelemetry`,
  `RegenerateTelemetryID`, and `UpdateTelemetryMinLevel` each now call `Bus.Publish(ConfigChangeEvent{...})`
  (`Section: "telemetry"`, `Source: "config"`, `ChangedKeys` naming the touched dotted path) right
  after a successful `updateGlobalCfgFile`. Investigated first: these setters only wrote the
  GLOBAL config file, and `cmd/root.go`'s `WatchConfigFile` watches whichever path
  `ResolveConfigFilePath` (local-preferring) resolves to — a different file whenever a
  project-local `.pando.toml` is active — so an in-process TUI/WebUI call to `UpdateTelemetry`
  would otherwise never reach `internal/app`'s telemetry runtime. Same pattern already used
  elsewhere (`propagateCoderModelToAgents` in config.go publishes directly after persisting, for
  the same reason).

### `internal/app` (new `telemetry.go` — the lifecycle/hot-toggle wiring)
- No new subpackage was needed: `internal/app` already imports `internal/config`, `internal/logging`,
  and (newly) `internal/telemetry`, and — importantly — `AppOptions.StartupMode` already exists and
  is already set correctly to `tui`/`serve`/`desktop`/`acp`/`cronjob`/`agui`/`mcp`/`app` at every
  `app.New(...)` call site (`cmd/root.go` ×2, `cmd/cronjob.go`, `cmd/agui_serve.go`,
  `cmd/mcp_server.go`, and `internal/api/server.go:107` which forwards `ServerConfig.StartupMode`
  for `cmd/serve.go`/`cmd/desktop.go`/`cmd/app.go`). So "mode" needed no new setter — `opt.StartupMode`
  is passed straight through as `telemetry.Options.Mode`.
- `telemetryRuntime` struct (mutex-guarded `*telemetry.Shipper` + last-applied
  `config.TelemetryConfig` + a `config.Bus` subscription channel + cancel func), mirroring
  `internal/cronjob.Service`'s Start/watch/Stop pattern:
  - `initTelemetry(cfg *config.Config, mode string) func(context.Context) error`: applies the
    initial state, then subscribes to `config.Bus` and starts a `watch` goroutine; returns the
    shutdown func.
  - `watch`: on every Bus event (no `ChangedKeys` filtering — `apply` is a cheap, idempotent
    no-op when nothing telemetry-related changed, same simplification `cronjob.Service` makes),
    re-reads `config.Get().Telemetry` and calls `apply`.
  - `apply(next)`: starts the shipper when `next.Enabled && telemetry.Available()` and none is
    running; stops it when it should no longer run; live-updates `DebugID` via
    `Shipper.SetDebugID` when only that changed; **restarts** (stop+start) when `MinLevel` changed,
    since `Options.MinLevel` is read once at `NewShipper` construction with no live setter exposed
    (documented as an accepted simplification — a rarely-changed field, not worth adding shipper
    API surface for).
  - `startLocked`: builds `telemetry.Options` from `telemetry.Endpoint()`/`telemetry.Token()`
    (build-time or env-overridden) + `cfg.DebugID`/`mode`/parsed `MinLevel`, `NewShipper` + `Start`,
    installs it via `logging.SetRemoteSink`, then logs `logging.Info("Telemetry enabled", "event",
    "telemetry.enabled", "mode", mode)` through the ordinary logging path (so it appears locally
    too, and is itself subject to the just-configured `MinLevel` like any other record — no
    special-casing needed since the sink is already installed by this point).
  - `stopLocked`: `logging.SetRemoteSink(nil)` **before** `Shipper.Stop(ctx)` (2 s deadline,
    `remoteSinkStopDeadline`), per the brief — no new record can be routed to a shipper that is
    already draining.
  - `shutdown(ctx)`: cancels the watch goroutine, unsubscribes from `config.Bus`, and — only if a
    shipper is running — ships an `event=app.shutdown` record **directly against the shipper**
    (bypassing slog, since the sink is about to be cleared) before `SetRemoteSink(nil)` +
    `Shipper.Stop(ctx)`.
  - `parseTelemetryLevel(string) slog.Level`: maps `config.TelemetryLevelDebug/Info/Warn/Error`
    (case-insensitive) to `slog.Level`, defaulting to Info.
- **`app.go`**: added `App.telemetryShutdown func(context.Context) error` field (next to the
  existing `openlitShutdown`); `app.telemetryShutdown = initTelemetry(cfg, opt.StartupMode)` added
  right after the existing `observability.Init(cfg.OpenLit, ...)` block inside `New`'s
  `if cfg := config.Get(); cfg != nil { ... }` section; `App.Shutdown()` calls it with a
  `remoteSinkStopDeadline` (2 s) bounded context, right after the `openlitShutdown` call.
- **No `cmd/*.go` changes were needed** for entry-point wiring (the brief's Task 5 anticipated
  possibly touching `cmd/desktop.go`/`serve`/`acp` directly): confirmed by tracing every
  `app.New(...)` call site plus `internal/api/server.go:107-108` (`app.New(ctx, cfg.DB,
  app.AppOptions{DBQuerier: cfg.Querier, StartupMode: cfg.StartupMode})`) that **every** entry
  point (tui, acp, serve, desktop, app, agui, cronjob, mcp) already funnels through `app.New`, and
  every one of them already calls `.Shutdown()` (directly, or via `api.Server.Shutdown` →
  `s.app.Shutdown()`) on graceful exit. Hooking the lifecycle into `app.New`/`App.Shutdown` alone
  was therefore sufficient and covers every surface without per-entry-point edits.

## API added
- `internal/logging`: `RemoteSink`, `RemoteSinkFlusher` interfaces; `SetRemoteSink(RemoteSink)`,
  `RemoteSinkActive() bool`, `FlushRemoteSink(ctx) error`; `NewTeeHandler(slog.Handler) slog.Handler`.
- `internal/app` (unexported, package-internal): `initTelemetry`, `telemetryRuntime` (+ its
  methods), `parseTelemetryLevel`, `remoteSinkStopDeadline`.

## Dependency rule respected
`internal/logging` now imports `internal/telemetry` (only for the `SkipAttrKey` string constant,
in `tee_handler.go`) — confirmed safe: `internal/telemetry` imports only stdlib + `internal/redact`
+ `internal/version`, none of which import `internal/logging`, so no cycle. `internal/logging`
still does **not** import `internal/config`. The `config`↔`telemetry`↔`logging`↔`app` wiring lives
entirely in `internal/app/telemetry.go`, which already legitimately imports all three.

## Hot toggle trigger (in-process TUI/WebUI calls)
`config.UpdateTelemetry` / `RegenerateTelemetryID` / `UpdateTelemetryMinLevel` each publish on
`config.Bus` directly after a successful persist (added in this phase, see above) — this is what
`internal/app`'s `telemetryRuntime.watch` reacts to. A config-file edit picked up by
`cmd/root.go`'s file watcher also publishes on `Bus` (pre-existing, via `Reload()`), so an external
edit to the global config file works too, whenever the watched path happens to be the global file.

## Verification
- `go build ./...` — clean (whole repo, including concurrently-landed Phase 4/5 changes from other
  agents).
- `go vet ./internal/logging ./internal/telemetry/... ./internal/config ./internal/app ./cmd/...`
  and `go vet ./...` — clean.
- `go test -race ./internal/logging ./internal/telemetry/... ./internal/config -count=1` — all
  `ok` (new tests: `internal/logging/tee_handler_test.go` — primary-only/nil-sink, WithAttrs+WithGroup
  dotted-key flattening, `SkipAttrKey` not forwarded, `Enabled()` true when only the sink wants the
  level, a concurrency smoke test; `internal/logging/logger_panic_test.go` — `RecoverPanic` ships
  an `event=panic` record with a real stack trace and calls `Flush` synchronously, and behaves
  unchanged with no sink; `internal/config/telemetry_reload_test.go` —
  `TestLoadWiresTeeHandlerAcrossReload` proves a sink set once keeps receiving records across a
  second `Load()` call simulating `Reload()`).
- `go test -race ./internal/app -count=1` and `go test ./internal/app ./internal/api -count=1` —
  both `ok` (new `internal/app/telemetry_test.go`: `TestTelemetryRuntimeHotToggle` drives
  `telemetryRuntime.apply` against an `httptest` mock ingest server via `PANDO_TELEMETRY_ENDPOINT`
  + `PANDO_TELEMETRY_TOKEN=test` (`t.Setenv`) — enabling ships records carrying the configured
  `debug_id` and `app.mode`, plus the `telemetry.enabled` lifecycle record; disabling clears
  `RemoteSinkActive()` immediately and no further record is ever routed; a companion test confirms
  `apply` never starts a shipper when `telemetry.Available()` is false regardless of `Enabled`).
- `go test ./internal/llm/agent ./internal/api -count=1` (CLAUDE.md's verified command) —
  `internal/api` PASS; `internal/llm/agent` has the same 4 pre-existing, previously root-caused
  HOME-leakage failures (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`,
  `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged`) in
  files this change never touches — unrelated, not investigated further (see
  [[remote_telemetry_betterstack_phase0_1]] and [[pando_repo_pitfalls]] for the prior root-cause).
- `gofmt -l` on every touched/new file in `internal/logging`, `internal/telemetry`,
  `internal/config`, `internal/app` — no output (clean).

## Hard constraints honored
No Better Stack token was ever read, printed, or fetched; `kvage` was never run; every test hits
only local `httptest.Server` instances, never the real Better Stack endpoint. No commits, no
jj/git mutating commands, no `git stash`. Did not touch `internal/api/handlers_settings.go`,
`web-ui/**`, or `internal/tui/**` (owned by the concurrent Phase 4/5 agents, confirmed untouched
via `jj st` before and after this work).

## Open items / deviations for later phases
- `MinLevel` live-update restarts the shipper (stop+start) rather than exposing a live setter on
  `Shipper` for that one field — acceptable per the brief's "restart or update" wording; flagged in
  code comments in case Phase 6+ wants a true live setter instead.
- The `telemetry.enabled` record is shipped through the normal `logging.Info` call (so it is itself
  subject to `MinLevel` filtering, and also appears in local logs) rather than being force-sent
  unconditionally; considered the more consistent behavior and not called out as a deviation in the
  brief, but noting it here in case a future phase wants lifecycle events to always ship regardless
  of `MinLevel`.

Related: [[remote_telemetry_betterstack_plan]], [[remote_telemetry_betterstack]],
[[remote_telemetry_betterstack_phase0_1]], [[fix_config_tests_home_leakage_and_template_drift]]