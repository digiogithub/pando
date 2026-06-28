---
created_at: 2026-06-29T06:55:56.996252206Z
updated_at: 2026-06-29T06:55:56.996252206Z
tags:
    - feature
    - rtk
    - output-filter
    - token-optimization
    - bash
    - cli
    - docs
---
# Feature: Output Compression Filters (RTK-style token reduction)

**Status:** Phases 1, 2, 4, 5, 6 DONE (Phase 3 token analytics SKIPPED per user). 2026-06-29.
**Plan:** `pando/plans/rtk_output_token_reduction_plan.md`

## Summary
Pando compresses verbose command output at the `bash` tool boundary before it reaches
the model, cutting tokens on noisy tools (test runners, builds, installers, linters) by
~60-90%. Fail-safe, exit-code preserving, conservative (never drops errors), on by default.

## Architecture
Package `internal/llm/tools/outputfilter/`. Two tiers, consulted in order by `Engine.Apply`:

1. **Native structured parsers** (`parsers.go`) — `Parser` iface + `parseTier`
   (Full/Degraded/Passthrough). Built-ins: `go-test-json` (`go test … -json`),
   `golangci-lint` (JSON Issues), `tsc`. A parser that matches but only passthroughs reports
   not-applied and does NOT fall through to TOML filters.
2. **Declarative TOML filters** (`engine.go`/`pipeline.go`/`load.go`/`defaults.toml`) — an
   8-stage line pipeline (strip_ansi → replace → match_output short-circuit →
   strip/keep_lines → truncate_lines_at → head/tail_lines → max_lines → on_empty), matched to
   a command by `match_command` regex, first-match-wins. 15 embedded defaults across git,
   docker, cargo, go, gradle/maven, npm/pnpm/yarn, bun, deno, swift, pip, pytest.

Sources/precedence (high first): project `.pando/filters.toml` → `Bash.OutputFilterPaths` →
embedded defaults. Wired via `bash_outputfilter.go` (lazy singleton, `ResetOutputFilterEngine`
hot-reload) → `bash.go` `Run` + `runWithACP` before `truncateOutput`.

## Configuration
`[Bash] OutputFilterDisabled` (default false = ON) and `OutputFilterPaths []string`.
UI exposes the POSITIVE "enabled" toggle in TUI (`Bash → Output Filter`) and WebUI
(`Settings → General → Command Output Filter`); the disabled flag is read live per bash call.

## Tool-result metadata
`BashResponseMetadata` carries `output_filter` (matched filter/parser name),
`output_filter_chars_before`, `output_filter_chars_after` (all omitempty). `applyOutputFilter`
returns `outputFilterResult{Output,Name,Before,After}`.

## CLI: `pando filter test [file]`
`cmd/filter.go` — validates the inline `[[filters.<name>.tests]]` in a filter TOML file
(or the built-in defaults when no arg). Prints PASS/FAIL per case with a got/want diff,
exits non-zero on any failure (CI-friendly), `SilenceUsage` so a failed case isn't treated
as misuse. Backed by public API in `outputfilter/testrunner.go`:
`LoadFileFilters(path, data) (*Engine, error)` (single file, NO embedded defaults) and
`(*Engine).RunInlineTests() []FilterTestResult`.

## Docs
- Authoring guide: `docs/output-filters.md` (schema table, pipeline order, inline-test
  example, worked git-status example, tips).
- README: Features bullet + "Output compression filters (token reduction)" usage subsection.

## Tests
`internal/llm/tools/outputfilter/{engine_test,parsers_test,testrunner_test}.go` — engine
routing, all 17 inline filter tests, the 3 parsers, and the file-loader/inline-runner API.

## Change docs
`rtk_output_filter_phase1_2.md`, `…default_filters_expansion.md`,
`…phase4_structured_parsers.md`, `…phase5_ui_i18n_metadata.md`, `…phase6_docs_cli.md`.
