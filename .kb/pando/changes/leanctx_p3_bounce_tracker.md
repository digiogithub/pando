---
created_at: 2026-06-30T13:47:13.011822767Z
updated_at: 2026-06-30T13:47:13.011822767Z
tags:
    - change
    - lean-ctx
    - token-optimization
    - read-modes
    - bounce-tracker
    - phase3
---
# Change: lean-ctx Phase 3 — Adaptive auto-mode safety (bounce tracker)

**Date:** 2026-06-30
**Plan:** `pando/plans/leanctx_context_intelligence_plan.md` (Phase 3)
**Builds on:** P1 (read modes) + P2 (content-hash dedup) — `pando/changes/leanctx_p1_p2_read_modes_and_dedup.md`

## Goal
Make the `view` `auto` read mode safe to enable by default by learning, within a
session, when compression backfires — a "bounce" = a compressed read
(signatures/map) of a path followed by a full read of the same path. High-bounce
paths/extensions get their next `auto` read escalated toward full
(signatures → map → full); wasted compressed bytes are tallied for the Phase-5
ledger.

## What changed

### New: `internal/llm/tools/readmode/bounce.go`
- `BounceTracker` (own `sync.Mutex`, concurrency-safe):
  - `RecordCompressed(path, mode, renderedBytes)` — arms a pending bounce for the
    path and tentatively credits the extension Beta posterior toward "compression
    held" (α++).
  - `RecordFull(path) bool` — if a compressed read of the same path was pending,
    counts a bounce: per-path + per-extension counters++, `wastedBytes +=
    pending.bytes`, posterior shifts toward "bounced" (α--, β++). Returns true.
  - `Upgrade(path, base, learning) Mode` — escalates a concrete compressed mode
    along the `signatures → map → full` ladder by `max(pathBounces, extBounces)`
    steps. When `learning` and no hard bounces yet, a per-extension Beta posterior
    bounce-rate ≥ 0.5 (≥3 observations) adds one extra step. ModeFull/ModeAuto
    returned unchanged.
  - `Stats() BounceStats {Bounces, WastedBytes, HotPaths, HotExts}`, `Reset()`.
  - Helpers: `escalate` (ladder, clamped), `normPath` (filepath.Clean),
    `extOf` (lower ext or `<none>`). All methods nil-safe.
- Deterministic by default (escalation driven only by hard bounce counts);
  `ReadModeLearning` only turns on the Beta layer.

### `internal/llm/tools/cache.go` — fidelity-aware dedup + tracker home
- `SessionCache` gains `bounce *readmode.BounceTracker` (init in `NewSessionCache`,
  reset in `Clear`); accessor `BounceTracker()`.
- **Fixed a latent P1↔P2 interaction bug:** the old dedup hashed the *raw* window
  and would collapse a later `full` re-read to a stub referencing an earlier read
  that had only delivered *signatures* — telling the agent "you already have this"
  when it didn't. `RecordRead` now takes a `deliveredMode` arg and stores it on the
  window record. New `ReadDedupUpgraded` status: identical content but the caller
  now wants higher fidelity than was delivered ⇒ do **not** collapse, bump the
  recorded fidelity, let the real read proceed. `canCollapseFidelity(prev, now)`:
  collapse only when `prev=="full"` (covers everything) or `prev==now`.
  `normalizeDeliveredMode` maps to full/signatures/map.
- `CacheStats` gains `Bounces` + `BounceWastedBytes` (populated from the tracker in
  `Stats()`); `cache_stats` tool prints an "Auto-mode bounces (compressed→full)" line.

### `internal/llm/tools/view_modes.go` — resolve / record split
- New `resolveViewMode(ctx, path, sizeBytes, requested, diagActive) Mode`: resolves
  the requested/default mode to a concrete tier and, **only for `auto`**, applies
  `BounceTracker.Upgrade` (honoring `ReadModeLearningEnabled`). Explicit
  signatures/map are honored verbatim.
- `dedupViewRead` now takes the resolved `nowMode` and threads it into
  `RecordRead`; `ReadDedupUpgraded`/`ReadDedupNew` ⇒ handled=false.
- `renderViewMode` simplified to take an already-resolved concrete mode
  (parse + render + safeguard `len(rendered) >= len(content)`).
- Bounce hooks: `bounceTracker(ctx)`, `recordCompressedRead(ctx, path, mode, bytes)`,
  `recordFullRead(ctx, path)`.

### `internal/llm/tools/view.go` — both Run + runWithACP
- Reordered: LSP/diagnostics computed **before** dedup so `auto` keeps
  diagnostic-active files full and dedup keys on the about-to-be-delivered fidelity.
- Flow: `resolveViewMode` → fidelity-aware `dedupViewRead` → compressed render
  (`recordCompressedRead`) or full branch (`recordFullRead`). `full` default path
  stays byte-identical.

### `internal/config/config.go`
- `ReadModeLearningEnabled()` helper (nil-safe; off by default). Clarified the
  `ReadModeLearning` doc: the bounce tracker is always active for `auto`; the flag
  only enables the Beta escalation layer.

## Behavior / safety
- Escalation only ever moves toward **more** fidelity (safe direction); worst case
  is less compression, never missing bytes.
- Pure session-local state; nothing persisted (per-project persistence noted as
  future work in the plan). `full`-default and dedup-disabled paths unchanged.

## Verification
- New `internal/llm/tools/readmode/bounce_test.go`: escalate ladder, single/double
  bounce escalation + waste tally, per-extension propagation, no-bounce-without-
  pending, learning off-by-default vs synthetic posterior escalation, reset,
  `-race` concurrent hammer, nil-safety.
- New `TestRecordReadFidelityAware` in `cache_dedup_test.go`: signatures→signatures
  collapses; signatures→full ⇒ `ReadDedupUpgraded` (no mask), label stable;
  post-full re-read collapses; only genuine collapses counted as dedup hits.
- Updated existing `RecordRead` callers to the new `deliveredMode` arg.
- Green: `go build ./...`; `go test -race ./internal/llm/tools/readmode/...
  ./internal/llm/tools/`; `go test ./internal/llm/agent ./internal/api`.
- Pre-existing config failures (NOT from this change, confirmed by reverting the
  in-progress `init.go`): `TestDefaultConfigTemplateEnablesPandoPreferredDefaults`
  (from the uncommitted init.go template refactor) and
  `TestMesnadaDelegationWarmDefaultsUnderShadowing` (documented viper
  nested-default shadow, fails on HEAD too).

## Follow-ups (per plan)
- P4 code property graph, P5 savings ledger (`pando gain`) — the bounce
  `WastedBytes` is exposed for P5 to subtract. No new settings-UI knob needed; the
  existing `ReadModeLearning` toggle already lives in the Token Optimization section.
