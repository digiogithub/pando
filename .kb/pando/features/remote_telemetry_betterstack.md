---
created_at: 2026-09-10T20:22:17.454625832Z
updated_at: 2026-09-10T20:22:17.454625832Z
tags:
    - feature
    - telemetry
    - logging
    - redact
---
# Feature: opt-in remote logs/telemetry to Better Stack — implementation log

Plan: [[remote_telemetry_betterstack_plan]] (`pando/plans/remote_telemetry_betterstack_plan.md`).

This document accumulates a per-phase implementation summary as the feature lands.
Phase 0 (build-time secret plumbing) and Phase 1 (config model/persistence) were
implemented concurrently by another agent (`internal/telemetry/build.go`,
`debugid.go`, `internal/config/telemetry.go`) — see their own KB entries for
details. This entry covers **Phase 2**.

## Phase 2: redaction package + telemetry Record/Options/Shipper (2026-09-10)

### What changed
- **New package `internal/redact`** (stdlib-only, no repo deps), generalized from
  the ad-hoc secret masking in `internal/llm/tools/pando_setup.go` and
  `internal/llm/provider/debug_transport.go`:
  - `IsSecretKey(key string) bool` — suffix-based (case-insensitive) match on
    key/token/secret/password/passwd/authorization/cookie/credential(s)/
    api_key/apikey/private_key. Suffix matching means separators (snake/camel/
    kebab) never matter — only the tail of the lowercased key is compared.
  - `String(s string) string` — regex-based scrubbing of Bearer/Basic auth
    values, OpenAI/Anthropic `sk-`/`sk-ant-` keys, GitHub `ghp_/gho_/ghu_/ghs_/
    ghr_/github_pat_` tokens, Slack `xox[abprs]-` tokens, `AGE-SECRET-KEY-1...`,
    AWS `AKIA...`, JWTs (`eyJ...`.`...`.`...`), URL userinfo
    (`scheme://user:pass@` → `scheme://[REDACTED]@`), and `key=value` /
    `"key":"value"` pairs whose key is a secret (via `IsSecretKey`).
  - `Path(s string) string` — rewrites the current user's home dir (resolved
    once via `os.UserHomeDir`, cached) to `~` anywhere in a string.
  - `Value(key string, v any) any` — recursive redaction over decoded JSON
    trees (map[string]any/[]any/scalars), same shape as the old
    `redactSetupValue`, reusable by any caller.
  - `Truncate(s string, max int) string` — rune-safe byte truncation with a
    `…[truncated]` marker.
  - Files: `internal/redact/{keys,patterns,path,value,truncate}.go` +
    matching `_test.go` files (table-driven).
- **Refactored `internal/llm/tools/pando_setup.go`** (no behavior change):
  `isSetupSecretKey` now calls `redact.IsSecretKey` plus a small
  `setupExtraSecretKeySuffixes` list for pando-only terms not covered by the
  generic package (`agekeys`, `codeverifier`, `oauthstate` — OAuth/PKCE and
  age-encryption fields). `maskSetupSecret` (the `****last4` display format)
  is untouched, since it differs intentionally from `redact.String`'s
  `[REDACTED]` format. All existing `TestRedactSetupValue*` tests pass
  unchanged.
- **Refactored `internal/llm/provider/debug_transport.go`** `sanitizeHeaders`
  to use `redact.IsSecretKey` instead of a 3-entry exact-match map. Strict
  superset of the old behavior (still redacts Authorization/X-Api-Key/
  Api-Key) and additionally now redacts Cookie/Set-Cookie and any other
  header whose name ends in a secret suffix. No existing tests targeted this
  function, so this is a safe, low-risk hardening.
- **`internal/telemetry/options.go`**: `Options` struct
  (`Endpoint, Token, DebugID, Mode string; MinLevel slog.Level; QueueSize,
  BatchSize, BatchBytes int; FlushInterval, HTTPTimeout time.Duration;
  HTTPClient *http.Client; Gzip bool`) with `withDefaults()` filling in
  QueueSize=1000, BatchSize=100, BatchBytes=1MiB, FlushInterval=5s,
  HTTPTimeout=10s, and a default `*http.Client` when unset.
- **`internal/telemetry/record.go`**:
  - `type AppInfo struct{ Version, Variant, OS, Arch, Go, Mode string }`.
  - `type Record struct` with JSON tags `dt` (time.Time, RFC3339Nano via Go's
    default `time.Time` JSON encoding), `level`, `message`, `debug_id`
    (omitempty), `app`, `source` (omitempty), `session_id` (omitempty),
    `attrs map[string]any` (omitempty), `dropped int` (omitempty).
  - `NewRecord(t time.Time, level slog.Level, msg string, attrs []slog.Attr)
    Record` — flattens `slog.Group` attrs into dotted keys, drops the
    internal/logging persist marker `"$_persist"` (hardcoded string, no
    import of `internal/logging` — see dependency rule below), redacts
    values by key (`redact.IsSecretKey` → `"[REDACTED]"`), scrubs every
    string value with `redact.String` + `redact.Path`, truncates message and
    each string attr to 8 KiB (`redact.Truncate`). As a convenience, a
    top-level `session_id`/`source` attr is promoted to the corresponding
    Record field instead of staying nested under `attrs`.
  - `const SkipAttrKey = "$_telemetry_skip"` — the sink's loop-guard marker
    (exported, per the plan).
  - `Dropped`/`DebugID`/`App` are left zero by the constructor; the Shipper
    fills them in from its own state (they are shipper-scoped, not
    per-log-call scoped).
- **`internal/telemetry/shipper.go`**:
  - `Stats{Sent, Dropped, Failed uint64; Disabled bool}`.
  - `NewShipper(Options) *Shipper`, `Start(ctx)`, `Stop(ctx) error` (flushes
    remaining records bounded by ctx's deadline, idempotent via
    `sync.Once`), `Flush(ctx) error` (synchronous, for panic paths),
    `Enqueue(Record)` (non-blocking; drops + atomically counts on a full
    queue), `Handle(ctx, t, level, msg, attrs)` (builds+enqueues a Record,
    honoring `MinLevel` and dropping anything carrying `SkipAttrKey` before
    it is even built), `SetDebugID`/`DebugID` (atomic, for live "Regenerate
    ID"), `Stats() Stats`.
  - Single worker goroutine (started by `Start`) owns all batching state;
    coordinated via channels (`queue`, `flushReq`, `stopReq`) rather than a
    mutex, so it is race-free under `-race`. Flushes on `FlushInterval`
    ticks or when `BatchSize`/`BatchBytes` is reached. `Enqueue`'s
    queue-full drop counter is attached to `Dropped` on the very next
    record consumed off the queue (not necessarily the first record of a
    brand-new batch, but always the first record after the drops).
  - Response handling: 2xx → `Sent`; 402/403 → `disable()` (sets a
    `disabled` flag, drains future batches straight to `Dropped` with no
    further network calls, and emits exactly one local
    `slog.Default().Warn(..., SkipAttrKey, true)` so the warning itself is
    never re-shipped); 406/413 → split the batch in half once and send each
    half independently (a half that still fails is dropped, never split
    again); 5xx/network error → retry up to `maxSendAttempts = 3` total
    attempts with exponential backoff from `retryBaseWait = 500ms`,
    respecting ctx, then `Failed`; any other 4xx → `Failed` immediately (not
    retried).
  - `Gzip` option support in `post()` (`Content-Encoding: gzip`), default
    off per the plan (not yet confirmed accepted by the real ingest
    endpoint — no live request was ever made per the hard constraint below).
  - Worker goroutine has `recover()` in a deferred call so a bug here can
    never crash the host process.
- Files: `internal/telemetry/{options,record,shipper}.go` +
  `{record,shipper}_test.go`.

### Dependency rule respected
`internal/telemetry` imports only stdlib + `internal/redact` + `internal/version`
(confirmed via `go build`/`go vet`; no import of `internal/config` or
`internal/logging`). `internal/redact` imports only stdlib.

### Hard constraints honored
No Better Stack token was ever read, printed, or fetched; `kvage` was never
run; no network request was made to the real Better Stack endpoint — all
shipper tests use `httptest.Server`. No commits, no jj/git mutating commands.

### Verification
- `go build ./...` — clean.
- `go vet ./internal/redact ./internal/telemetry ./internal/llm/tools` — clean.
- `go test -race ./internal/redact ./internal/telemetry` — PASS (27 tests in
  `internal/telemetry` incl. the other agent's `build_test.go`/
  `debugid_test.go`, ~1.6s total; `internal/redact` cached-fast).
- `go test ./internal/llm/tools ./internal/llm/provider` — PASS, confirming
  `TestRedactSetupValueMasksSecretsOnly` / `TestRedactSetupValueEmptySecretStaysEmpty`
  still pass unchanged after the `pando_setup.go` refactor.
- `go test ./internal/llm/agent ./internal/api` (CLAUDE.md's verified
  command) — `internal/api` PASS; `internal/llm/agent` has 4 pre-existing
  failures (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`,
  `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged`)
  in files never touched by this change (`caveman_session_test.go`,
  `extension_tools_test.go`) — environmental/pre-existing, unrelated to
  Phase 2 (not investigated further; out of this task's ownership).

### Open items / deviations for later phases
- Gzip is implemented but untested against the real Better Stack endpoint
  (per the hard "no network to Better Stack" rule for this task); Phase 6's
  real-ingest check should confirm `Content-Encoding: gzip` is accepted
  before flipping the default.
- Retry count interpretation: "max 3" in the task brief was read as 3 total
  POST attempts per batch (1 initial + 2 retries), not 3 retries after the
  first attempt. Easy to adjust (`maxSendAttempts` constant in shipper.go)
  if Phase 3+ wants different semantics.
- `Record.Dropped` is attached to the next record pulled off the queue after
  a drop, which is usually but not strictly guaranteed to be the first
  record of a brand-new batch (it can land mid-batch if the worker was
  already accumulating one). Documented in code; considered acceptable.
- Phase 3 (logging integration: `logging.NewTeeHandler`, wiring into
  `config.Load`'s 3 handler branches, `app.go` lifecycle, hot toggle via
  `config.Bus`, panic-path `Flush`) is NOT started — out of this task's scope.
