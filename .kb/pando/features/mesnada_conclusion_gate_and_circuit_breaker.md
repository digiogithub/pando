---
created_at: 2026-07-23T21:25:35.407095298Z
updated_at: 2026-07-23T21:25:35.407095298Z
tags:
    - feature
    - mesnada
    - delegation
    - orchestration
    - hermes
    - integrity
    - breaker
    - p4
    - p6
---
# Feature: P6 anti-hallucination conclusion gate + P4 circuit breaker & respawn guard

Date: 2026-07-23. Items **P6** and **P4** of [[hermes-kanban-swarm-vs-pando-delegation]],
implemented together after [[mesnada_swarm_blackboard_conclusion_forwarding]] (P1+P2) and
[[mesnada_swarm_verifier_gate]] (P3). Both are ON by default; both are switchable off from
config, TUI and WebUI.

Ported concepts: Hermes' `HallucinatedCardsError` completion gate (P6) and its
`consecutive_failures` circuit breaker + `check_respawn_guard` (P4).

---

## P6 — Anti-hallucination conclusion gate

### Why
Pando accepted a delegated task's `Conclusion` verbatim. A subagent could close with
`status: success` while citing files it never wrote or memory refs it never stored. After P3
this became load-bearing: the swarm verifier gate passes **only** on `status == "success"`, so
one hallucinated success opens the synthesizer.

### What it does
`conclusion.Verify` inspects a conclusion **only when its status is `success`**, stats every
*checkable* artifact and resolves every memory ref, and on any positively-missing reference
downgrades the status to `partial` and appends a warning naming what was missing. It never
discards data — same "warn, don't discard" principle as the D1 parser validation
([[delegation_d1_conclusion_schema_validation]]).

**Fail-open on the unverifiable, fail-closed on the disproved.** These refs are ignored, not
treated as missing: URLs / `git@` refs, glob patterns (`*?[`), strings containing whitespace
(prose that landed in the artifacts list), and relative paths when no `BaseDir` is known. A
`stat` error other than "not exists" (permissions, broken mount) also counts as present.

### Files & symbols
- `internal/mesnada/conclusion/verify.go` (new) — `Verify(c, VerifyOptions) VerifyResult`,
  `VerifyOptions{Disabled, BaseDir, Memory}`, `MemoryRefValidator func(string) bool`,
  `VerifyResult{Downgraded, MissingArtifacts, MissingMemoryRefs}`, plus `artifactCheckable` /
  `artifactExists` / `verifyWarning` / `joinCapped`. Constants `StatusSuccess`,
  `StatusPartial`, `maxVerifyRefsListed = 10` (warnings can't bloat the task record).
- `internal/mesnada/orchestrator/orchestrator.go` — `captureConclusion` runs `Verify` right
  after `Enrich`, with `BaseDir = task.WorkDir` and the wired memory validator; a downgrade
  bumps a metric and logs `task_event=conclusion_gate_downgraded`.
- `Config.MemoryRefValidator` + `Orchestrator.memoryRefValidator` (new wiring field).
- `internal/app/app.go` — `makeMemoryRefValidator(ctx, kbStore)`: resolves a ref first as a
  memory key (`KBStore.GetMemoryByKey`), then as a KB document path (`KBStore.GetDocument`).
  **Fail-open**: a nil store or any store error reports the ref as present. Wired only when
  `app.Remembrances != nil`; nil ⇒ artifacts-only verification.

---

## P4 — Circuit breaker + respawn guard

### Why
A delegated task that failed just became `failed`. Nothing counted failures, nothing backed
off: an agent (or a human) could relaunch an unfixable or quota-walled task indefinitely,
burning tokens and provider quota. Pando has no automatic respawn loop, so the guard is
applied at the respawn **entry points** (`Relaunch`, `Retry`).

### The two mechanisms
- **Circuit breaker**: `ConsecutiveFailures` counts failed executions since the last success
  and **accumulates across retry chains** (a `Retry` seeds the new task from the previous
  one, so N failed replicas trip exactly like N failed relaunches). Reaching the limit denies
  further respawns until something actually changes.
- **Respawn guard**, checked in order of severity:
  1. `breaker_tripped` — limit reached; needs a fix, never clears with time.
  2. `blocker_auth` — last failure classified as auth; retrying cannot help.
  3. `rate_limit_cooldown` — last failure was a quota wall and the cooldown has not elapsed;
     reports `RetryAfter`.
  4. `recent_success` — the task is *currently* `completed` and succeeded inside the window;
     a re-run is probably redundant. (Deliberately scoped to completed tasks so a task that
     has failed since is never blocked by an old success.)

### Files & symbols
- `internal/mesnada/breaker/` (new, dependency-free: models + stdlib)
  - `Classify(text) FailureKind` — regex classification into `rate_limit` / `auth` / `other`.
    **Rate-limit wins over auth** when both match (a 429 with an auth-ish word is a quota
    wall; deferring is the safer response). Empty text ⇒ `other`.
  - `Guard(task, Options, now) Decision` — nil task or `Options.Disabled` ⇒ allow.
  - `RecordOutcome(task, Options, now) (tripped bool)` — success resets the counter and
    clears the classification; failure increments + classifies; **cancelled / non-terminal
    statuses are untouched** (a user cancel is not a task failure).
  - `FailureText(task)` — error → raw_error → conclusion summary → output tail.
  - `Decision.Error()` — stable message ending in "Pass force to override."
  - Defaults: `DefaultMaxRetries = 3`, `DefaultRateLimitCooldown = 5m`,
    `DefaultRecentSuccessWindow = 2m`.
- `pkg/mesnada/models/task.go` — `Task.ConsecutiveFailures`, `Task.MaxRetries` (per-task
  override), `Task.LastFailureKind`, `Task.LastFailureAt`, `Task.LastSuccessAt`;
  `SpawnRequest.MaxRetries` + `SpawnRequest.ConsecutiveFailures` (the retry-chain carry-over).
  Persisted by the whole-Task FileStore — no migration.
- `internal/mesnada/orchestrator/orchestrator.go`
  - `recordBreakerOutcome(task)` — called from `onTaskComplete` **after** `captureConclusion`
    (so the classifier can read the conclusion) and **before** `broadcastCompletion` (so
    subscribers see fresh counters). Panic-recovered, persists the task.
  - `breakerOptions()` — config → `breaker.Options`, applying package defaults for zeros.
  - `guardRespawn(task, op, force)` — enforced in `Relaunch` and in `Retry`'s failed branch.
    `force` bypasses and logs `task_event=respawn_forced`; a denial logs
    `task_event=respawn_refused` and bumps a metric.
  - `RespawnDecision(taskID) (breaker.Decision, error)` — public, read-only: lets tools/UI
    explain a refusal *before* attempting a respawn. Does not count as a refusal.
  - `RelaunchOptions.Force` / `RetryOptions.Force`. Changing prompt/engine/model does **not**
    implicitly force — the bypass is always deliberate and logged. Forcing does not reset
    `ConsecutiveFailures`: forcing does not forgive history.
- `logBreakerTripped`, `logRespawnRefused`, `logRespawnForced`, `logConclusionGateDowngraded`.

---

## Metrics
`DelegationMetrics` gains `conclusionGateDowngrades`, `breakerTrips`, `respawnsRefused`
(surfaced in `DelegationMetricsSnapshot` as `conclusion_gate_downgrades`, `breaker_trips`,
`respawns_refused`). Unlike the older recorders these three are **nil-safe**: they are called
from panic-recovered bookkeeping paths where a nil-metrics panic would be silently swallowed
and take the real work with it (this was found by an actually-failing E2E test, see below).

## Config
`MesnadaDelegationConfig` (`internal/config/config.go`):

| Key | Default | Meaning |
|---|---|---|
| `ConclusionGateDisabled` | `false` | inverted opt-out; gate ACTIVE by default |
| `BreakerDisabled` | `false` | inverted opt-out; breaker ACTIVE by default |
| `MaxTaskRetries` | `3` | consecutive failures before the breaker trips |
| `RateLimitCooldown` | `"5m"` | defer after a quota wall |
| `RecentSuccessWindow` | `"2m"` | redundant-re-run window |

**Why the booleans are inverted**: viper's nested-default shadowing drops
`mesnada.delegation.*` defaults when an unrelated `[Mesnada]` sibling key is present, so a
default-true boolean silently becomes false (the `AutoStartWarmInstance` trap). With
`…Disabled` the shadowed zero value *is* the safe/active behaviour and no normalize fallback
is needed. The int/duration knobs do get the usual `normalizeMesnadaDelegationDefaults`
fallback.

Note: unlike the rest of the delegation flags, the **breaker does not require
`Delegation.Enabled`** — it guards the plain task respawn paths too. The conclusion gate does
(conclusions are only captured when delegation is on).

## Surfaces
- Config template `internal/config/init.go` — the 5 keys with explanatory comments.
- REST `internal/api/handlers_settings.go` — GET/PUT `delegation_conclusion_gate`,
  `delegation_breaker`, `delegation_max_task_retries`, `delegation_rate_limit_cooldown`,
  `delegation_recent_success_window`. Exposed as **positive** enabled-flags (inverted on the
  wire from the stored opt-out) so the UI shows an on-by-default toggle. Durations validated
  with `time.ParseDuration`, retries validated `>= 0`.
- TUI `internal/tui/page/settings.go` — 2 toggles + 3 text fields in the Delegation section.
- WebUI `GeneralSettings.tsx` + `types/index.ts` + `settingsStore.ts` + all 7 i18n locales
  (en/es/de/fr/pt/ja/zh).
- Tool `mesnada_spawn` gains a `force` parameter (relaunch path only), documented as "only
  set it after actually changing something". Same for `internal/mesnada/server/tools.go` and
  the REST retry endpoint (`server/gin.go`).

## Verification
- `go build ./...` clean; `go vet` clean on every touched package; `npx tsc --noEmit` clean.
- `go test ./internal/mesnada/... ./internal/config ./internal/api ./internal/app
  ./internal/llm/agent ./internal/llm/tools ./internal/tui/... ./pkg/mesnada/...` — all ok.
- New tests:
  - `internal/mesnada/breaker/breaker_test.go` — `Classify` table (incl. rate-limit-wins-over-auth),
    guard allow/trip/per-task-override/zero-limit, auth never clears, cooldown `RetryAfter`
    arithmetic and expiry, recent-success scoped to completed tasks, `RecordOutcome`
    increment/classify/reset, cancelled+non-terminal no-ops, `Decision.Error()`.
  - `internal/mesnada/conclusion/verify_test.go` — downgrade on a missing artifact (data
    preserved), no downgrade when evidence exists (relative + absolute), unverifiable refs
    ignored, only `success` inspected, disabled/nil no-ops, memory-ref validation, `joinCapped`.
  - `internal/mesnada/orchestrator/breaker_test.go` — `breakerOptions` defaults/overrides,
    `recordBreakerOutcome` persistence + trip metric + disabled no-op, `Relaunch` refused by a
    tripped breaker (task left untouched, metric bumped), `Force` bypass (counter survives),
    `RespawnDecision` reports without counting a refusal, `captureConclusion` end-to-end
    downgrade closing the P3 verifier gate, and the gate's disable switch.

### One real regression found and fixed by the tests
`TestDelegationE2E_ConclusionBlockCapturedAndBroadcast` started failing with "expected
conclusion to be captured": its fixture cited `internal/foo/bar.go` under a fake `/tmp/work`,
so the new gate fired, `o.metrics` was nil in that hand-built Orchestrator, and the panic was
swallowed by `captureConclusion`'s `recover()` — losing the conclusion entirely. Fixed by
(a) making the three new metric recorders nil-safe, (b) giving the fixture a real metrics
value, and (c) rewriting the test to use a real temp work dir with the artifact created, so
it now also proves the gate leaves honest conclusions alone.

## Follow-ups (not implemented)
- P5 durable event log for delegation signals; P7 claim-lease dispatch.
- A real `TaskStatusBlocked`/`Review` lane (still deferred from P3).
- Making `partial` a configurable passing status for the P3 gate.
- Blackboard GC / size cap.
- An automatic respawn path (gate-fail auto-recovery) that would exercise `recent_success`
  the way Hermes' dispatcher does.
