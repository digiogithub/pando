---
created_at: 2026-09-14T17:58:26.064013092Z
updated_at: 2026-09-14T17:58:26.064013092Z
---
# PANDO-US-0020 + PANDO-US-0021 + PANDO-US-0022 + PANDO-US-0023: AG-UI operability for embedded deployments

Part of [[PANDO-EP-0004]] "AG-UI operability for embedded deployments", the last open
epic in milestone PANDO-M-0001. Builds on PANDO-US-0011..0014 (agent pool/profiles),
PANDO-US-0015/0016 (see [[pando/features/agui-thread-api-and-messages-snapshot.md]]) and
PANDO-US-0017/0018/0019 (see [[pando/features/agui-run-parking-reattach-cancel.md]]),
whose per-run `pump` goroutine, `runStore.lockThread` and `finishRun`/`finalizeRun`
lifecycle this work extends rather than replaces. All four stories implemented and
merged in one pass. `go build ./...` clean; `go test -race ./internal/agui/...
./internal/config/... ./cmd/...` green; `go test ./...` clean repo-wide (101 packages
ok, no failures) both before and after this change.

## PANDO-US-0020 — Unauthenticated GET {path}/healthz

Files touched:
- `internal/agui/server.go` — `HealthResponse` struct (status/version/uptimeSeconds,
  plus activeRuns/maxConcurrentRuns/draining shaped in from the start per the story's
  note); `handleHealthz` (new); `Register` mounts `GET {path}/healthz` directly against
  the handler, never through `authorize()`, and never calls `setCORSHeaders`.
- `internal/agui/runtime.go` — `Runtime.startedAt time.Time`, set in `New`.

The route is registered inside `Register`, which is the single mux both `Runtime.Handler()`
(dedicated `agui-serve` listener) and `internal/api/server.go`'s `setupAGUI`/`routes.go`
(the co-mounted `pando serve --agui-port` path) call — no changes were needed in
`internal/api`: its `isAGUIPath` prefix match already routes every sub-path, healthz
included, around both `corsMiddleware` and `authMiddleware` unconditionally (verified by
reading `internal/api/server.go:421-519`), so healthz inherits that bypass and needs no
extra wiring there.

Tests: `internal/agui/server_test.go` (`TestHealthzIsUnauthenticatedAndMinimal` asserts
the exact field set and that no origin/token/agent/session data leaks;
`TestHealthzNotRoutedThroughCORSOrOriginAllowList`; `TestHealthzBypassesAuthorizeButInfoStillRequiresToken`
is the regression proving `/info` still 401s with no token, i.e. authorize() was not
widened) and `internal/agui/listener_test.go` (`TestListenerHealthzIsUnauthenticated`,
a literal HTTP GET against a real dedicated listener).

## PANDO-US-0021 — MaxConcurrentRuns cap with 503 and Retry-After

Files touched:
- `internal/agui/admission.go` (new) — `runAdmission`: mutex-guarded current/max
  counter, `tryAdmit`/`release`/`snapshot`, all nil-receiver-safe (a `Runtime` built
  directly by a test with no `admission` set behaves as unlimited).
- `internal/agui/runtime.go` — `Runtime.admission *runAdmission`, constructed in `New`
  as `newRunAdmission(cfg.MaxConcurrentRuns)`.
- `internal/agui/server.go` — `handleRun` calls `r.admission.tryAdmit()` immediately
  after validating the trailing user message and before `sessionForThread`/`pool.get`/
  `NewSSEWriter`/`runPrelude`/`svc.Run`/`runs.put`; every one of those failure branches
  now also calls `r.admission.release()` (a slot reserved but never turned into a
  registered run must not leak). `rejectOverCapacity` (503 + numeric `Retry-After`,
  constant `runRejectedRetryAfterSeconds = 5`) is the shared rejection path, reused by
  draining in US-0022.
- `internal/agui/run.go` — `Runtime.finishRun` now calls `r.admission.release()`,
  guarded by `activeRun.stop()`'s once-only return so a run torn down from more than one
  call site (explicit cancel racing the pump's natural finish) never double-releases.
- `internal/agui/deps.go` / `internal/config/config.go` — `Config.MaxConcurrentRuns` /
  `AGUIConfig.MaxConcurrentRuns int` (toml `MaxConcurrentRuns`), 0/unset = unlimited
  (today's behaviour), resolved as-is in `ConfigFromApp` (no special-casing needed since
  0 already means unlimited on both sides).

Key design point: a suspended (interrupt-parked) run is *not* released — its slot stays
held for as long as it is registered in `runStore`, exactly matching the pre-existing
"a parked run still occupies resources" invariant from PANDO-US-0017. A resumption
(`beginResumeSegment`) never calls `tryAdmit` again since it operates on an
already-registered `activeRun`, not a new one.

Tests: `internal/agui/server_test.go` —
`TestMaxConcurrentRunsRejectsOverCapWithNoStateCreated` (503 + Retry-After, no
RUN_ERROR, no session, no `agui_threads` row — `r.pool` deliberately left nil so a bug
that let the request through would panic instead of silently passing);
`TestMaxConcurrentRunsZeroMeansUnlimited`;
`TestMaxConcurrentRunsHoldsSlotWhileSuspendedThenReleasesOnFinish` (drives a real run
through `handleRun` to `RUN_FINISHED{outcome:"interrupt"}` via a new
`fakeAgentService.suspendCall`/`resumeSignal` mode, asserts the gauge is unchanged while
suspended and a second request is still rejected, then resolves and asserts release);
`TestMaxConcurrentRunsNoLeakAcrossRepeatedCycles` (20 cap-to-limit cycles, gauge back to
0); `TestResumeOfAdmittedRunNotRejectedEvenWhenCapFull`. All run clean under `-race`.

## PANDO-US-0022 — Graceful drain of in-flight runs on shutdown

Files touched:
- `internal/agui/runtime.go` — `Runtime.draining atomic.Bool`,
  `StartDraining`/`isDraining` (exported/unexported respectively — `StartDraining` is
  called from `cmd/agui_serve.go`); `Runtime.Close` rewritten: starts draining first,
  partitions active runs into suspended vs. waiting, releases every suspended run
  immediately (`pending.cancelAll` first — a HITL wait in `hitl.go` selects on the
  adapter's own base context, not the run's, so cancelling the run's context alone would
  not reach it, exactly mirroring `handleCancelRun`'s existing two-step pattern — then
  `finishRun`), waits up to `Config.ShutdownGrace` for the rest via `run.done`, then
  hard-cancels whatever is left and logs the cut count.
- `internal/agui/server.go` — `handleRun` checks `r.isDraining()` before the admission
  check and answers through the same `rejectOverCapacity` helper.
- `internal/agui/deps.go` / `internal/config/config.go` —
  `Config.ShutdownGrace time.Duration` / `AGUIConfig.ShutdownGrace string` (toml
  `ShutdownGrace`), default 30s; unlike `DisconnectGrace`'s existing pattern, an explicit
  `"0s"` is honoured (`d >= 0`, not `d > 0`) since 0 is a documented deliberate choice
  ("today's immediate cancel"), not a value to discard in favour of the default.
- `cmd/agui_serve.go` — captures `aguiRuntimeCfg := agui.ConfigFromApp(cfg.AGUI)`;
  SIGTERM handler calls `runtime.StartDraining()` before `listener.Shutdown`, and gives
  the listener a `context.WithTimeout(..., aguiRuntimeCfg.ShutdownGrace)` instead of the
  previous hardcoded `5*time.Second`. `listener.Shutdown` still runs before the deferred
  `runtime.Close()` (already true via Go's defer-runs-on-return semantics; preserved,
  not restructured).

**What drain does to a parked/suspended run, and why**: a suspended run (parked on a
permission or frontend-tool prompt) is never waited on — it is checkpointed (its
accumulated messages are already durably persisted incrementally by
`internal/llm/agent`'s `a.messages.Update` calls on every streamed chunk, confirmed by
reading `internal/llm/agent/agent.go:1850-1943`; nothing in this package duplicates that
write) and released immediately regardless of `ShutdownGrace`, because no human is going
to answer a HITL prompt inside a shutdown window and holding the process open for one
would defeat the point of a bounded grace. A run that is actively streaming (not
suspended) gets up to `ShutdownGrace` to finish naturally; one that still hasn't finished
when the grace expires is cancelled and counted in a single `logging.Warn` line.

Tests: `internal/agui/runtime_test.go` (new file) —
`TestDrainingRejectsNewRunsWithoutOpeningAStream`,
`TestCloseWaitsForInFlightRunWithinGrace`,
`TestCloseCancelsRunsThatExceedGraceAndCountsThem` (asserts `RUN_ERROR` on the cut run —
required extending `fakeAgentService` in `server_test.go` to emit an explicit
`AgentEventTypeError` on `ctx.Done()` instead of silently closing its channel, mirroring
what a real provider call does on a cancelled context; without that the pump's
`cancelSignal`-vs-`events`-closing race made the test flaky ~40% of the time),
`TestCloseReleasesSuspendedRunsImmediately`, `TestCloseNoGoroutineLeak` (mirrors
PANDO-US-0017's `TestParkedRunExpiryLeavesNoGoroutine`). Also fixed two latent test-helper
gaps this surfaced: `newTestRuntime`/`newThreadTestRuntime` never set `Runtime.cancel`
(nil-panicked the first time anything called `Close()`), and the goroutine-leak test
needed `newThreadTestRuntime(t, nil)` instead of a real `sql.Open("sqlite3", ":memory:")`
DB — the latter's `database/sql` `connectionOpener` background goroutine (closed only via
`t.Cleanup`, i.e. after the test body's own goroutine-count assertion) produced a false
positive; `newThreadStore(nil)` already degrades cleanly to in-memory-only, so no real DB
is needed for these tests. All clean under `-race`, stress-run 30x with no flakes.

## PANDO-US-0023 — Token provisioning: PANDO_AGUI_TOKEN and --token-file

Files touched:
- `cmd/agui_serve.go` — `resolveAGUIToken(flagToken string, flagSet bool, tokenFile
  string, tokenFileSet bool, envToken string, envSet bool) (token string, generated
  bool, err error)`: precedence `--token` > `--token-file` > `PANDO_AGUI_TOKEN` >
  generated. Each source is identified by *whether it was explicitly set*
  (`cmd.Flags().Changed(...)` / `os.LookupEnv`'s second return), not merely by a
  non-empty value, so an explicitly-empty source (`--token ""`, an empty
  `--token-file`, `PANDO_AGUI_TOKEN=""`) is a startup error rather than a silent
  fall-through to the next source or to a generated token. `--token-file` content is
  read via `os.ReadFile` and whitespace-trimmed. `runAGUIServe` calls this only when
  `!noToken` (`--no-token` semantics unchanged); the stdout banner now prints the token
  only when `generated == true`. `--help` `Example:` and flag descriptions updated to
  match (the old example, `--token "$PANDO_AGUI_TOKEN"`, implied the binary read that
  env var directly when it did not — now it does, and the example shows it used
  without `--token`).

**Where the token can be observed in each provisioning path** (the security-relevant
finding this story exists to close):
- `--token` on the command line: visible in `ps`/`/proc/<pid>/cmdline` to any local user
  who can read the process's own info, and in shell history — this was already true
  before this change and is unavoidable for a CLI flag; unchanged.
- `--token-file`: only the *path* appears in `ps`; the token content is never read into
  an argv-visible form. The file itself must be permissioned by the operator (e.g.
  container secret mount, `0600`) — this tool does not create or chmod it.
  `resolveAGUIToken` never logs the file's content, only (on error) its path.
- `PANDO_AGUI_TOKEN`: not visible in `ps` on Linux (env vars are only visible via
  `/proc/<pid>/environ`, readable by the same user or root); the previous example
  (`--token "$PANDO_AGUI_TOKEN"`) *did* put it in argv via shell expansion — the new
  example avoids that by not passing `--token` at all.
- Generated: printed once to stdout on startup (the only case with no other way for the
  operator to learn it) — visible to whatever captures that process's stdout (a
  terminal, a log file if stdout is redirected, systemd's journal if not
  `StandardOutput=null`). This is unchanged from before and is the documented tradeoff
  of the generated case; an operator who wants it out of any log should use `--token-file`
  or `PANDO_AGUI_TOKEN` instead.
- At no level (`logging.Info/Debug/Warn/Error`, including `internal/agui/runtime.go`'s
  `New()` "AG-UI adapter ready" startup config dump, and `internal/agui/listener.go`'s
  "AG-UI dedicated listener started" line) is the token ever passed as a log argument —
  verified by reading both call sites; confirmed by a new test,
  `TestNewNeverLogsTheToken` in `internal/agui/runtime_test.go`, which substitutes a
  buffer-backed `slog` handler, calls `agui.New` with a known secret in `Deps.Token`, and
  asserts the buffer never contains it.

Tests: `cmd/agui_serve_test.go` (new file) — `TestResolveAGUIToken_Precedence` (table
test covering flag>file>env>generated, file-content newline trimming, missing/unreadable
file, explicitly-empty flag/env-as-errors); `TestResolveAGUIToken_MissingFileNamesThePath`;
`TestResolveAGUIToken_GeneratedTokensAreNotIdentical`.

## Verification

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l` on every touched file — clean (one file needed a `gofmt -w` pass mid-work).
- `go test -race ./internal/agui/... ./internal/config/... ./cmd/...` — clean, including
  a 30x stress run of the new US-0022/0023 tests and 3x of the full targeted suite under
  `-race`, specifically to rule out the flakiness the `fakeAgentService` cancellation-race
  fix (see US-0022 above) was chasing.
- `go test ./...` — clean repo-wide, 101 packages ok, run twice (once before the final
  `doc.go`/`listener_test.go` polish pass, once after).

## Scope notes

Nothing in `internal/rag/`, `internal/api/`, `internal/mesnada/`, `internal/llm/` or
`sdk/` was touched, per the story constraints. `internal/api/server.go`'s existing
`isAGUIPath` prefix-based bypass of both `corsMiddleware` and `authMiddleware` was read
and confirmed sufficient for US-0020's co-mount requirement without any change there.
`internal/llm/agent/agent.go`'s message-persistence-on-every-chunk behaviour was read to
justify US-0022's "checkpointing is already durable" design decision, but not modified.

See also [[PANDO-EP-0004]], [[pando/features/agui-run-parking-reattach-cancel.md]],
[[pando/features/agui-thread-api-and-messages-snapshot.md]].