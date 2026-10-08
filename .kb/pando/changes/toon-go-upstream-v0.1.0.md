---
created_at: 2026-10-05T20:07:08.946390898Z
updated_at: 2026-10-05T20:07:08.946390898Z
tags:
    - change
    - deps
---
# Switch to upstream toon-format/toon-go v0.1.0 (TOON spec v4.1)

Date: 2026-10-05. Commit 147303cc91ab (change unoznnyo) on `main`, pushed.

## What changed
- go.mod: removed `replace github.com/toon-format/toon-go => github.com/madeindigio/toon-go v0.0.0-20260824122047-953870f65a68` (fork = head of upstream PR #19).
- go.mod: require bumped from `v0.0.0-20251108125615-44b4cd22477f` to `v0.1.0` (tag 2ef047f, contains upstream PR #25 bf156d1 "implement TOON specification v4.1"). Upstream main has one later commit (#27, module trim refactor) that is not needed.
- go.sum updated by `go mod tidy`.
- No Go code changes needed: Pando only uses `toon.Marshal` (internal/llm/tools/json_output.go) and `toon.DecodeString` (internal/agui/subagents.go, internal/mcpgateway/catalog_pagination_test.go); both unchanged upstream.

## Behaviour changes inherited from #25 review fixes
- Numbers overflowing float64 (e.g. `1e999`) decode as strings.
- Strict mode rejects ill-formed UTF-8 input.
- Root string starting with U+FEFF is quoted on encode.
- O(1) next-non-blank-line lookup (perf).

## Verification
`go build ./...`, `go vet ./...`, `go test ./...` all pass (no failures locally).

Related: [[fix_agui_state_per_thread_toon_results]]
