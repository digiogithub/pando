---
id: PANDO-US-0001
type: story
title: REST document upsert, delete and KB reindex
status: backlog
priority: critical
parent: PANDO-EP-0005
milestone: PANDO-M-0002
author: claude
labels: [rag, kb, api]
estimate: 5
created: 2026-09-13T21:14:28Z
updated: 2026-09-13T21:14:28Z
---

## Description

As the host of an external corpus, I want to upsert, delete and re-sync knowledge-base documents over the authenticated REST API, so that I can drive freshness deterministically instead of waiting for a filesystem watcher I have to trust.

Add one file, `internal/api/handlers_remembrances_kb.go`, and four lines in `internal/api/routes.go` next to the existing remembrances block (`routes.go:144-152`). Everything needed is already on `App`: `internal/app/app.go:104` exposes `Remembrances *rag.RemembrancesService`, whose `KB` field is a `*kb.KBStore` (`internal/rag/service.go:20-25`); handlers reach it as `s.app.Remembrances.KB` exactly as `handlers_remembrances.go:32,37` reaches `.Code`.

- `POST /api/v1/remembrances/kb/documents` — body `{file_path, content, metadata, tags}`. Call `KB.AddDocument` (`internal/rag/kb/kb.go:171`) when the path is unknown and `KB.UpdateDocument` (`kb.go:523`) when it exists; store `metadata` verbatim, as `AddDocument` already does (`kb.go:234-240`). Note that `updateDocument` is delete-then-add with wholesale metadata replacement (`kb.go:537-569`), so an upsert that omits `metadata` must re-send the stored map rather than silently dropping it.
- `DELETE /api/v1/remembrances/kb/documents` — `KB.DeleteDocument` (`kb.go:431`) plus `DeleteDocumentFromFilesystem` (`internal/rag/kb/filesystem.go:65`) so the mirror does not resurrect the document on the next sync.
- `POST /api/v1/remembrances/kb/reindex` — `KB.SyncDirectoryWithStats(ctx, KB.FilesystemMirrorPath(), true)` (`internal/rag/kb/sync.go:42`, `filesystem.go:36`). `FilesystemMirrorPath()` is the resolved absolute `KBPath` set at `internal/app/remembrances.go:89`, which is the one rooting that yields stable document keys — this is why the objection in `internal/llm/tools/remembrances_kb.go:13-22` against a re-sync *tool* does not apply to a *host* route: the host chooses the root, not the model. Guard the run with a package-level `sync.Mutex` held for the whole sync and answer `409` with a JSON error when it is already held; return the `kb.SyncStats` struct (`internal/rag/kb/types.go:44`) as the 200 body. `SyncDirectoryWithStats` skips files whose `source_mtime_unix` is unchanged (`sync.go:150-157`), so a re-sync after one item changed re-embeds one document.

Secondary Pando instances must proxy writes to the primary, following the pattern already in `kb.go:190-226`.

Do NOT add a generic `POST /api/v1/tools/{name}/call` bridge — it exposes every tool, including the write and exec groups, over REST. Do NOT wire any new auth middleware: `internal/api/server.go:496` gates on the `/api/` prefix and `hasValidToken` (`:448-453`) accepts `X-Pando-Token` or `?token=`, so these routes are authenticated the moment they are registered. Do NOT expose a `path` parameter on reindex.

## Acceptance Criteria

- [ ] `POST /api/v1/remembrances/kb/documents` creates a document and, called again for the same `file_path`, updates it; a `metadata` map sent on either call comes back byte-identical from `kb_get_document`.
- [ ] `DELETE /api/v1/remembrances/kb/documents` removes the row, its chunks and the mirrored file under `FilesystemMirrorPath()`.
- [ ] `POST /api/v1/remembrances/kb/reindex` returns 200 with the `SyncStats` fields (indexed, updated, skipped, deleted, errors) and re-embeds only the documents whose mtime changed.
- [ ] A second reindex issued while one is running returns `409` with a JSON error body and does not start a second sync; a test starts two goroutines and asserts exactly one 200 and one 409.
- [ ] Every route answers `401` for a request without a valid `X-Pando-Token`, and answers an empty/`503` JSON body rather than panicking when `s.app.Remembrances` or `.KB` is nil, matching `handlers_remembrances.go:32`.
- [ ] On a secondary instance the three write routes proxy to the primary instead of writing locally.

## Notes

Size S-M. No schema change, no new config, no new dependency. This is the story that unblocks git-in-track: GIT-US-0073 exports its corpus to disk and runs with `KBWatch=false`, so `POST /kb/reindex` is what makes its sync deterministic, and GIT-US-0091 surfaces the returned `SyncStats` in the settings card. It is independent of the metadata stories in this epic — build it first and do not sequence it behind them.
