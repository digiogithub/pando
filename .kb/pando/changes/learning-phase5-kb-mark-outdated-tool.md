---
created_at: 2026-07-15T17:17:08.985025079Z
updated_at: 2026-07-15T17:17:08.985025079Z
tags:
    - change
    - learning
    - kb
    - tools
---
# Learning mode — Phase 5: kb_mark_outdated MCP tool

Part of the [[learning-opt-in-mode-implementation]] plan. Closes the capability gap the Learning harness relies on: the harness (`internal/learning`) already names `kb_mark_outdated`, but no MCP tool exposed `KBStore.MarkDocumentOutdated` — only the GC path called it.

## What changed

Added a new KB tool `kb_mark_outdated` that flags a knowledge-base document as outdated (superseded) without deleting it. Outdated docs are excluded from `kb_search_documents` by default (still retrievable with `exclude_outdated=false`) and the flag is mirrored into the document's front matter.

- **`internal/llm/tools/remembrances_kb.go`**
  - New const `kbMarkOutdatedToolName = "kb_mark_outdated"`.
  - New `KBMarkOutdatedTool` struct + `NewKBMarkOutdatedTool(store *kb.KBStore) BaseTool`.
  - `Info()`: single required param `file_path`; description steers the agent to prefer marking stale plans/features/fixes outdated (and adding the fresh doc) over leaving contradictory docs; documents idempotency.
  - `Run()`: validates `file_path`; calls `store.GetDocument` first and returns "Document not found" when nil — necessary because `KBStore.MarkDocumentOutdated` silently no-ops on a missing doc (its UPDATE touches zero rows), so without the pre-check the tool would falsely confirm. On success calls `MarkDocumentOutdated` and returns a confirmation.

- **Registration (both toolset builders that expose the KB tools):**
  - `internal/llm/agent/tools.go` — added `tools.NewKBMarkOutdatedTool(remembrances.KB)` after the delete tool.
  - `internal/mesnada/server/tools.go` — added `llmtools.NewKBMarkOutdatedTool(svc.KB)` (covers the pando MCP server + internal mesnada server).

- **`internal/llm/tools/remembrances_kb_mark_outdated_test.go`** (new): self-contained in-memory SQLite store (minimal kb_documents/kb_chunks/kb_links schema, nil embedder — docs inserted via direct SQL to avoid embedding). Cases: missing `file_path` → error; nonexistent doc → "Document not found"; existing doc → confirmation + `outdated=1` in DB; idempotent re-mark stays `outdated=1`.

## Why

The Learning mode harness instructs the agent to keep KB docs honest by marking superseded ones outdated. That verb needed a real tool; previously `MarkDocumentOutdated` was reachable only from the memory GC service (`internal/rag/kb/memory_gc.go`). The `outdated` flag is already honored by `kb_search_documents`'s `exclude_outdated` (default true), so exposing the mark completes the read/write loop.

## Verification

- `go build ./...` clean.
- `go vet ./internal/llm/tools/ ./internal/llm/agent/ ./internal/mesnada/server/` clean.
- `go test ./internal/llm/tools/ -run KBMarkOutdated -v` — 4/4 pass.
- `go test ./internal/llm/tools/` — package green.

## Remaining

Phase 6: README "Learning" section + `pando/features/learning_mode.md`, flip plan STATUS to COMPLETE, full build/vet/test + `-race` on new code.
