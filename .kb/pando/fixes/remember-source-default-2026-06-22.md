---
created_at: 2026-06-22T09:25:29.875011086Z
updated_at: 2026-06-22T09:25:29.875011086Z
tags:
    - fixes
    - memory
    - kb
---
# Fix remember source default

## What changed
- Added a default source fallback in `internal/rag/kb/memory.go` so `KBStore.UpsertMemory` now normalizes empty or whitespace-only `MemoryUpsertOptions.Source` values to `"memory"` before any keyed or keyless memory write.
- Added a regression test in `internal/rag/kb/memory_upsert_test.go` covering keyed memory upserts without an explicit source and asserting the stored `kb_documents.source` value is populated.

## Files and symbols touched
- `internal/rag/kb/memory.go`
  - `KBStore.UpsertMemory`
- `internal/rag/kb/memory_upsert_test.go`
  - `TestUpsertMemoryByKeyDefaultsSource`

## Reason / motivation
The `remember` tool builds `MemoryUpsertOptions` without setting `Source`. The `kb_documents.source` column is `NOT NULL`, so direct DB upserts could fail with `NOT NULL constraint failed: kb_documents.source`. Normalizing an empty source centrally in `KBStore.UpsertMemory` fixes the bug for `remember` and any other caller that omits the field.

## Verification
- Ran: `go test ./internal/rag/kb ./internal/llm/tools`
- Added regression coverage asserting successful upsert and persisted default source value.
