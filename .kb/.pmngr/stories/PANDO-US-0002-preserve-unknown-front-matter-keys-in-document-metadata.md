---
id: PANDO-US-0002
type: story
title: Preserve unknown front-matter keys in document metadata
status: done
priority: critical
parent: PANDO-EP-0005
milestone: PANDO-M-0002
author: claude
labels: [rag, kb]
estimate: 3
created: 2026-09-13T21:14:28Z
updated: 2026-09-14T00:00:00Z
---

## Description

As a host exporting structured records into the knowledge base, I want every front-matter key to survive into the document's `metadata`, so that a search hit carries the fields I wrote (`id`, `type`, `status`, `project`, `milestone`) instead of only `tags` and `aliases`.

`FrontMatter` (`internal/rag/kb/frontmatter.go:15-30`) is a fixed struct with no inline map, so `yaml.Unmarshal` drops every key it does not name. `SyncDirectoryWithStats` then lifts exactly two of them (`internal/rag/kb/sync.go:214-228`: `InjectTagsIntoMetadata`, `InjectAliasesIntoMetadata`), leaving stored metadata as `{source_path, source_mtime_unix, source_format[, converted][, tags][, aliases]}` and nothing else.

Implementation: add a second return path to `ParseFrontMatter` (`frontmatter.go:41`) — unmarshal the YAML block a second time into a `map[string]interface{}` and return it alongside the typed struct (a new `ParseFrontMatterWithRaw`, keeping `ParseFrontMatter` as a thin wrapper, avoids touching the other call sites). In `sync.go:206-228`, merge the unrecognised keys into `meta` after the typed handling, skipping the reserved key set: `source_path`, `source_mtime_unix`, `source_format`, `converted`, `tags`, `aliases`, `created_at`, `updated_at`, and the memory fields (`key`, `scope`, `source`, `outdated`, `expires_at`, `hits`, `importance`). Values that are not JSON-representable scalars, lists or maps should be stringified rather than dropped, because metadata is marshalled to JSON at `kb.go:234-240`.

Do NOT change the `Document` struct or the `kb_documents` schema — metadata is already a JSON column and is already returned in every search result (`internal/llm/tools/remembrances_kb.go:296-316`), so the fix pays off for MCP clients with no client change. Do NOT start encoding these fields as tags: `matchesTags` (`kb.go:820-829`) is substring containment in both directions, so `status:backlog` would match a document tagged `backlog` and a document tagged `s` would match everything.

## Acceptance Criteria

- [ ] `ParseFrontMatter` (or its new sibling) returns the full key set alongside the typed `FrontMatter`, with a unit test covering scalars, lists, nested maps and an empty block.
- [ ] A document whose front matter declares `status: backlog` returns `metadata.status == "backlog"` from `kb_get_document` and from a `kb_search_documents` hit.
- [ ] Reserved keys keep their current typed handling and a front-matter key named `source_path` cannot overwrite the real one.
- [ ] `tags` and `aliases` still arrive through `InjectTagsIntoMetadata` / `InjectAliasesIntoMetadata`, unchanged.
- [ ] A document whose front matter fails to parse is still indexed, with a warning logged and the body indexed as-is (existing fallback at `sync.go:216-219`).

## Notes

Size S. Independent of PANDO-EP-0005's REST stories; the watcher story (parse front matter in the watcher) must land after this one so both paths build identical metadata. git-in-track does not hard-block on this — GIT-US-0082 resolves a `file_path` hit back to its own index for authoritative fields — but it is the difference between Pando returning usable records and returning bare paths, and it is the prerequisite for any structured filter on top of metadata.
