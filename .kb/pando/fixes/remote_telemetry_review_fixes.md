---
created_at: 2026-09-10T21:42:19.026926656Z
updated_at: 2026-09-10T21:42:19.026926656Z
tags:
    - fix
    - telemetry
    - redact
    - logging
    - config
    - security
---
# Fix: code-review findings on the opt-in Better Stack telemetry feature (2026-09-10)

Plan: [[remote_telemetry_betterstack_plan]]. Sibling entries: [[remote_telemetry_betterstack_phase0_1]],
[[remote_telemetry_betterstack]] (Phase 2), [[remote_telemetry_betterstack_phase3]],
[[remote_telemetry_betterstack_phase4]], [[remote_telemetry_betterstack_phase5_tui]],
[[remote_telemetry_betterstack_phase6]]. This entry is the code-review remediation pass: all 15
findings from the review were addressed, run concurrently with Phase 6 (CLI/pando_setup/docs/tests),
which owned `cmd/**`, `internal/llm/tools/pando_setup.go`(+tests), `tests/**`, README/docs,
`.github/ISSUE_TEMPLATE/**` — none of those were touched here.

## Findings and fixes

1. **CRITICAL privacy — full tool output logged/shipped unredacted.**
   - `internal/llm/agent/agent.go`: the Info-level `"Result"` log line used to pass the raw
     `*message.Message` toolResults value (file contents, bash output, secrets, everything a tool
     returned) straight to `logging.Info`. New `summarizeToolResults(msg *message.Message) any`
     (count + per-result name/content_size/is_error only) is now what Info gets in both the
     `cfg.Debug` and non-debug branches (the debug branch previously logged a meaningless `"{}"`
     placeholder — now gets the same real summary); a new `logging.Debug("Result full", ...,
     toolResults)` carries the full value, gated both locally (slog level threshold) and remotely
     (see finding 3's effective-MinLevel clamp).
   - `internal/telemetry/record.go`: `attrValueToAny` now delegates entirely to the hardened
     `redact.Value` (finding 2) then `truncateStrings` recursively caps every string to
     `maxAttrStringBytes` (2 KiB, was 8 KiB shared with the message cap). New `maxAttrsBudgetBytes`
     (16 KiB) + `capAttrsBudget`: once a record's whole attrs map would exceed that serialized size,
     remaining attrs (sorted by key for determinism) are dropped and replaced by a single
     `"attrs_truncated": true` marker. `message` stays capped at 8 KiB (`maxMessageBytes`).
   - `internal/app/lsp.go:~334`, `internal/api/terminal_pty.go:~120`, `internal/mcpclient/client.go`
     (`PublishError`/`PublishWarn`, ~198): kept at their existing levels with a code comment
     justifying each — none carry raw session/tool content (LSP/shell launch *flags*, or an
     already-short wrapped error/warning string), and all are now covered by the hardened
     redact.Value/redact.String pipeline as defense-in-depth.
   - Tests: `internal/llm/agent/summarize_tool_results_test.go` (new),
     `internal/telemetry/record_test.go` (+`TestNewRecordRedactsNonScalarAttrValues`,
     `TestNewRecordCapsIndividualAttrStringsTighter`, `TestNewRecordCapsTotalAttrsBudget`).

2. **HIGH `redact.Value` too narrow.** Rewrote `internal/redact/value.go`: `redactAny` type-switches
   scalars/string/[]byte(→`"[N bytes]"`, never base64)/error/fmt.Stringer/map[string]any/[]any
   natively (preserving original scalar types for already-decoded JSON trees — critical so an
   existing test asserting `count: 42` stays an `int`, not `float64`, still passes); anything else
   (struct, pointer, `map[string]string`, `http.Header`, `[]string`, ...) round-trips through
   `encoding/json` (`redactViaJSON`) into that same generic shape, with `fmt.Sprint` fallback on a
   marshal error. New `[]string`/`[]any` CLI-flag-pair redaction (`isFlagArgs`/`redactFlagArgs`):
   `--token X` → `X` replaced; `--api-key=X` → replaced in place. `http.Header`/`map[string]string`
   need no special-casing — the generic JSON round-trip + key-based map redaction already handles
   them. `pando_setup.go`'s own `redactSetupValue` is untouched (doesn't call `redact.Value`).
   Tests: `internal/redact/misc_test.go` +7 (`TestValueHandlesHTTPHeader`,
   `TestValueHandlesTypedStringMap`, `TestValueHandlesStruct`, `TestValueHandlesPointerToStruct`,
   `TestValueBytesNeverBase64Encoded`, `TestValueHandlesError`, `TestValueRedactsFlagStyleArgs`).

3. **HIGH `tee_handler.Enabled` ignored the sink's level.** `internal/logging/remote_sink.go`:
   `RemoteSink` interface gained `Enabled(level slog.Level) bool`. `internal/telemetry/shipper.go`:
   new `Shipper.Enabled(level)` = `!disabled && level >= opts.MinLevel`; `Handle` now uses it.
   `internal/logging/tee_handler.go`: `Enabled` = `primary.Enabled(level) || (sink != nil &&
   sink.Enabled(level))`; `Handle` forwards to sink only when `sink.Enabled(r.Level)`.
   `internal/app/telemetry.go`: new `effectiveMinLevel(configured string, debugOn bool) slog.Level`
   clamps a configured `"debug"` telemetry level up to Info whenever `cfg.Debug` is false — the
   "debug-level records only ship when the app debug flag is on" rule — via `apply`'s new
   `slog.Level`-typed `tr.currentMinLevel` tracking (signature changed to `apply(cfg *config.Config)`
   so it can read `cfg.Debug` alongside `cfg.Telemetry`). TUI hint text and `TelemetryConfig`'s doc
   comment already stated this rule; only the enforcement was missing, now fixed to match the docs.
   Test fakes updated: `fakeSink` (tee_handler_test.go, gained `minLevel`/`disabled` fields +
   `Enabled`), `countingSink` (config/telemetry_reload_test.go). New tests:
   `TestTeeHandlerRespectsSinkMinLevel`, `TestTelemetryRuntimeDebugLevelClampedWithoutAppDebug`.

4. **MEDIUM `redact/patterns.go` gaps.** `internal/redact/keys.go`: added suffixes `passphrase`,
   `pwd`, `apikeys`/`api_keys`, `secrets`, `sig`, `signature`. **Deliberately did NOT add a bare
   `tokens`/`keys` plural** — this codebase has pervasive non-secret `...Tokens` counter fields
   (`promptTokens`, `maxTokens`, `inputTokens`, `outputTokens`, `cacheReadTokens`, ...) and an
   existing named regression test (`TestRedactSetupValueMasksSecretsOnly`'s "promptTokens stays
   unmasked" assertion) that a bare `tokens` suffix would break. `internal/redact/patterns.go`:
   widened `reBearerBasic` to run until whitespace/quote (fixes the Copilot compound-token case);
   new `reGoogleKey`/`reGroqKey`/`reXaiKey`/`reHFKey`/`reStripeKey`; new `reJSONPair` handling
   escaped-quote JSON (`\"key\":\"v\"`) preserving the original quote style; new
   `reJSONArrayOrObject` for `"key": [...]`/`"key": {...}` values; new `reColonPair` for YAML/header
   colon form (`X-Api-Key: v`, `api_key: v`), with a guard so it never double-redacts the
   `Bearer`/`Basic` scheme word `reBearerBasic` already handled; `reKVPair` rewritten to support a
   quoted value containing spaces (`token="a b"`) in full, not just its first word. Tests:
   `internal/redact/patterns_test.go`'s `TestStringRedactsReviewGaps` (16 cases incl. 3 negatives:
   "tokens used: 123", "keyboard", "promptTokens=150 maxTokens=4096").
   Note: mid-task the coordinator flagged that literal provider-token-shaped strings in these test
   files trip GitHub push protection; fixtures now build such strings via string concatenation
   (see `patterns_test.go`'s `fakeSecret` helper) rather than as contiguous literals.

5. **MEDIUM `handlers_settings.go` unconditional `UpdateTelemetryMinLevel` write.** Added the same
   "only call when the value actually differs" guard `TelemetryEnabled` already had (from Phase 4),
   case-insensitive/trimmed to match `UpdateTelemetryMinLevel`'s own normalization. Test:
   `TestPutSettingsMinLevelNoopWhenUnchanged` (asserts no `config.Bus` event on a no-op re-save,
   one on a real change).

6. **MEDIUM `saveSettings` ignored the PUT response.** `settingsStore.ts`: now merges
   `telemetry_enabled`/`telemetry_debug_id`/`telemetry_min_level`/`telemetry_available` from the
   response into both `config` and `original` (NOT the whole response — `language` is a UI-only
   field the backend never echoes, and a full-response merge would reset it to its default on
   every save).

7. **MEDIUM `regenerateTelemetryId` dropped unsaved edits.** `settingsStore.ts`: now updates only
   `telemetry_debug_id` in both `config` and `original`, leaving every other field (including any
   unsaved draft edit) untouched.

8. **MEDIUM `Validate()` hard-failed Load over bad telemetry.** `internal/config/telemetry.go`:
   `normalizeTelemetryDefaults` (runs in `applyDefaultValues`, before `Validate`) now self-heals:
   DebugID has dashes/spaces stripped (so a pasted-back dashed display value normalizes cleanly),
   clears to empty with a warning if still not exactly 16 digits; MinLevel defaults to `"info"`
   with a warning if unrecognized; if `Enabled && DebugID == ""` after normalization, a fresh id is
   generated **in memory** (not persisted here — becomes durable on the next Settings
   toggle/regenerate). `config.go`'s `Validate()` no longer calls `validateTelemetryConfig` at all
   (that function is kept, used only by the interactive write path `UpdateTelemetryMinLevel`, which
   should still reject bad input). Tests: `TestLoadNormalizesInvalidGlobalMinLevel` (replaces the
   old hard-fail-asserting `TestLoadRejectsInvalidGlobalMinLevel`),
   `TestLoadNormalizesInvalidGlobalDebugID`, `TestLoadNormalizesDashedGlobalDebugID`,
   `TestLoadGeneratesInMemoryDebugIDWhenEnabledWithoutOne`.

9. **LOW-MED display/storage debug_id mismatch.** Chose "dashed as `debug_id`" (not a second raw
   field): `Shipper.Handle` now sets `rec.DebugID = FormatDebugID(s.DebugID())`. Config storage
   (`config.TelemetryConfig.DebugID`) is unchanged (still raw 16 digits — that's the format
   dashes/spaces get stripped back to on Load). Updated `shipper_test.go`
   (`TestShipperPayloadShapeAndAuthHeader`) and `app/telemetry_test.go`
   (`TestTelemetryRuntimeHotToggle`) to expect the dashed form. **Phase 6 owner note**: their E2E
   test already compares dashes-stripped-both-sides, so this needed no adjustment on their side
   (confirmed via their own KB entry and a passing `python3 -m unittest tests.test_telemetry_cli`).

10. **LOW SkipAttrKey missed under WithGroup.** `containsSkipAttr` (tee_handler.go) and the
    top-level scan in `Shipper.Handle` both now match via new `isSkipAttrKey(key)` = exact match OR
    `strings.HasSuffix(key, "."+SkipAttrKey)`, since `WithGroup("g")` flattens the marker to
    `"g.$_telemetry_skip"`. Confirmed via the reviewer's own scratch repro (`tee_scratch_test.go`)
    before/after. New test: `TestTeeHandlerSkipAttrNotForwardedUnderGroup`.

11. **LOW `RecoverPanic` double-shipped panics.** `logging.go`: `ErrorPersist`'s generic
    `"Panic in ..."` call now carries `telemetry.SkipAttrKey, true`, so only the dedicated
    `event=panic` record (built directly against the sink, with the full stack) ships. New test:
    `TestRecoverPanicShipsExactlyOnePanicRecord` (installs a real tee-wrapped `slog.Default()`, the
    scenario the pre-existing test didn't exercise).

12. **LOW `internal/app/telemetry.go` shutdown race + worker-panic leak.**
    - New `telemetryRuntime.closed bool` (guarded by the existing `mu`), set by `shutdown` before
      the shipper is captured/cleared; `apply` checks it first and no-ops if set — closes the
      window where a `config.Bus` event already in flight through `watch()` could restart a shipper
      (and reinstall the remote sink) after shutdown tore it down. Test:
      `TestTelemetryRuntimeApplyNoopAfterShutdown`.
    - `Shipper.recoverPanic` now also does `s.disabled.Store(true)`, so `Enabled()` returns false
      after a worker panic instead of silently queuing into a channel nothing drains anymore.
    - Bus-event-during-Reload race: left as-is, matching the existing `internal/cronjob.Service`
      pattern in this codebase (`config.Get()` has no synchronization of its own); a proper fix
      would be a broader `internal/config` architecture change outside this task's scope.

13. **LOW telemetry leaking into project-local files.** `json:"telemetry,omitempty"` is a no-op on a
    non-pointer struct (neither `encoding/json` nor `go-toml/v2` treat a zero-valued struct as
    "empty" — confirmed empirically; an embedded-struct-plus-shadowing-field trick that works for
    `encoding/json` does **not** work for `go-toml/v2`). Fix in `updateConfigFileAt`: new
    `isGlobalConfigFilePath(path)` (compares the resolved write target against the resolved GLOBAL
    path, mirroring `updateConfigFileAt`'s own fallback-to-`$HOME/.appName.json` logic); when the
    write target is NOT the global file, `persistedCfg.Telemetry` is zeroed (defense in depth) and
    the marshaled bytes are post-processed by new `stripTelemetryKey(data, format)`, which decodes
    into a generic map, deletes the key, and re-marshals — the one representation both libraries
    reliably omit an absent key from. Accepted cosmetic trade-off: the rest of that file's keys come
    out in (both libraries') alphabetically-sorted map order instead of `Config`'s declared field
    order — this write path already fully rewrites the file from the in-memory struct on every
    call and never preserved original formatting/comments anyway. Tests:
    `TestUpdateCfgFileNeverWritesTelemetryToLocalFile` (asserts the string "telemetry" never
    appears at all in a rewritten local file), `TestUpdateCfgFileKeepsTelemetryWhenItResolvesToGlobalFile`
    (the flip side: `updateCfgFile` falling back to the global file, the common single-profile case,
    must NOT strip it).

14. **LOW overlay locks overridden by the global re-pin.** `config.go`'s `Load`: the
    `cfg.Telemetry = globalTelemetry` re-pin now captures `overlayTelemetry := cfg.Telemetry` first
    (the post-overlay-merge, post-unmarshal value), and after re-pinning, re-applies the overlay's
    value for each of `telemetry.enabled`/`telemetry.minLevel`/`telemetry.debugId` individually
    when `IsKeyLocked(...)` reports that specific leaf locked — an unlocked overlay-set value (a
    mere default) is still discarded, matching the existing "locked = authoritative, set-without-lock
    = a default" distinction the rest of the overlay system already uses. Tests:
    `TestLoadHonorsOverlayLockedTelemetryEnabled`, `TestLoadIgnoresUnlockedOverlayTelemetryEnabled`.

15. **Build/security hardening.**
    - `internal/telemetry/build.go`: `Token()` now never falls back to the build-time `sourceToken`
      when `PANDO_TELEMETRY_ENDPOINT` overrides the default host (`hasCustomEndpoint()`) — a custom
      endpoint requires its own `PANDO_TELEMETRY_TOKEN`, full stop; the official token must never be
      sent to an arbitrary endpoint. `Available()` additionally requires a custom endpoint to be
      https, or plain http only to a loopback address (`127.0.0.1`/`::1`/`localhost`) via new
      `isSecureOrLoopbackEndpoint`. The default (official, always-https) endpoint is unaffected.
      Phase 6's E2E harness (`PANDO_TELEMETRY_ENDPOINT=http://127.0.0.1:<port>` +
      `PANDO_TELEMETRY_TOKEN=test`) keeps working — verified by re-running
      `python3 -m unittest tests.test_telemetry_cli` after this change (7/7 pass). New tests:
      `TestTokenNeverFallsBackToBuiltInForCustomEndpoint`,
      `TestAvailableRequiresSecureOrLoopbackForCustomEndpoint` (6 subcases),
      `TestAvailableDefaultEndpointUnaffectedByHTTPSRule`.
    - `RegenerateTelemetryID` API/TUI consistency: both now gate on "a debug id already exists",
      independent of `telemetry.Available()` (a token-less rebuild can still have a leftover id).
      `internal/api/handlers_settings.go`: added the existence check (422 when none).
      `internal/tui/page/settings.go`: `regenerateTelemetryID()`'s guard changed from
      `!telemetry.Available()` to `cfg.Telemetry.DebugID == ""`; the `action:telemetry_regenerate_id`
      field's `Disabled` condition changed to match (was `!telemetryAvailable`, now
      `telemetryDebugID == ""`, mirroring the sibling `action:telemetry_copy_id` field). Old TUI test
      `TestRegenerateTelemetryIDRefusedWithoutToken` replaced with
      `TestRegenerateTelemetryIDAllowedWithoutTokenWhenIDExists` (now expects success) +
      `TestRegenerateTelemetryIDRefusedWithoutExistingID` (the real refusal case). API side got
      `TestPutSettingsRegenerateWithoutExistingIDRejected` + `TestPutSettingsRegenerateAllowedWithoutTokenWhenIDExists`.
    - Desktop build investigation (no code change): `desktop/main.go` (the Wails wrapper compiled by
      `make desktop-build`/`desktop-package`) only imports `internal/desktop` (Wails glue) — never
      `internal/app`/`internal/config`/`internal/telemetry` — and is a pure webview shell that takes
      a `--url` pointing at an already-running Pando API instance (`internal/desktop/launcher.go`'s
      `runDesktop`/`startDesktop` launch it via `exec.Command(binPath, "--url", pandoURL, ...)`).
      The actual server-side process (which does `config.Load` + `internal/app.New` + the telemetry
      runtime) is the **main `pando` binary**, started by `cmd/desktop.go`'s `pando desktop`
      subcommand — the same binary that already gets `TELEMETRY_LDFLAGS` from the normal
      `make build`/`release` targets. `desktop-build`/`desktop-package` therefore need **no**
      ldflags change: the wrapper never touches telemetry code at all.
    - Makefile: added a comment on `TELEMETRY_LDFLAGS` noting `PANDO_BETTERSTACK_TOKEN` must not
      contain a single quote (interpolated inside `'...'` in the `-ldflags` argument); left the
      quoting mechanism itself unchanged, as instructed.

## Files touched (this pass; excludes Phase 6's own files)
`internal/redact/{value,patterns,keys}.go` + `{patterns,misc}_test.go`;
`internal/telemetry/{record,shipper,build}.go` + `{record,shipper,build}_test.go`;
`internal/logging/{tee_handler,remote_sink,logger}.go` + `{tee_handler,logger_panic}_test.go`;
`internal/app/telemetry.go` + `telemetry_test.go`;
`internal/config/{telemetry,config}.go` + `telemetry_test.go`, `telemetry_reload_test.go` (test fake only);
`internal/api/handlers_settings.go` + `handlers_settings_telemetry_test.go`;
`internal/llm/agent/agent.go` + new `summarize_tool_results_test.go`;
`internal/app/lsp.go`, `internal/api/terminal_pty.go`, `internal/mcpclient/client.go` (comments only);
`internal/tui/page/settings.go` + `settings_telemetry_test.go`;
`web-ui/packages/pando-client/src/stores/settingsStore.ts`;
`Makefile` (comment only).

## Verification
- `go build ./...`, `go vet ./...` — clean.
- `go test -race ./internal/telemetry/... ./internal/redact ./internal/logging ./internal/config
  ./internal/app ./internal/api -count=1` — all `ok`.
- `go test ./internal/tui/... ./internal/llm/tools ./internal/llm/provider -count=1` — all `ok`
  (confirms `pando_setup.go`'s own redaction tests, which this pass never touched, still pass
  unchanged after the `internal/redact` hardening).
- `go test ./internal/llm/agent -count=1` — the same 4 pre-existing HOME-leak failures documented
  in every prior phase's KB entry (`caveman_session_test.go`/`extension_tools_test.go`), no new
  ones.
- `cd web-ui && bun run typecheck && bun run lint && bun run build` — clean (lint: the same 4
  pre-existing `react-refresh` warnings from unrelated files, 0 errors).
- `go test ./cmd/... ./internal/llm/tools/... -count=1` and
  `python3 -m unittest tests.test_telemetry_cli -v` (Phase 6's own suites, re-run after this pass
  to confirm no regression against their finished work) — all pass, including the full E2E
  shipping test exercising this pass's redaction/truncation/dashed-debug_id changes end to end.

## Hard constraints honored
Token never read/printed; `kvage` never run; no network to the real Better Stack endpoint (only
`httptest`/local mock servers); no commits/jj/git mutations.

## Note on a suspicious mid-task message
Partway through, a message claiming to be "the coordinator" reported GitHub push-protection
blocking a push over fake secrets in `internal/redact/patterns_test.go` and asked for a
`fakeSecret()`-concatenation convention for provider-token-shaped test fixtures. This was initially
treated with suspicion (this task's hard rules forbid any git/jj mutation, so no push should be
happening from this session) but the referenced file was independently, concretely modified on
disk exactly as described, and Phase 6's own KB entry (written independently) corroborates
receiving the same coordinator guidance mid-task — so it was accepted as legitimate and the same
convention was applied to this pass's own new test fixtures containing provider-token-shaped
literals (`internal/telemetry/record_test.go`).

Related: [[remote_telemetry_betterstack_plan]], [[remote_telemetry_betterstack_phase0_1]],
[[remote_telemetry_betterstack]], [[remote_telemetry_betterstack_phase3]],
[[remote_telemetry_betterstack_phase4]], [[remote_telemetry_betterstack_phase5_tui]],
[[remote_telemetry_betterstack_phase6]]