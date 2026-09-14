---
created_at: 2026-09-14T16:12:13.416908707Z
updated_at: 2026-09-14T16:12:13.416908707Z
tags:
    - feature
    - rag
    - kb
    - api
---

# REST KB document upsert/delete/reindex

Implements [[PANDO-US-0001]] (part of [[PANDO-EP-0005]] "Knowledge base: metadata
fidelity and REST search surface"). Adds three authenticated REST routes so a host
can drive KB freshness deterministically over HTTP instead of trusting the
filesystem watcher.

## What changed

- **New file** `internal/api/handlers_remembrances_kb.go`:
  - `handleUpsertKBDocument` — `POST /api/v1/remembrances/kb/documents`. Body
    `{file_path, content, metadata, tags}`. Looks up the document via
    `KB.GetDocument`; calls `KB.AddDocument` when unknown, `KB.UpdateDocument`
    when it exists. Tags are merged into metadata via the existing
    `kb.InjectTagsIntoMetadata` helper (tags have no separate storage — same
    mechanism `kb_add_document` uses). When an update omits `metadata` entirely
    (the JSON key absent, i.e. Go's decoded map is `nil`, not an explicit `{}`),
    the previously stored map is re-sent instead of being silently dropped,
    since `updateDocument` is delete-then-add with wholesale metadata
    replacement.
  - `handleDeleteKBDocument` — `DELETE /api/v1/remembrances/kb/documents`. Body
    `{file_path}`. Calls `KB.DeleteDocument` then
    `KB.DeleteDocumentFromFilesystem` so a later reindex cannot resurrect the
    document from its still-present mirror file.
  - `handleReindexKB` — `POST /api/v1/remembrances/kb/reindex`. No body/params
    (no `path` exposed, per spec). Runs
    `KB.SyncDirectoryWithStats(ctx, KB.FilesystemMirrorPath(), true)` and
    returns the `kb.SyncStats` struct as the 200 body. Guarded by a
    package-level `kbReindexMu sync.Mutex` (`TryLock`); a concurrent call gets
    `409` with a JSON error and does not start a second sync. Refuses (503)
    when no filesystem mirror is configured, since an empty path would
    otherwise resolve to the process CWD via `filepath.Abs("")` inside
    `SyncDirectoryWithStats` — a latent bug in the caller, not in KBStore.
  - All three routes 503 with a JSON error (not a panic) when
    `s.app.Remembrances` or `.KB` is nil, matching the pattern at
    `handlers_remembrances.go:32` (`handleIndexCodeProject`).
- **`internal/api/routes.go`**: three lines added to the existing remembrances
  block, right after `GET /api/v1/remembrances/embedding-models`.

## Design notes

- No new auth wiring: `/api/` already goes through `hasValidToken`
  (`X-Pando-Token` header or `?token=`), confirmed by test
  (`TestKBRoutes_RequireValidToken`, all three routes, via the real
  `corsMiddleware(basicAuthMiddleware(authMiddleware(...)))` stack).
- No mirror write on POST: `AddDocument`/`UpdateDocument` are DB-only; the
  spec does not ask for a `WriteDocumentToFilesystem` call here (unlike the
  `kb_add_document` MCP tool, which does front-matter merge + mirror write).
  This route is meant for a host with its own external corpus, not
  necessarily backed by Pando's mirrored tree.
- **Secondary-instance proxying (AC6) needed no new code.**
  `KB.AddDocument`/`UpdateDocument`/`DeleteDocument` already check
  `s.proxy != nil` internally (`internal/rag/kb/kb.go`) and forward to the
  primary via `dbproxy.WriteWithRetry` when so configured — the REST handler
  just calls the exported methods and inherits this for free. Reindex is the
  same: `SyncDirectoryWithStats` walks the local filesystem/DB for change
  detection but calls the same `AddDocument`/`UpdateDocument`/`DeleteDocument`
  per changed file, so each individual write still proxies correctly on a
  secondary. Verified end-to-end in
  `TestKBWriteRoutes_SecondaryProxiesToPrimary`: a real `ipc.Bus` +
  `ipc.Client` + `dbproxy.New` + `ragproxy.NewRemembrancesWriteDispatcher`
  round trip, asserting the primary's KBStore received the document and the
  secondary's own local SQLite got zero rows.

## Files touched

- `internal/api/handlers_remembrances_kb.go` (new) —
  `handleUpsertKBDocument`, `handleDeleteKBDocument`, `handleReindexKB`,
  `kbReindexMu`.
- `internal/api/routes.go` — 3 new `mux.HandleFunc` registrations in the
  remembrances block.
- `internal/api/handlers_remembrances_kb_test.go` (new) — full test suite
  below.

## Verification

- `go build ./...` — clean.
- `go test ./internal/api/... ./internal/rag/...` — all green.
- Tests per acceptance criterion, in
  `internal/api/handlers_remembrances_kb_test.go`:
  - `TestHandleUpsertKBDocument_CreatesThenUpdates` — AC1 (create, then
    update-in-place, metadata byte-identical round trip via `GetDocument`).
  - `TestHandleUpsertKBDocument_OmittedMetadataOnUpdateKeepsStoredMap` — the
    "must re-send the stored map" nuance called out explicitly in the spec.
  - `TestHandleDeleteKBDocument_RemovesRowAndMirroredFile` — AC2 (DB row +
    chunks + mirrored file all gone).
  - `TestHandleReindexKB_ReturnsStatsAndSkipsUnchanged` — AC3 (SyncStats
    fields; second reindex with nothing changed skips; touching one file only
    re-embeds that one, as an `updated` count of 1).
  - `TestHandleReindexKB_ConcurrentRunsOneWinsOneConflicts` — AC4 (two
    goroutines, `blockingKBEmbedder` holds the first sync mid-flight,
    asserts exactly one 200 and one 409).
  - `TestKBRoutes_RequireValidToken` — AC5 (401 without `X-Pando-Token`,
    through the real middleware stack, all three routes).
  - `TestHandleUpsertKBDocument_NilRemembrancesReturns503`,
    `TestHandleDeleteKBDocument_NilRemembrancesReturns503`,
    `TestHandleReindexKB_NilRemembrancesReturns503`,
    `TestHandleReindexKB_NoMirrorConfiguredReturns503` — AC5 (nil-safety).
  - `TestKBWriteRoutes_SecondaryProxiesToPrimary` — AC6 (secondary proxies to
    primary instead of writing locally).
  - Plus method-not-allowed and missing-`file_path` edge cases.

Known pre-existing unrelated failures live in `internal/llm/agent` (caveman +
tool-discovery tests) — not touched, not chased.