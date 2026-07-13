---
created_at: 2026-07-13T15:41:09.607676201Z
updated_at: 2026-07-13T15:41:09.607676201Z
tags:
    - change
    - kb
    - wiki
    - links
    - config
    - sync
    - docs
---
# KB Wiki Links — Phases 4 (sync stats) and 5 (config, guidance, docs)

Date: 2026-07-13
Status: DONE — the plan [[kb_wiki_links_plan]] is now complete (P1-P5).
Previous: [[kb_wiki_links_phase1]], [[kb_wiki_links_phase2_graph]], [[kb_wiki_links_phase3_tools]], [[kb_wiki_links_phase4_backfill]]

## What was changed

Two remaining pieces: the filesystem sync now reports the links it indexes (the
rest of Phase 4 — the backfill half shipped earlier), and the whole feature became
a first-class, switchable setting with agent guidance and user docs (Phase 5).

### Phase 4 — `SyncStats.LinksIndexed`

- `internal/rag/kb/types.go`: `SyncStats.LinksIndexed int` (`json:"links_indexed"`).
- `internal/rag/kb/sync.go`: `SyncDirectoryWithStats` adds
  `stats.LinksIndexed += s.countIndexedLinks(bodyContent)` after each successful
  add/update, and logs `links_indexed` on completion. **Unchanged documents are not
  counted**: the number is what the run wrote, not the size of the graph.
- `internal/rag/kb/links.go`: `(*KBStore).countIndexedLinks(body)` — a cheap re-scan
  (`ExtractWikiLinks` bails out immediately on a body with no `[[`) that avoids
  threading a count back out of the store's write methods.
- `internal/llm/tools/remembrances_kb.go`: `kb_import_path` adds `links_indexed` to
  its response **only when > 0**, and its description mentions the graph.
- `internal/app/remembrances.go`: the startup import's `WarnPersist` line appends
  "— N wiki links indexed" only when > 0; the structured log always carries the field.

### Phase 5 — config toggle

- `internal/config/config.go`: `RemembrancesConfig.KBWikiLinks`
  (`json:"kb_wiki_links" toml:"KBWikiLinks"`), nil-safe accessor
  `(*Config).KBWikiLinksEnabled()` defaulting to **true** (mirrors
  `BuildCodeGraphEnabled`), `viper.SetDefault("remembrances.kb_wiki_links", true)`.
- `internal/config/init.go`: `KBWikiLinks = true` in the generated `[Remembrances]`
  template.
- `internal/rag/kb/kb.go`: `KBStore.wikiLinks` field (default true in `NewKBStore`) +
  `SetWikiLinksEnabled` / `WikiLinksEnabled` (guarded by the existing `fsMu`).
  Precedent: `CodeIndexer.SetGraphEnabled` — the store owns the flag, the app injects
  it, so the `kb` package keeps no dependency on `config`.
- `internal/rag/service.go`: `kbStore.SetWikiLinksEnabled(config.Get().KBWikiLinksEnabled())`
  at construction (not in `initRemembrancesKBSync`, which early-returns when KBPath is
  empty — wiki links must work for tool-written documents with no filesystem mirror).
- **Single write choke point**: `(*KBStore).indexDocumentLinks` wraps
  `replaceDocumentLinks` with the toggle check; all three write paths call it
  (`AddDocument`, `AddDocumentWithEmbeddings` = the IPC-primary path, and the keyed
  memory raw-SQL path in `memory.go`).
- **Read guards**: `HasLinks` returns false when off (so the P3 tool sections vanish
  wholesale, since `attachDocumentLinks` and `LinkCountsFor` already gate on it), and
  `OutgoingLinks` / `Backlinks` / `RelatedDocuments` / `WantedConcepts` return nil.
  `backfillLinks` no-ops, and `initKBLinkBackfill` does not even spawn its goroutine.
- **Tool hidden when off**: `kb_related_documents` is registered only when
  `remembrances.KB.WikiLinksEnabled()` — in `internal/llm/agent/tools.go` (coder
  toolset) and `cmd/mcp_server.go`. With the graph off it would answer empty on every
  call, so it should not occupy a slot in the tool catalogue.
- `cmd/kb.go`: `pando kb relink` refuses to run with a clear message when the toggle
  is off, instead of silently building a graph nothing will read.
- UI: TUI settings field "KB Wiki Links" (`remembrances.kb_wiki_links`, toggle + apply
  case in `internal/tui/page/settings.go`) and WebUI toggle "Wiki Links" in
  `RemembrancesSettings.tsx` + `kb_wiki_links` in `types/index.ts` and
  `servicesSettingsStore.ts`. **No i18n keys**: the Remembrances settings labels in both
  surfaces are plain English strings today (the KBConvertDocuments precedent has none),
  so adding 7-locale keys for this one toggle would have been inconsistent — the plan's
  i18n item does not apply.

### Guidance and docs

- `internal/agentsmd/template.md` (the `/improve-agents-md` canonical ruleset): new
  step 2 in the context-gathering section ("Follow the graph, do not only search" —
  hop with `kb_related_documents` / backlinks rather than firing a second search), and
  a "Link what the document builds on" block in the mandatory-documentation section
  that tells the agent to write `[[concept]]` links and that linking a document that
  does not exist yet is correct and useful.
- `README.md`: new "Wiki links in the Knowledge Base" section (syntax, query-time
  resolution, wanted concepts, the four tools, `pando kb relink`, `KBWikiLinks`).

## Semantics of the toggle

Turning it off **does not wipe the graph**: the rows already indexed stay in the
database, invisible, and light up again on re-enable with no reindex. Only a document
rewritten while it is off loses its rows (`UpdateDocument` = delete + add, FK CASCADE).
`BackfillLinks` rebuilds anything skipped, because the content — not the link table —
is the source of truth. Applied at startup (a restart picks up a change), same as
`KBConvertDocuments` and `BuildCodeGraph`.

## Why

Phase 4 closes the observability gap: an import that silently built a graph gave no
signal that the syntax was even being picked up. Phase 5 makes the feature honest
about being optional — the retro-compat guarantee of P1-P3 was "a KB with no links
reads exactly as before"; the toggle extends that to "a user who does not want the
graph gets exactly the old behaviour", with no dead tool in the catalogue.

## Verification

- `go build ./...` and `go vet ./internal/... ./cmd/...` clean (the only vet complaint,
  `internal/mesnada/agent/spawner_template.go` context leak, is pre-existing).
- `go test ./internal/rag/kb/... ./internal/llm/tools/... ./internal/llm/agent
  ./internal/api ./internal/agentsmd` — all green.
- New `internal/rag/kb/sync_links_test.go`: `TestSyncReportsLinksIndexed` (2 links on
  first run, **0 on a no-op re-sync**), `TestSyncRefreshesLinksOfEditedFile` (edit on
  disk → old link gone, new link indexed — the watcher's path),
  `TestWikiLinksDisabledWritesNothing` (no rows; HasLinks/OutgoingLinks/Related/Wanted/
  Backfill all empty), `TestWikiLinksReenabledRecoversTheGraph` (backfill restores it).
- `npx tsc --noEmit` in `web-ui` clean.
- `internal/config` has two failing tests (`TestDefaultConfigTemplateEnablesPandoPreferredDefaults`,
  `TestMesnadaDelegationWarmDefaultsUnderShadowing`) — **confirmed pre-existing on HEAD**
  via a clean git worktree; unrelated to this change (they concern `[InternalTools]` and
  Mesnada warm-instance defaults).
