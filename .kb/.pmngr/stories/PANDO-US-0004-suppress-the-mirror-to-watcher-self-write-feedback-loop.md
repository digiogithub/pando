---
id: PANDO-US-0004
type: story
title: Suppress the mirror to watcher self-write feedback loop
status: done
priority: high
parent: PANDO-EP-0005
milestone: PANDO-M-0002
author: claude
labels: [rag, kb]
estimate: 3
created: 2026-09-13T21:14:28Z
updated: 2026-09-13T21:14:28Z
---

## Description

As a caller of `kb_add_document`, I want the metadata I passed to still be there a moment later, so that the filesystem mirror does not feed its own write back through the watcher and clobber it.

`kb_add_document` writes the database and mirrors the document to `<KBPath>/<file_path>` with regenerated front matter (`internal/llm/tools/remembrances_kb.go:200,214` → `internal/rag/kb/filesystem.go:44-63`). With `KBWatch=true` the watcher sees that write. Because the metadata stored by the tool has no `source_mtime_unix`, the mtime comparison at `watcher.go:150-161` reads 0, concludes the file changed, and runs `UpdateDocument` with bare `source_*` metadata — discarding the `metadata` and `tags` the tool just stored.

The infrastructure to stop this already exists and is unused: `WithWriteOrigin(ctx, "watcher"|"sync"|"tool")` (`internal/rag/kb/observer.go:96-112`, set at `watcher.go:107` and `sync.go:45`) is consumed only by the optional write observer, never by the watcher's own skip logic. Implementation: have `WriteDocumentToFilesystem` (`filesystem.go:44`) and `DeleteDocumentFromFilesystem` (`filesystem.go:65`) record the absolute target path and the mtime they just produced in a small guarded map on `KBStore`; have the watcher consult that map before the debounce fires and drop the event when the path and mtime match. Bound the map: entries expire on a short TTL (a few seconds, comfortably longer than the 250 ms per-path debounce at `watcher.go:46-59`) and a sweep caps its size, so a burst of mirror writes cannot grow it without limit. Record the entry before `os.WriteFile` returns, so the fsnotify event cannot arrive first.

Do NOT solve this by writing `source_mtime_unix` into the tool's metadata — that makes the mtime check pass by lying about provenance and still breaks on any second write. Do NOT disable the watcher for mirrored paths wholesale: a user editing a mirrored file by hand must still be picked up.

## Acceptance Criteria

- [ ] `WriteDocumentToFilesystem` and `DeleteDocumentFromFilesystem` record the path and mtime they wrote before returning.
- [ ] The watcher skips an event whose path and mtime match a recent self-write, and processes an event for the same path with a different mtime.
- [ ] The suppression map is bounded by both TTL and size; a test writes many documents and asserts the map does not grow past the cap.
- [ ] A test calls `kb_add_document` with `metadata: {"status":"x"}` under `KBWatch=true` and asserts `kb_get_document` still reports `status: x` five seconds later.
- [ ] A hand edit to a mirrored file is still indexed.

## Notes

Size S. Independent of the other metadata stories and can be built in parallel; together with the watcher front-matter story it makes `KBWatch=true` safe to recommend again. Not on git-in-track's path (it runs the watcher off), but it is what lets a future host use the push model without losing metadata.
