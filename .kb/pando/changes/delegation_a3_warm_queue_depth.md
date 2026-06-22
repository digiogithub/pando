---
created_at: 2026-06-22T11:17:17.261288761Z
updated_at: 2026-06-22T11:17:17.261288761Z
tags:
    - change
    - mesnada
    - delegation
    - warm-instance
    - queue
    - concurrency
    - config
---
# Change: A3 — Bounded warm-instance delegation queue (WarmQueueDepth)

Date: 2026-06-22. Backlog item A3 from
`pando/plans/delegation_future_improvements.md`. Default-OFF (queueDepth 0 =
today's cold-fallback behaviour). See feature
`pando/features/delegated_conclusions_resurrection.md`.

## What changed / motivation
Before A3, when a warm per-project instance was already at `MaxConcurrent`
in-flight delegated sessions, the next delegated task fell back to the cold path
(cold-spawned a `pando -p` CLI), losing the warm-context benefit under load. A3
adds an optional bounded FIFO queue per `Instance`: over the cap, up to
`WarmQueueDepth` delegations BLOCK waiting for a freed slot instead of
cold-falling-back; beyond cap+queueDepth they still fall back. A freed slot wakes
a queued delegation. Strictly default-off: `WarmQueueDepth = 0` keeps the exact
prior behaviour.

## Files / symbols touched
- `internal/project/instance.go`
  - New `Instance.cond *sync.Cond` + `Instance.waiters int` (guarded by `mu`).
  - New `acquireDelegationSlotOrQueue(ctx, max, queueDepth) bool`: immediate
    acquire under cap; else if queueDepth>0 and waiters<queueDepth, enter a
    bounded FIFO and `cond.Wait()` until a slot frees, ctx is cancelled, or the
    instance starts closing; else return false (cold fallback). A watcher
    goroutine broadcasts on `ctx.Done()` (sync.Cond has no ctx-aware Wait). The
    pre-existing non-blocking `acquireDelegationSlot(max)` is retained (used by
    tests / GC).
  - `releaseDelegationSlot()` now `cond.Broadcast()`s so a freed slot is handed to
    a queued waiter.
  - New `beginCloseAndWake()`: sets `closing` + broadcasts (used by Stop) so
    queued waiters bail to the cold path even though Stop does not require
    inflight==0 (unlike `tryBeginClose`).
- `internal/project/delegation.go` — `WarmDelegate(...)` gains a `queueDepth int`
  param and calls `acquireDelegationSlotOrQueue(ctx, maxConcurrent, queueDepth)`.
- `internal/project/manager.go` — `StopReport` calls `inst.beginCloseAndWake()`
  before `inst.cancel()` so a stopped instance releases its queued delegations.
- `internal/config/config.go` — `MesnadaDelegationConfig.WarmQueueDepth int`
  (json `warmQueueDepth`); viper default
  `mesnada.delegation.warmQueueDepth = 0`. No normalize entry needed (0 IS the
  intended default, so viper nested-default shadowing — item A1 — is harmless
  here).
- `internal/config/init.go` — `WarmQueueDepth = 0` in the generated
  `[Mesnada.Delegation]` template.
- `internal/app/app.go` — `makeWarmTargetResolver` reads
  `cfg.Mesnada.Delegation.WarmQueueDepth` and passes it to `WarmDelegate`.
- `internal/tui/page/settings.go` — "Delegation Warm Queue Depth" int field +
  save case (>=0 validation).
- `internal/api/handlers_settings.go` — `delegation_warm_queue_depth` on GET
  response + PUT request (pointer, >=0 validation) + apply.
- web-ui: `types/index.ts` (`delegation_warm_queue_depth: number`),
  `stores/settingsStore.ts` (default 0), `components/settings/GeneralSettings.tsx`
  (number TextInput, disabled unless enabled && reuse-warm), and
  `settings.general.delegationWarmQueueDepth` in all 7 i18n locales
  (en/es/de/fr/pt/ja/zh).

## Concurrency notes
- Queue ordering is best-effort (release broadcasts; all waiters re-race) but the
  queue DEPTH is strictly bounded by `waiters < queueDepth`.
- Lock order is preserved: `acquireDelegationSlotOrQueue` only touches `inst.mu`
  (never `Manager.mu`); `StopReport` holds `Manager.mu` then `inst.mu` via
  `beginCloseAndWake` — no inversion.
- Cold-fallback sentinel unchanged: a refused/cancelled queue attempt returns
  `ErrWarmCapReached`, already mapped to `orchestrator.ErrNoWarmTarget`.

## Verification
- New `internal/project/instance_queue_test.go` (race-clean): immediate acquire;
  cold-fallback when queueDepth=0; queue-then-proceed + bounded-FIFO refusal of a
  third over cap+depth + freed-slot handoff; ctx-cancel of a queued waiter;
  beginCloseAndWake unblocks a queued waiter.
- Existing `WarmDelegate` call sites updated with `queueDepth=0` (behaviour
  unchanged); `TestWarmDelegateCapReached` still passes.
- `go build ./...`, `go test -race ./internal/project`, and suites
  project/config/app/api/llm-agent/tui-page/mesnada-* all green; gofmt clean;
  web-ui JSON valid + `tsc --noEmit` clean.
