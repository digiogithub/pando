---
id: PANDO-EP-0023
type: epic
title: "Read-before-edit guard survives restarts: session-history fallback with content check and compaction cut-off"
status: in_review
priority: high
author: mcp
labels: [tools, edit, webui, macos-report]
created: 2026-11-03T12:02:02Z
updated: 2026-11-03T12:06:20Z
started: 2026-11-03T12:02:22Z
---

## Description
macOS user (WebUI, v1.5.2, debug_id 7402-2673-1745-4679) reports that `edit` often fails with `you must read the file before editing it. Use the View tool first` and only succeeds on the second try. The guard state (`fileRecords` in `internal/llm/tools/file.go`) lives only in process memory, so a restart of the Pando server or app wipes it while the persisted session history still shows the read. The model edits, the guard rejects, the model re-reads, and the second attempt works.

Related fixes already done in the same report:
- `view` returns raster images (png/jpeg/webp/gif) as image blocks instead of an error.
- `view` over ACP resolves the path before reading and recording, so it uses the same key as `edit`.

## Design
When the in-memory map has no read for the resolved path, `edit`/`patch`/`write` fall back to the session history:
1. Only the same session (`SessionIDContextKey`).
2. Only messages after the last compaction point (`session.SummaryMessageID`).
3. A successful `view` (or alias `read`) tool result whose `file_path` resolves to the same absolute path.
4. Last-read time = timestamp of the assistant message holding the tool call (before the actual read, so stricter).
5. The existing mtime check still applies.
6. Content check: the content recorded in `ViewResponseMetadata.Content` must still be present in the current file. This catches changes that preserve mtime (`rsync -t`, `cp -p`, tar).
7. On success, cache the result in the in-memory map. On any doubt, fail closed.

## Acceptance Criteria
- Edit after a simulated restart (empty map, history with a view of the file) succeeds.
- Edit is rejected when: no view in history; view from another session; view only before the compaction point; file mtime later than the read; recorded content no longer in the file; view result was an error.
- `go test ./internal/llm/tools/ ./internal/llm/agent ./internal/api` passes.
- KB summary updated.

## Notes
The guard ends up equal or stricter than before: today the map is process-global (a read in session A allows an edit in session B).
