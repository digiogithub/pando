---
created_at: 2026-06-28T22:32:54.272632336Z
updated_at: 2026-06-28T22:32:54.272632336Z
tags:
    - change
    - rtk
    - output-filter
    - token-optimization
    - parsers
    - phase4
---
# Change: RTK Output Filter — Phase 4 (native structured parsers)

**Date:** 2026-06-29
**Author:** Claude (Opus 4.8)
**Phase:** 4 of the RTK output-token-reduction plan (`pando/plans/rtk_output_token_reduction_plan.md`). Phase 3 (token analytics) was explicitly SKIPPED per user request.

## What changed
Added a second compression tier to the `outputfilter` engine: native, code-driven
**structured parsers** for commands whose output is not well served by the
line-oriented TOML pipeline (test runners, linters, compilers). Parsers implement
RTK's 3-tier degradation internally — **Full** (structured JSON/NDJSON → failure-focused
summary) → **Degraded** (best-effort regex grep) → **Passthrough** (raw, unchanged).

Engine precedence is now: **native parsers first**, then TOML filters. When a parser
matches a command and produces a Full or Degraded result, that result wins; if it can
only passthrough, `Apply` reports `applied=false` and returns the raw output (and does
NOT fall through to TOML filters, since the parser claimed the command).

### Built-in parsers
- **go-test-json** — matches `go test … -json`. Parses NDJSON events, keeps only
  failing tests (with their cleaned output, stripping `=== RUN/CONT/PAUSE`, `--- PASS/SKIP`,
  `PASS`, `ok`, `FAIL` noise), surfaces package-level BUILD errors and any non-JSON
  leftovers, and emits `SUMMARY: X passed, Y failed, Z skipped (N packages, M failed)`.
  Degrades (regex grep of FAIL/panic/`.go:`/`#`) when there are zero JSON records.
- **golangci-lint** — matches `golangci-lint`. Full parse when output is JSON
  (`{"Issues":[…]}`): one line per issue `file:line:col [linter] text`, capped at 60 with
  an omitted-count note, plus `SUMMARY: N issues across M files (linter: count, …)`.
  Degrades by grepping `:\d+:\d+:` diagnostic lines for text-mode output.
- **tsc** — matches `tsc` (word-boundary, incl. `npx tsc`, `node_modules/.bin/tsc`).
  Parses `file(line,col): error TSxxxx: msg` diagnostics → `file:line:col [TSxxxx] msg`
  (capped 60) + `SUMMARY: N errors in M files`. Degrades by grepping `error TS\d+`.

All parsers are **fail-safe**: `parse` never errors, only returns a tier; an unmatched or
unparseable output passes through untouched.

## Files touched
- `internal/llm/tools/outputfilter/parsers.go` (NEW) — `Parser` interface, `parseTier`
  (Full/Degraded/Passthrough), `defaultParsers()`, shared `degradedGrep`/`sortedKeys`
  helpers, and the three parsers (`goTestJSONParser`, `golangciLintParser`, `tscParser`).
- `internal/llm/tools/outputfilter/engine.go` — `Engine.parsers []Parser`; `New()` seeds
  built-in parsers; `Apply` now consults parsers before filters with the passthrough rule.
- `internal/llm/tools/outputfilter/load.go` — `LoadDefaults`/`Load` seed `defaultParsers()`.
- `internal/llm/tools/outputfilter/parsers_test.go` (NEW) — match/Full/Degraded/Passthrough
  tests for all three parsers + engine precedence + passthrough fall-through.

No config or wiring changes were needed: parsers run through the existing
`bash_outputfilter.go` → `bash.go` path and honour the same `Bash.OutputFilterDisabled`
toggle. Default ON, fail-safe, additive.

## Verification
- `go test ./internal/llm/tools/outputfilter/ -v` — all pass (15 inline TOML filter tests +
  6 engine tests + 12 new parser tests).
- `go build ./...` — OK. `go vet ./internal/llm/tools/outputfilter/` — clean.
- `go test ./internal/llm/tools ./internal/llm/agent ./internal/api` — all pass.

## Notes / next
- Phase 3 (token analytics / `pando stats`) skipped by user decision.
- Remaining: Phase 5 (TUI/WebUI toggle + 7-locale i18n + surface filter/parser name &
  savings in tool-result metadata), Phase 6 (docs + `.pando/filters.toml` authoring guide).
- Possible future parsers: `eslint --format json`, `jest --json`, `pytest` JSON report,
  `cargo test --message-format json`. They slot in via `defaultParsers()`.
