---
created_at: 2026-06-29T06:56:15.747143415Z
updated_at: 2026-06-29T06:56:15.747143415Z
tags:
    - change
    - rtk
    - output-filter
    - token-optimization
    - cli
    - docs
    - phase6
---
# Change: RTK Output Filter — Phase 6 (docs, authoring guide, `pando filter test`)

**Date:** 2026-06-29
**Author:** Claude (Opus 4.8)
**Phase:** 6 (final) of `pando/plans/rtk_output_token_reduction_plan.md`. Follows Phase 5
(`rtk_output_filter_phase5_ui_i18n_metadata.md`).

## What was implemented
1. **`pando filter test [file]` CLI** — validates the inline `[[tests]]` in an output-filter
   TOML file (or the built-in defaults with no arg). PASS/FAIL per case + got/want diff on
   failure; exits non-zero if any case fails (CI-friendly).
2. **Public test-runner API** in the `outputfilter` package so the CLI doesn't reach into
   unexported internals.
3. **Docs**: a full authoring guide + README Features bullet and usage subsection.

## Files & symbols touched

### New public API
- `internal/llm/tools/outputfilter/testrunner.go`
  - `FilterTestResult{Filter,Test,Passed,Got,Expected,Err}`.
  - `LoadFileFilters(path string, data []byte) (*Engine, error)` — builds an engine from a
    single TOML file WITHOUT the embedded defaults (so authors test exactly their file) and
    WITHOUT seeding native parsers (only declarative filters carry inline tests). Fail-safe.
  - `(*Engine) RunInlineTests() []FilterTestResult` — runs every filter's inline `[[tests]]`
    via the (unexported) `f.run`, trailing-newline-insensitive comparison; a filter that
    reports failure on its input yields a failed result with `Err` set. Never panics.

### CLI
- `cmd/filter.go` (new) — `filterCmd` parent ("filter") + `filterTestCmd` ("test [file]",
  `MaximumNArgs(1)`, `SilenceUsage: true`). Reads the file (or `LoadDefaults`), runs
  `RunInlineTests`, prints results via `indent()` helper, returns an error on any failure.
  Registered on `rootCmd` in `init()`.

### Docs
- `docs/output-filters.md` (new) — guarantees, the two tiers (parsers + TOML), sources &
  precedence, full schema table, pipeline order, inline-test example, `pando filter test`
  usage, worked git-status example, authoring tips.
- `README.md` — new Features bullet ("Output Compression (token reduction)") and a
  "### Output compression filters (token reduction)" usage subsection (config snippet,
  on/off toggle, `pando filter test` examples, link to docs).

### Tests
- `internal/llm/tools/outputfilter/testrunner_test.go` (new) —
  `TestLoadFileFiltersRunsInlineTests` (one passing + one deliberately failing case;
  asserts results, got value) and `TestLoadFileFiltersNoDefaults` (empty file → no inline
  tests and defaults must NOT leak in).

## Verification
- `go build ./...` OK; `go vet ./cmd/... ./internal/llm/tools/outputfilter/` clean.
- `go test ./internal/llm/tools/... ./cmd/` all pass.
- Manual: `pando filter test` → 17 passed / 0 failed on built-in defaults; a custom file with
  a wrong `expected` prints FAIL + diff and exits 1, with no usage dump (SilenceUsage).

## Phase status
Phase 6 DONE → the whole RTK output-token-reduction feature is complete (Phases 1,2,4,5,6;
Phase 3 token analytics SKIPPED per user). Feature doc: `pando/features/output_filters.md`.
