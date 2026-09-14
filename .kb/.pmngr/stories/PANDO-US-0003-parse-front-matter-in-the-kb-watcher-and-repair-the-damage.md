---
id: PANDO-US-0003
type: story
title: Parse front matter in the KB watcher and repair the damage at startup
status: done
priority: high
parent: PANDO-EP-0005
milestone: PANDO-M-0002
author: claude
labels: [rag, kb]
estimate: 5
created: 2026-09-13T21:14:28Z
updated: 2026-09-13T21:14:28Z
---

## Description

As an operator running `KBWatch=true`, I want an edit to a document to preserve its metadata, so that the tags and front-matter fields stored by the initial sync are not erased by the first save.

`handleWatchEvent` (`internal/rag/kb/watcher.go:163-191`) calls `loadDocumentBody` — which returns content, format and a converted flag, no front matter — and builds metadata containing only `source_path`, `source_mtime_unix` and `source_format` (`watcher.go:173-177`). It then calls `UpdateDocument` (`watcher.go:189`), and `updateDocument` is delete-then-add (`kb.go:563-568`), so metadata is replaced wholesale, not merged. The loss is permanent: the watcher writes a current `source_mtime_unix`, so the next startup sync treats the file as unchanged (`sync.go:150-157`) and never repairs it. Measured on Pando's own database: 539 documents carry `source_mtime_unix`, 16 still carry tags. A second, quieter symptom is that the watcher stores the raw front-matter block inside the indexed body while the sync path strips it, so the same document embeds differently depending on which path touched it last.

Implementation: factor the metadata-and-body construction out of `sync.go:206-228` into one helper (for example `buildDocumentMetadata(absPath, docPath string, mtimeUnix int64, res loadResult) (map[string]interface{}, string)`) and call it from both `sync.go` and `watcher.go:163-180`, so the two paths cannot drift again. Then add a startup repair: after the initial `SyncDirectoryWithStats` (`internal/app/remembrances.go:102-141`), walk documents whose stored metadata lacks keys their source file declares and re-index those, ignoring the mtime skip for that pass. Bound the repair (log a count, run it in the existing background goroutine, honour the context) and make it a one-shot — record a marker so it does not re-scan the whole corpus on every boot.

Do NOT make `updateDocument` merge metadata as a way around this: callers rely on replacement semantics, and merging would make a deleted front-matter key un-deletable. Do NOT repair by deleting and re-importing the corpus — `file_path` is the key other hosts resolve against.

## Acceptance Criteria

- [ ] The watcher and the sync path build metadata and body through one shared helper; a test asserts both produce identical metadata for the same file.
- [ ] The body stored by the watcher is front-matter-stripped, matching the sync path.
- [ ] A regression test writes a document with `tags` and a custom front-matter key, lets the watcher index it, edits the body, and asserts the tags and the custom key survive the edit.
- [ ] The startup repair re-indexes documents whose stored metadata lacks tags or keys their source declares, logs how many it repaired, and does not re-run on the next start.
- [ ] The repair is a no-op on a corpus indexed after this change.

## Notes

Size M. Depends on the front-matter-preservation story (it supplies the shared parse) — land that one first. Related to the mirror self-write story: both are watcher metadata-destruction bugs, but they have independent causes and independent fixes. git-in-track sidesteps both by running `KBWatch=false` (decision of 2026-09-13), so this is a correctness fix for Pando's own knowledge base and for any host that wants the watcher on.
