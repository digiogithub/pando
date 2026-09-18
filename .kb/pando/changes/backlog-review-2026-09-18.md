---
created_at: 2026-09-18T12:42:47.348617422Z
updated_at: 2026-09-18T12:42:47.348617422Z
tags:
    - backlog
    - review
---
# Backlog review of open PANDO items (2026-09-18)

Reviewed the 15 non-terminal items in `.kb/.pmngr/`. The gintrack MCP in this session is bound to the GIT project, so the files were edited directly, not through the MCP.

## Status changes
- PANDO-US-0031 moved to `done`, all acceptance criteria checked. The fix is commit 38abda013. Its four acceptance tests exist in `internal/agui`. `go test -race ./internal/agui ./internal/permission` passed.
- PANDO-T-0005 moved to `cancelled` as a duplicate of PANDO-US-0032 (same race, same goroutine at agent.go:939).

## Status comments (items left open)
Every item got a comment file at `comments/<ID>/20260918T124000Z-claude.md`.
- PANDO-US-0032: `go test -race -count=3 ./internal/llm/agent` still fails in TestSessionModelIDFollowsOverride.
- PANDO-T-0006: fresh clone still does not build. There is no go:generate step or build dependency on `make embed-stubs`, and the README does not mention the stubs.
- PANDO-T-0007: `ledger.go:134` still ranges over the shared field `r.ch`.
- PANDO-T-0004: not started.
- PANDO-US-0050: needs real macOS hardware. The Sandbox E2E run was in progress at review time.
- PANDO-EP-0008 and US-0033..0039: not started. Only `grok-build-memory-survey.md` exists. The surveyed defects (MemoryAutoCapture has no reader, `BuildMemoryBlock(ctx, "")`) are still present and not filed as items.

Related: [[pando/fixes/agui_mcp_tool_permission_binding.md]], [[pando/analysis/grok-build-memory-survey.md]]
