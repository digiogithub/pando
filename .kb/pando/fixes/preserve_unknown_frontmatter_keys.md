---
created_at: 2026-09-14T15:43:54.155013651Z
updated_at: 2026-09-14T15:43:54.155013651Z
tags:
    - fix
    - rag
    - kb
---
# Fix: Preserve unknown front-matter keys in KB document metadata (PANDO-US-0002)

## What changed

`internal/rag/kb/frontmatter.go`:
- Added `ParseFrontMatterWithRaw(raw string) (FrontMatter, map[string]interface{}, string, error)`.
  It does the same delimiter splitting as `ParseFrontMatter`, then unmarshals the same YAML
  block a second time into a generic `map[string]interface{}` so callers can recover the
  full front-matter key set, not just the fields the typed `FrontMatter` struct names.
- `ParseFrontMatter` is now a thin wrapper around `ParseFrontMatterWithRaw` (drops the raw
  map). No other call site of `ParseFrontMatter` (`memory.go:386`, `StripFrontMatter`) needed
  changes.
- Added `reservedFrontMatterKeys` (`source_path`, `source_mtime_unix`, `source_format`,
  `converted`, `tags`, `aliases`, `created_at`, `updated_at`, and the memory fields `key`,
  `scope`, `source`, `outdated`, `expires_at`, `hits`, `importance`).
- Added `MergeUnknownFrontMatterKeys(meta, rawFM map[string]interface{}) map[string]interface{}`:
  copies every `rawFM` key not in the reserved set into `meta`, so a front-matter key named
  e.g. `source_path` can never shadow the authoritative sync-computed value.
- Added `jsonSafeValue(v interface{}) interface{}`: recursively normalizes YAML-decoded
  values (scalars pass through, `[]interface{}`/`map[string]interface{}` walked
  element-wise, anything else — e.g. `time.Time` — stringified via `fmt.Sprintf("%v", v)`)
  since metadata is marshalled to JSON at `kb.go:234-240`.

`internal/rag/kb/sync.go` (`SyncDirectoryWithStats`, non-converted branch, ~line 213-235):
- Switched `fm, body, _ := ParseFrontMatter(res.content)` to
  `fm, rawFM, body, parseErr := ParseFrontMatterWithRaw(res.content)`.
- On `parseErr != nil`, logs `logging.Warn("kb sync: front matter parse failed, indexing
  body as-is", "doc_path", ..., "error", parseErr)` — the existing fallback (raw content as
  body) still runs, so the document is still indexed.
- After the existing `InjectTagsIntoMetadata` / `InjectAliasesIntoMetadata` calls, added
  `meta = MergeUnknownFrontMatterKeys(meta, rawFM)` so host-defined keys (`id`, `type`,
  `status`, `project`, `milestone`, arbitrary nested maps/lists, ...) survive into
  `metadata`.

No changes to `Document`, the `kb_documents` schema, `internal/agui`, `internal/config`, or
any SDK code (out of scope per the story and per concurrent work by other agents).

## Why

Search hits and `kb_get_document` only ever returned `{source_path, source_mtime_unix,
source_format[, converted][, tags][, aliases]}` in metadata — every other front-matter key
a host wrote (e.g. `status: backlog`) was silently dropped because `yaml.Unmarshal` into
the fixed `FrontMatter` struct ignores unknown keys. Story: PANDO-US-0002 (parent
PANDO-EP-0005, milestone PANDO-M-0002).

## Verification

- `go build ./...` — passes.
- `go vet ./internal/rag/kb/...` — clean.
- `go test ./internal/rag/...` — all packages pass (kb package: 71 tests, 0 failures).
- New unit tests in `internal/rag/kb/frontmatter_test.go`: `ParseFrontMatterWithRaw` for
  scalars, lists, nested maps, empty block, no-front-matter, and unparseable (duplicate-key)
  YAML; `MergeUnknownFrontMatterKeys` for unknown-key copy, reserved-key collision (cannot
  overwrite `source_path`, cannot inject memory `key`), nil rawFM, nil meta;
  `jsonSafeValue` for exotic-type stringification and nested list/map normalization.
- New integration tests in `internal/rag/kb/sync_frontmatter_test.go` (using the existing
  `openTestKBDB` / `writeKBFile` / `fakeEmbedder` test helpers), run through
  `SyncDirectoryWithStats` + `GetDocument`:
  - `TestSyncDirectoryPreservesUnknownFrontMatterKeys` — `id`/`type`/`status`/`project`/
    `milestone` all land in `metadata`, tags still go through the typed path.
  - `TestSyncDirectoryFrontMatterKeyCannotOverwriteSourcePath` — a front-matter key named
    `source_path` does not overwrite the real one; an unreserved key alongside it still
    merges.
  - `TestSyncDirectoryUnparseableFrontMatterStillIndexed` — duplicate-key (invalid) YAML
    front matter still results in the document being indexed, with the raw file content as
    body and no `status` leaking into metadata.

Pre-existing unrelated failures observed in `internal/llm/agent` (`TestSetAndGetCavemanMode`,
`TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`,
`TestApplyToolDiscoveryWithoutManagerIsUnchanged`) — untouched by this change, not chased
per story instructions to work only inside `internal/rag/kb`.

## Acceptance criteria status

All five acceptance criteria from PANDO-US-0002 are satisfied; see Verification above for
the corresponding tests. No criterion was left unsatisfied.
