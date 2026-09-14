---
created_at: 2026-09-14T15:59:49.470017563Z
updated_at: 2026-09-14T15:59:49.470017563Z
tags:
    - rag
    - kb
    - fix
    - watcher
    - filesystem-mirror
---
## PANDO-US-0004 — Suppress the mirror-to-watcher self-write feedback loop

### What changed

- `internal/rag/kb/kb.go` — `KBStore` gained `selfWriteMu sync.Mutex` and
  `selfWrites map[string]selfWriteEntry`, a small guarded record of filesystem mirror
  writes/deletes the store itself just made.
- `internal/rag/kb/selfwrite.go` (new)
  - `selfWriteEntry{mtimeUnix int64, isDelete bool, expiresAt time.Time}`.
  - `selfWriteTTL = 3 * time.Second` (comfortably longer than the watcher's 250ms per-path
    debounce), `selfWriteCap = 2048`.
  - `recordSelfWrite(absPath, mtimeUnix)` / `recordSelfDelete(absPath)` write an entry via
    `putSelfWrite`, which sweeps expired entries and, if still at `selfWriteCap`, evicts the
    entry closest to expiry — bounding the map by both TTL and size.
  - `consumeSelfWrite(absPath, mtimeUnix)` / `consumeSelfDelete(absPath)` are non-destructive
    reads (`peekSelfWrite`): a match does **not** delete the entry. This matters because a
    single `os.WriteFile` on a new file commonly surfaces as two separate fsnotify events
    (Create then Write) with the same final mtime — both must be recognized as the same
    self-write, not just the first. Entries are instead reclaimed by TTL expiry, swept
    opportunistically on the next `recordSelfWrite`/`recordSelfDelete` call.
  - `selfWriteCount()` — test-only accessor for the cap assertion.
- `internal/rag/kb/filesystem.go`
  - `WriteDocumentToFilesystem`: records a predicted self-write (`time.Now().Unix()`) *before*
    calling `os.WriteFile` (so the fsnotify event this write is about to generate can never
    race ahead of the watcher's check), then corrects the entry to the real on-disk mtime via
    `os.Stat` immediately after the write completes.
  - `DeleteDocumentFromFilesystem`: records a self-delete via `recordSelfDelete` before calling
    `os.Remove`.
- `internal/rag/kb/watcher.go`
  - `WatchDirectory`'s event loop now calls `s.shouldSkipSelfWriteEvent(event)` right after the
    `isIndexableFile` filter and *before* `handleWithDebounce` schedules anything — so a
    self-write event is dropped before it ever enters the 250ms debounce, not merely ignored
    once handled.
  - New `shouldSkipSelfWriteEvent(event fsnotify.Event) bool`: for `Remove`/`Rename` it checks
    `consumeSelfDelete(absPath)` (path only, a removed file has no meaningful mtime); otherwise
    it `os.Stat`s the file and checks `consumeSelfWrite(absPath, mtime)`. A stat failure (or no
    matching entry) means "process normally" — `handleWatchEvent` already re-stats and handles
    a vanished file as a delete.

### Why

`kb_add_document` (`internal/llm/tools/remembrances_kb.go`) writes the DB document then mirrors
it to `<KBPath>/<file_path>` via `WriteDocumentToFilesystem`. With `KBWatch=true` the watcher saw
that mirror write as a normal fsnotify event. Because the tool's stored metadata has no
`source_mtime_unix`, the watcher's mtime-skip check (`watcher.go`, PANDO-US-0003) read 0,
concluded the file had "changed", and ran `UpdateDocument` with bare `source_*` metadata —
discarding the `metadata`/`tags` the tool call had just stored, on every single mirrored write.
`WithWriteOrigin(ctx, "watcher"|"sync"|"tool")` already existed for observers but was never
consulted by the watcher's own skip logic; this fix adds that logic as a dedicated, bounded
path/mtime record instead of reusing the origin tag (the tag can't carry a path+mtime to match
against).

### Verification

- `go build ./...` — green.
- `go test ./internal/rag/...` (including `-race -count=2` on the new/changed tests) — green, no
  data races on the new guarded map.
- New tests in `internal/rag/kb`:
  - `selfwrite_test.go`: `TestSelfWriteMatchesRepeatedly` (a self-write keeps matching more than
    one event), `TestSelfWriteMismatchedMtimeNotConsumed`, `TestSelfDeleteMatchesRepeatedly`,
    `TestSelfWriteDoesNotMatchSelfDeleteEntry`, `TestSelfWriteEntryExpiresAfterTTL`,
    `TestSelfWriteMapBoundedBySizeCap` / `TestSelfWriteMapBoundedEvenWithinOneTTLWindow` (many
    writes never push `selfWriteCount()` past `selfWriteCap`, even within one TTL window so the
    cap enforcement — not just TTL expiry — is what's under test).
  - `selfwrite_watcher_test.go`: `TestShouldSkipSelfWriteEventMatchesRecordedWrite` /
    `...ProcessesDifferentMtime` / `...MatchesRecordedDelete` / `...NoEntryProcessesNormally`
    (deterministic, no real fsnotify); `TestMirrorSelfWriteDoesNotClobberMetadata` (live
    `WatchDirectory` + `AddDocument`+`WriteDocumentToFilesystem` exactly as the tool does,
    asserts `metadata.status == "x"` five seconds later — the scenario named in the story's
    acceptance criterion, kept inside `internal/rag/kb` per this task's scope rather than in
    `internal/llm/tools`); `TestHandEditToMirroredFileStillIndexed` (a direct `os.WriteFile` to
    the mirrored path, at least one full wall-clock second after the seed mirror write to avoid
    an mtime-second collision, is still indexed within 5s).
- Confirmed via a standalone probe that fsnotify/inotify events are delivered in this sandbox
  before relying on real-timing watcher tests.
- Pre-existing, unrelated failures in `internal/llm/agent` left untouched, as instructed.

Related: [[pando/fixes/frontmatter_watcher_repair_pando_us_0003.md]],
[[.pmngr/epics/PANDO-EP-0005-knowledge-base-metadata-fidelity-and-rest-search-surface.md]].
