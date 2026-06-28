---
created_at: 2026-06-29T06:41:37.479071068Z
updated_at: 2026-06-29T06:56:59.139581278Z
tags:
    - plan
    - rtk
    - token-optimization
    - output-filter
    - bash
---
# Implementation Plan: RTK-style Output Token Reduction in Pando

**Date:** 2026-06-29
**Author:** Claude (Opus 4.8)
**Goal:** Reduce LLM token consumption from command/tool outputs (mainly the `bash` tool) by 60-90%, porting the best ideas from RTK (https://github.com/rtk-ai/rtk) into Pando.

## Context & Prior Work
- Prior deep analysis: `pando/analysis/rtk-deep-analysis.md` (RTK architecture, 12 filter strategies, fail-safe, exit-code preservation).
- Prior integration sketch: `pando/analysis/rtk-leanctx-integration-plan.md` (proposed `outputcompressor` pkg + token analytics, regex-based).
- DeepWiki review of `rtk-ai/rtk` (2026-06-29) clarified RTK's two complementary mechanisms:
  1. **Rust per-command modules** (`src/cmds/`) with the `OutputParser` trait + 3-tier degradation (Full JSON → Degraded regex → Passthrough truncated). Used for complex/structured output (test runners, lint, tsc).
  2. **Declarative TOML filter engine** (`src/core/toml_filter.rs`, `src/filters/*.toml`) — an **8-stage pipeline** for line-oriented output, addable without writing code. This is the most maintainable model to port to Go first.

## Design Decision
Port RTK's **TOML filter engine** as the foundation (declarative, hot-reloadable, no recompile to extend), then add structured/Go-native parsers later as a second tier. Integrate at the **`bash` tool** output boundary (both the host path and the ACP terminal path) because that is where the command string is known (needed for command→filter mapping) and where raw output is largest. The compressor runs *before* the existing `truncateOutput`/head/tail/cache-interceptor so caching stores already-compressed text.

### Core principles (inherited from RTK)
1. **Fail-safe / never block** — any parse/regex error returns the raw output unchanged.
2. **Exit-code preservation** — compression touches only the text shown to the LLM, never exit codes or control flow.
3. **Transparent** — opt-out, no change to agent prompting required. Default ON because it is fail-safe and additive.
4. **Deterministic** — first matching filter wins; project/user filters override built-ins.

## TOML Filter Engine — 8-stage pipeline (per matched filter)
1. `strip_ansi` — remove ANSI escapes.
2. `replace[]` — chained per-line regex substitutions (`pattern`/`replacement`).
3. `match_output[]` — whole-blob short-circuit: if a pattern matches, return `message` (with optional `unless`).
4. `strip_lines_matching[]` / `keep_lines_matching[]` — line filter (mutually exclusive).
5. `truncate_lines_at` — cap each line length.
6. `head_lines` / `tail_lines` — keep first/last N lines.
7. `max_lines` — absolute cap on total lines.
8. `on_empty` — message if output became empty.

### Filter schema (`[filters.<name>]`)
`description`, `match_command` (regex on full command), `strip_ansi`, `filter_stderr`, `strip_lines_matching[]`, `keep_lines_matching[]`, `replace[]`, `match_output[]`, `truncate_lines_at`, `max_lines`, `head_lines`, `tail_lines`, `on_empty`, plus inline `[[filters.<name>.tests]]` (`name`/`input`/`expected`) for self-testing.

### Filter sources & precedence
1. Embedded built-in defaults (`defaults.toml`, go:embed).
2. User-global filters (config `Bash.OutputFilterPaths`).
3. Project-local `.pando/filters.toml`.
Higher precedence loaded first; first `match_command` hit wins.

## Native structured parsers (Phase 4 — second tier, ahead of TOML filters)
Code-driven `Parser`s for structured output (test runners, linters, compilers), each with
RTK's internal 3-tier degradation: **Full** (JSON/NDJSON → failure-focused summary) →
**Degraded** (regex grep) → **Passthrough** (raw). Engine precedence: parsers first, then
TOML filters; a parser that only passthroughs reports not-applied and does not fall through.
Built-ins: `go-test-json` (`go test … -json`), `golangci-lint` (JSON `Issues`), `tsc`
(`file(line,col): error TSxxxx`). Live in `parsers.go`, seeded by `defaultParsers()`.

## Phases

### Phase 1 — Core TOML filter engine + embedded defaults  *(DONE)*
- New pkg `internal/llm/tools/outputfilter/` (`engine.go`, `pipeline.go`, `load.go`,
  `defaults.toml`, `engine_test.go`). `Apply(command, output)` first-match-wins, fail-safe.

### Phase 2 — Wire into the `bash` tool + config toggle  *(DONE)*
- `config.BashConfig.OutputFilterDisabled` (default = enabled) + `OutputFilterPaths`.
- Lazy singleton engine (`bash_outputfilter.go`, `ResetOutputFilterEngine` hot-reload),
  called in `bash.go` `Run` and `runWithACP` before `truncateOutput`.
- Default set later EXPANDED to 15 filters across 10 ecosystems
  (`pando/changes/rtk_output_filter_default_filters_expansion.md`).

### Phase 3 — Token analytics  *(SKIPPED — user decision 2026-06-29)*
- Not implemented. Was: in-memory ring + optional SQLite token tracker + `pando stats`/`gain`.

### Phase 4 — Structured/native parsers  *(DONE 2026-06-29)*
- `parsers.go`: `Parser` iface + `parseTier` (Full/Degraded/Passthrough) + `go-test-json`,
  `golangci-lint`, `tsc`. Wired into `Engine` (parsers before filters). Tests in
  `parsers_test.go`. Change doc: `pando/changes/rtk_output_filter_phase4_structured_parsers.md`.

### Phase 5 — UI & i18n  *(DONE 2026-06-29)*
- TUI + WebUI settings toggle for `OutputFilterDisabled` (exposed as the positive "enabled";
  config keeps the negative flag). 7-locale i18n (`settings.general.outputFilter[Description]`).
- Filter/parser name + char savings surfaced in tool-result metadata: `BashResponseMetadata`
  gained `output_filter`, `output_filter_chars_before`, `output_filter_chars_after`;
  `applyOutputFilter` now returns an `outputFilterResult` struct. Backend wiring in
  `bash.go`/`bash_outputfilter.go`/`settings.go`(TUI)/`handlers_settings.go`(API); frontend in
  `GeneralSettings.tsx`/`settingsStore.ts`/`types/index.ts` + locales.
- Change doc: `pando/changes/rtk_output_filter_phase5_ui_i18n_metadata.md`.

### Phase 6 — Docs & extensibility  *(DONE 2026-06-29)*
- `pando filter test [file]` CLI (`cmd/filter.go`) validating inline `[[tests]]` (file or
  built-in defaults), CI-friendly exit code, `SilenceUsage`. Public API
  `outputfilter/testrunner.go` (`LoadFileFilters(path,data)` single-file no-defaults loader,
  `(*Engine).RunInlineTests() []FilterTestResult`), tested in `testrunner_test.go`. Authoring
  guide `docs/output-filters.md` + README Features bullet & usage subsection. Feature doc
  `pando/features/output_filters.md`. Change doc `pando/changes/rtk_output_filter_phase6_docs_cli.md`.

## Expected Impact
Shell-output tokens for covered commands reduced 60-90%; additive & fail-safe; foundation for native parsers and (deferred) analytics.

## Status
- Phase 1: DONE.
- Phase 2: DONE (+ filter expansion to 15 filters).
- Phase 3: SKIPPED (user).
- Phase 4: DONE.
- Phase 5: DONE.
- Phase 6: DONE.
- **Whole feature COMPLETE** (Phases 1,2,4,5,6; Phase 3 skipped). Feature doc: `pando/features/output_filters.md`.
