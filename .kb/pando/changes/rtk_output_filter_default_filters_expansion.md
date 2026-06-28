---
created_at: 2026-06-28T22:25:18.708196974Z
updated_at: 2026-06-28T22:25:18.708196974Z
tags:
    - change
    - rtk
    - token-optimization
    - output-filter
    - bash
---
# Change: RTK Output Filter — Default Filter Set Expansion

**Date:** 2026-06-29
**Follows:** `pando/changes/rtk_output_filter_phase1_2.md` (engine + wiring)
**Plan:** `pando/plans/rtk_output_token_reduction_plan.md`

## What was changed
Expanded `internal/llm/tools/outputfilter/defaults.toml` from 2 to 15 built-in
filters, covering the requested ecosystems: docker, rust, golang, kotlin/java,
node.js, bun, deno, swift, python. All filters are conservative (never drop
error/failure/warning lines), use TOML literal-string regexes, and ship inline
`[[tests]]` with exact expected output.

### New filters (by ecosystem)
- **golang**: `go-mod-download` (drops `go: downloading`) — adds to existing `go-test`.
- **docker**: `docker-pull` (drops per-layer Pulling/Downloading/Extracting/Pull complete; keeps Digest/Status/image).
- **rust**: `cargo-build` (drops Compiling/Downloading/Updating/Blocking; keeps warnings/errors/Finished); `cargo-test` (failure-focus: drops `test ... ok`, headers, blanks; keeps failures + `test result`).
- **kotlin/java**: `gradle` (drops `> Task :… UP-TO-DATE/SKIPPED/NO-SOURCE/FROM-CACHE`, downloads, blanks; keeps executed/failed tasks + BUILD line); `maven` (drops `[INFO] Download…`, separator/empty INFO lines, blanks; keeps phases/warnings/errors/BUILD result).
- **node.js**: `npm-install` (npm/pnpm/yarn install|i|ci|add — drops deprecated/notice/funding noise; keeps package counts + audit + errors).
- **bun**: `bun-install` (drops banner + `+ pkg@ver` lines; keeps summary).
- **deno**: `deno-test` (failure-focus: drops `… ok (`, `Check file://`, `running N tests from`; keeps failures + `ok | N passed`).
- **swift**: `swift-build` (drops `[n/m] Compiling/Emitting`, `Building for`; keeps diagnostics + Build complete); `swift-test` (drops `passed (…`, start banners; keeps failed cases + summary).
- **python**: `pytest` (failure-focus: drops ` PASSED`, session headers; keeps failures + summary); `pip-install` (drops `Requirement already satisfied`, Collecting/Downloading/Using cached; keeps installs + errors).

### Match-precedence notes
Filters are sorted by name (deterministic); `match_command` patterns are
mutually exclusive across the set (e.g. `cargo build` vs `cargo test`,
`go test` vs `go mod download`), verified by the inline tests.

## Verification
- `go test ./internal/llm/tools/outputfilter/` — 23 PASS (17 inline filter tests across 15 filters + 6 engine tests).
- `go build ./...` — OK. `go vet ./internal/llm/tools/outputfilter/` — clean.

## Notes / next
- Extend further by appending to `defaults.toml` (always add inline `[[tests]]`), or override per-project via `.pando/filters.toml` / `Bash.OutputFilterPaths`.
- Remaining plan phases: 3 token analytics, 4 structured `go test -json`/lint/tsc parsers (3-tier degradation), 5 TUI/WebUI toggle + i18n, 6 docs/authoring guide.
