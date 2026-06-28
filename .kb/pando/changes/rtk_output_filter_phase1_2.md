---
created_at: 2026-06-28T22:18:24.059911933Z
updated_at: 2026-06-28T22:18:24.059911933Z
tags:
    - change
    - rtk
    - token-optimization
    - output-filter
    - bash
    - feature
---
# Change: RTK-style Output Filter — Phases 1 & 2

**Date:** 2026-06-29
**Plan:** `pando/plans/rtk_output_token_reduction_plan.md`
**Status:** Phases 1 & 2 COMPLETE. Phases 3-6 pending.

## What was implemented
A declarative, RTK-inspired output-compression engine that maps a shell command
to a TOML-defined filter and runs an 8-stage line-oriented pipeline to strip
noise from `bash` tool output before it reaches the LLM. Fail-safe and on by
default (opt-out).

### Phase 1 — Core engine (`internal/llm/tools/outputfilter/`)
- `engine.go` — `Filter`/`ReplaceRule`/`MatchRule`/`FilterTest` structs, `Engine`,
  `Filter.compile` (precompiles all regexes; rejects invalid filters), `Engine.Apply(command, output) -> (result, applied, name)` (first match wins, never panics, returns raw on failure).
- `pipeline.go` — the 8 stages: strip_ansi → replace[] → match_output[] (whole-blob short-circuit, with optional `unless`) → strip/keep_lines → truncate_lines_at → head/tail_lines → max_lines → on_empty. ANSI stripping via `charmbracelet/x/ansi`.
- `load.go` — `parseFile` (pelletier/go-toml v2; bad filters skipped, sorted by name for determinism), `LoadDefaults`, `Load(paths...)` (override paths first, then embedded defaults; missing files skipped; malformed contribute an error but never abort — fail-safe). `//go:embed defaults.toml`.
- `defaults.toml` — two conservative, well-tested built-ins:
  - `git-status`: short-circuits a clean tree to one line; strips `(use "git ...")` hints and blank lines on a dirty tree. Never drops change entries.
  - `go-test`: failure-focused — strips passing-package `ok` lines and `[no test files]` lines; `on_empty` summarises an all-pass run; preserves all FAIL/panic/error lines.
  - Pattern fields use TOML literal strings (`'...'`) so regexes need no escaping. Inline `[[filters.<name>.tests]]` (name/input/expected) self-test each filter.
- `engine_test.go` — compile check, an inline-test runner that executes every embedded filter's `[[tests]]`, no-match-returns-raw, command routing, user-path-overrides-builtin precedence, and bad-filter-skipped.

### Phase 2 — Wiring + config
- `internal/config/config.go` `BashConfig`: added `OutputFilterDisabled bool` (default = enabled) and `OutputFilterPaths []string`.
- `internal/llm/tools/bash_outputfilter.go` (new): lazy `sync.Once` singleton engine built from embedded defaults + project-local `.pando/filters.toml` + `Bash.OutputFilterPaths` (project path takes precedence); `getOutputFilterEngine`, `ResetOutputFilterEngine` (hot reload), `applyOutputFilter(command, output)` (fail-safe; honours `OutputFilterDisabled`; debug-logs chars before/after).
- `internal/llm/tools/bash.go`: call `applyOutputFilter(params.Command, stdout/output)` before `truncateOutput` in BOTH the host path (`Run`) and the ACP terminal path (`runWithACP`), so caching/truncation see already-compressed text. Exit-code and stderr handling untouched.

## Why
Shell-command output (test runs, git, builds) is a dominant source of token bloat
in agent sessions. RTK reports 60-90% reduction by compressing these outputs. This
ports RTK's most maintainable mechanism (the declarative TOML filter engine) so new
command filters can be added without recompiling, with project/user overrides.

## Verification
- `go test ./internal/llm/tools/outputfilter/` — all pass (incl. embedded inline filter tests).
- `go build ./...` — OK.
- `go vet ./internal/llm/tools/outputfilter/ ./internal/llm/tools/` — clean.
- `go test ./internal/llm/tools ./internal/llm/agent ./internal/api` — all pass (verified commands per CLAUDE.md).
- Ad-hoc end-to-end: `Apply("go test ./...", <ok lines>)` → collapses to the all-pass summary (90→46 chars on a trivial 3-line sample; scales with package count).

## Notes / next
- Default ON because the engine is fail-safe and additive; disable via `Bash.OutputFilterDisabled`.
- Phase 3 (token analytics / `pando stats`), Phase 4 (structured `go test -json`/lint/tsc parsers with 3-tier degradation), Phase 5 (TUI/WebUI toggle + i18n + surface savings in metadata), Phase 6 (docs + `.pando/filters.toml` authoring guide) remain.
- Built-in filter set is intentionally small (2) for this low-complexity phase; extend via `defaults.toml` with inline tests.
