# KB YAML Front Matter Implementation Plan

## Goal
Add YAML front matter to KB documents with automatic date management (created_at, updated_at) and user-provided tags. Enhance search with tag filtering and chronological ordering.

## Current State
- `Document` struct has `Metadata map[string]interface{}`, `CreatedAt`, `UpdatedAt` (from DB)
- Filesystem mirror writes raw content without front matter
- No tag concept in search or tool parameters
- `gopkg.in/yaml.v3` already in go.mod

---

## Phase 1: YAML Front Matter Parser/Serializer
**File:** `internal/rag/kb/frontmatter.go` (new)

Create a `FrontMatter` struct and utilities:

```go
type FrontMatter struct {
    CreatedAt time.Time `yaml:"created_at"`
    UpdatedAt time.Time `yaml:"updated_at"`
    Tags      []string  `yaml:"tags,omitempty"`
}
```

Functions:
- `ParseFrontMatter(raw string) (FrontMatter, string, error)` — Splits `---\n...\n---\n` from body, parses YAML. Returns zero FrontMatter + full content if no front matter found.
- `SerializeFrontMatter(fm FrontMatter, body string) string` — Produces `---\nyaml\n---\nbody`.
- `MergeFrontMatter(existing, new FrontMatter) FrontMatter` — Preserves `created_at` from existing, sets `updated_at` to now, merges tags.

**Tests:** `internal/rag/kb/frontmatter_test.go`

---

## Phase 2: Integrate Front Matter into KB Store Operations
**Files:** `internal/rag/kb/kb.go`, `internal/rag/kb/types.go`

### types.go changes:
- Add `Tags []string` field to `Document` struct
- Tags are stored in the existing `metadata` JSON column under key `"tags"` (no schema migration needed)

### kb.go changes:
- `AddDocument`: Extract tags from metadata, store in metadata JSON. DB `created_at`/`updated_at` already managed.
- `UpdateDocument`: Preserve original `created_at`, update `updated_at` to now. Merge tags if provided.
- `GetDocument`: Parse tags from metadata JSON into `Document.Tags`.
- `AddDocumentWithEmbeddings`: Same tag extraction logic.

**Key design:** Tags live in the `metadata` JSON column as `{"tags": ["tag1", "tag2"], ...}`. No DB migration required. Front matter is generated at the filesystem mirror boundary only.

---

## Phase 3: Tool Layer — Add Tags Parameter + Filesystem Mirror Front Matter
**Files:** `internal/llm/tools/remembrances_kb.go`, `internal/rag/kb/filesystem.go`

### Tool changes (remembrances_kb.go):
- `KBAddDocumentTool.Info()`: Add `"tags"` parameter (array of strings, optional).
- `KBAddDocumentTool.Run()`: Extract `tags` from request, inject into metadata `{"tags": [...]}` before calling store.
- Content sent to store should be stripped of any user-provided front matter (pure body only).

### Filesystem mirror (filesystem.go):
- `WriteDocumentToFilesystem`: Accept `Document` (or created_at, updated_at, tags) and generate YAML front matter prepended to content.
- New helper: `buildFilesystemContent(createdAt, updatedAt time.Time, tags []string, body string) string`

---

## Phase 4: Enhance Search with Tag Filtering + Chronological Ordering
**Files:** `internal/rag/kb/kb.go`

### New method signature:
```go
func (s *KBStore) SearchDocumentsWithOptions(ctx context.Context, query string, limit int, opts SearchOptions) ([]SearchResult, error)
```

```go
type SearchOptions struct {
    Tags          []string // filter by tags (fuzzy/embed match)
    SortByDate    bool     // sort results chronologically (newest first)
}
```

### Implementation:
- After RRF fusion, apply tag filtering:
  - If `opts.Tags` is non-empty, compute similarity between requested tags and document tags using:
    1. Fuzzy string matching (Levenshtein or contains)
    2. Optionally embed tags and compute cosine similarity
  - Boost/filter results based on tag match score
- If `opts.SortByDate`, re-sort final results by `Document.UpdatedAt` descending (breaking ties by RRF score)
- Original `SearchDocuments` delegates to `SearchDocumentsWithOptions` with zero options for backward compat

---

## Phase 5: Update Tool Responses
**Files:** `internal/llm/tools/remembrances_kb.go`

### KBSearchDocumentsTool:
- `Info()`: Add optional `tags` parameter (array of strings)
- `Run()`: Pass tags to `SearchDocumentsWithOptions`
- Response items: Add `tags`, `created_at`, `updated_at` fields

### KBGetDocumentTool:
- Response: Add `tags` field alongside existing `created_at`, `updated_at`

---

## Phase 6: Handle Front Matter in SyncDirectory Import
**Files:** `internal/rag/kb/sync.go`

When importing `.md` files from disk:
- Parse YAML front matter from file content
- Extract `tags` from front matter → store in metadata
- Extract `created_at` from front matter → use as document creation time if present (don't overwrite with `now`)
- Strip front matter from content before storing (store pure body in DB)
- On re-export (mirror write), regenerate front matter from DB dates + tags

---

## Implementation Order
1. Phase 1 (parser) — foundation, no side effects
2. Phase 2 (store integration) — tags in metadata
3. Phase 3 (tools + filesystem) — user-facing API + mirror output
4. Phase 4 (search enhancement) — tag filtering
5. Phase 5 (tool responses) — expose new data
6. Phase 6 (sync import) — round-trip front matter

## Design Decisions
- **No DB migration**: Tags stored in existing `metadata` JSON column
- **Front matter at boundary**: DB stores pure content; front matter only in filesystem mirror and import
- **Dates automatic**: `created_at` set on first add, `updated_at` on every update, never exposed as tool input
- **Tags explicit**: User provides tags via tool parameter, not via content front matter
- **Backward compatible**: Existing documents without tags work unchanged
