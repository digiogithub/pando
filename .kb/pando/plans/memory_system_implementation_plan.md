---
created_at: 2026-06-11T07:36:45.011267525Z
updated_at: 2026-06-11T07:36:45.011267525Z
tags:
    - plan
    - memory
    - kb
    - remembrances
    - architecture
---
# Memory System Implementation Plan

## Context

Based on `.kb/analysis/memory-tool-analysis.md` and analysis of the existing codebase:

### Current state
- `kb_documents` table: `id, file_path, content, metadata, created_at, updated_at`
- `FrontMatter` struct: `CreatedAt, UpdatedAt, Tags`
- Tools: `kb_add_document`, `kb_import_path`, `kb_search_documents`, `kb_get_document`, `kb_delete_document`
- Config (`RemembrancesConfig`): `Enabled`, embedding providers, `ContextEnrichmentEnabled` + params
- No memory-specific layer, key-value upsert, TTL, hits counter, or outdated flag

### Goals
- Transparent memory that the agent "knows" without making tool calls (auto-injection in system prompt)
- Simple intentional API: `remember / recall / forget`
- Key-value upsert with frontmatter `key` to prevent contradictory duplicates
- `outdated` flag driven by TTL + background GC service
- Usage counter (`hits`) that extends effective TTL
- Activatable/deactivatable feature (MemoryEnabled flag)
- Config sections in `.toml`, TUI settings, and Web UI settings

---

## Phase 1 — DB Migration: Memory fields in `kb_documents`

**File**: `internal/db/migrations/YYYYMMDD_add_memory_fields.sql`

Add columns to `kb_documents`:
```sql
ALTER TABLE kb_documents ADD COLUMN memory_key   TEXT;     -- unique upsert key (NULL = not a memory)
ALTER TABLE kb_documents ADD COLUMN memory_scope  TEXT;     -- "user/", "project/", "session/"
ALTER TABLE kb_documents ADD COLUMN outdated      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kb_documents ADD COLUMN expires_at    DATETIME; -- NULL = no expiry
ALTER TABLE kb_documents ADD COLUMN hits          INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kb_documents ADD COLUMN importance    REAL    NOT NULL DEFAULT 0.5;
ALTER TABLE kb_documents ADD COLUMN source        TEXT;     -- session_id or tool name

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_documents_memory_key ON kb_documents(memory_key)
    WHERE memory_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_kb_documents_expires ON kb_documents(expires_at)
    WHERE expires_at IS NOT NULL AND outdated = 0;
CREATE INDEX IF NOT EXISTS idx_kb_documents_scope ON kb_documents(memory_scope)
    WHERE memory_scope IS NOT NULL;
```

**Default TTL**: 180 days (6 months) from creation/last-use. When `hits` is incremented, `expires_at` is pushed forward: `expires_at = MAX(expires_at, now + 180 days)`.

---

## Phase 2 — FrontMatter Struct Extensions

**File**: `internal/rag/kb/frontmatter.go`

Extend `FrontMatter`:
```go
type FrontMatter struct {
    CreatedAt  time.Time  `yaml:"created_at,omitempty"`
    UpdatedAt  time.Time  `yaml:"updated_at,omitempty"`
    Tags       []string   `yaml:"tags,omitempty"`
    // Memory fields — only serialized when non-zero
    Key        string     `yaml:"key,omitempty"`        // upsert key, e.g. "user.preferred_lang"
    Scope      string     `yaml:"scope,omitempty"`      // "user/", "project/", "session/"
    Outdated   bool       `yaml:"outdated,omitempty"`   // true when TTL expired
    ExpiresAt  *time.Time `yaml:"expires_at,omitempty"` // absolute expiry timestamp
    Hits       int        `yaml:"hits,omitempty"`       // incremented on every recall/injection
    Importance float64    `yaml:"importance,omitempty"` // 0.0–1.0 relevance weight
    Source     string     `yaml:"source,omitempty"`     // creator session/agent
}
```

Update `MergeFrontMatter` to preserve `Key`, `Scope`, `Source`, `Hits` (carry forward), `Outdated` (from incoming if provided), and `ExpiresAt` (recalculated on update if hits change).

Update `NewFrontMatter(tags, key, scope, source string, defaultTTLDays int)` to accept memory params and compute `ExpiresAt` when `key != ""`.

---

## Phase 3 — Config Additions

**File**: `internal/config/config.go` → `RemembrancesConfig` struct

```go
// Memory subsystem
MemoryEnabled                  bool     `json:"memory_enabled" toml:"MemoryEnabled"`
MemoryContextEnrichmentEnabled bool     `json:"memory_context_enrichment_enabled" toml:"MemoryContextEnrichmentEnabled"`
MemoryContextMaxItems          int      `json:"memory_context_max_items" toml:"MemoryContextMaxItems"`          // default 10
MemoryContextMaxChars          int      `json:"memory_context_max_chars" toml:"MemoryContextMaxChars"`          // default 1500
MemoryDefaultTTLDays           int      `json:"memory_default_ttl_days" toml:"MemoryDefaultTTLDays"`            // default 180
MemoryGCInterval               string   `json:"memory_gc_interval" toml:"MemoryGCInterval"`                     // default "1h"
MemoryAutoCapture              bool     `json:"memory_auto_capture" toml:"MemoryAutoCapture"`                   // post-session extraction
MemoryPinnedScopes             []string `json:"memory_pinned_scopes" toml:"MemoryPinnedScopes"`                 // always injected, default ["user/"]
```

**TOML section** to add in `.pando.toml` defaults:
```toml
[Remembrances]
# ... existing fields ...
MemoryEnabled = false
MemoryContextEnrichmentEnabled = false
MemoryContextMaxItems = 10
MemoryContextMaxChars = 1500
MemoryDefaultTTLDays = 180
MemoryGCInterval = '1h'
MemoryAutoCapture = false
MemoryPinnedScopes = ['user/']
```

Add defaults normalization in `normalizeRemembrancesDefaults()`:
- `MemoryContextMaxItems = 10` if 0
- `MemoryContextMaxChars = 1500` if 0
- `MemoryDefaultTTLDays = 180` if 0
- `MemoryGCInterval = "1h"` if ""

---

## Phase 4 — KBStore Memory Extensions

**File**: `internal/rag/kb/kb.go` (new methods) + `internal/rag/kb/memory.go` (new file)

### New methods on `KBStore`

```go
// UpsertMemory creates or updates a memory document by key.
// If key is empty, falls back to file_path-based upsert (same as AddDocument).
// When updating, increments hits and extends expires_at.
func (s *KBStore) UpsertMemory(ctx context.Context, opts MemoryUpsertOptions) error

// GetMemoryByKey retrieves a memory document by its unique key.
func (s *KBStore) GetMemoryByKey(ctx context.Context, key string) (*Document, error)

// IncrementMemoryHits increments the hits counter and extends expires_at.
// New expires_at = MAX(current expires_at, now + defaultTTLDays).
func (s *KBStore) IncrementMemoryHits(ctx context.Context, docID int64, defaultTTLDays int) error

// MarkDocumentOutdated sets outdated=true and writes updated frontmatter to filesystem.
func (s *KBStore) MarkDocumentOutdated(ctx context.Context, filePath string) error

// GetExpiredDocuments returns file_path+id for documents where expires_at < now and outdated=0.
func (s *KBStore) GetExpiredDocuments(ctx context.Context) ([]DocumentRef, error)

// GetMemoriesForInjection returns the top-N memories for context enrichment.
// Scores: 0.6*vector_similarity + 0.3*(1/(age_days+1)) + 0.1*(hits/max_hits).
// Only returns non-outdated documents with tag "memory".
// Optionally filters by scope (always includes pinned scopes).
func (s *KBStore) GetMemoriesForInjection(ctx context.Context, query string, limit int, maxChars int, scopes []string) ([]MemoryResult, error)
```

### `MemoryUpsertOptions` struct

```go
type MemoryUpsertOptions struct {
    FilePath      string
    Key           string         // if set, upsert by key
    Scope         string         // "user/", "project/", "session/"
    Content       string
    Tags          []string       // must include "memory"
    Metadata      map[string]any
    Importance    float64
    Source        string
    DefaultTTLDays int           // 0 → use KBStore default (180)
}
```

### SQL for upsert by key (proxy path)
```sql
INSERT INTO kb_documents (file_path, content, metadata, memory_key, memory_scope,
    importance, source, expires_at, hits, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
ON CONFLICT(memory_key) DO UPDATE SET
    content    = excluded.content,
    metadata   = excluded.metadata,
    updated_at = excluded.updated_at,
    hits       = hits + 1,
    expires_at = MAX(expires_at, datetime('now', '+180 days')),
    outdated   = 0
WHERE memory_key IS NOT NULL;
```

---

## Phase 5 — Memory Tools (simplified API)

**File**: `internal/llm/tools/remembrances_memory.go` (new file)

### Tool: `remember`

```go
ToolInfo{
    Name: "remember",
    Description: `Store or update a memory that persists across sessions.
Use for user preferences, environment facts, project decisions, corrections.
If key is provided, performs upsert — replaces any previous memory with the same key.
Tag "memory" is added automatically. TTL defaults to 6 months, extended on each recall.`,
    Parameters: {
        "content":    string (required) — the fact or preference to remember,
        "key":        string (optional) — upsert key, e.g. "user.preferred_lang",
        "scope":      string (optional) — "user/", "project/", "session/" (default "user/"),
        "importance": float  (optional) — 0.0–1.0 weight for injection ranking (default 0.5),
        "ttl_days":   int    (optional) — override default TTL in days,
    },
    Required: ["content"],
}
```

### Tool: `recall`

```go
ToolInfo{
    Name: "recall",
    Description: `Search memories stored via 'remember'. Returns relevant memories sorted by
relevance + recency + access frequency. Automatically increments hit counter.`,
    Parameters: {
        "query":  string  (required),
        "scope":  string  (optional) — filter by scope prefix,
        "limit":  int     (optional, default 5, max 20),
    },
    Required: ["query"],
}
```

### Tool: `forget`

```go
ToolInfo{
    Name: "forget",
    Description: `Delete a memory by its key or file path. Use when information is
no longer valid or the user explicitly asks to forget something.`,
    Parameters: {
        "key":       string (optional) — upsert key,
        "file_path": string (optional) — direct path,
    },
    // At least one of key or file_path required (validated at runtime)
}
```

**Registration**: Wire into `internal/llm/agent/tools.go` alongside existing KB tools, gated behind `cfg.Remembrances.MemoryEnabled`.

---

## Phase 6 — Background Memory GC Service

**File**: `internal/rag/kb/memory_gc.go` (new file)

```go
// MemoryGCService periodically scans for expired memory documents
// and marks them as outdated in both SQLite and the filesystem mirror.
type MemoryGCService struct {
    store    *KBStore
    interval time.Duration
    done     chan struct{}
}

func NewMemoryGCService(store *KBStore, interval time.Duration) *MemoryGCService

func (g *MemoryGCService) Start(ctx context.Context)
func (g *MemoryGCService) Stop()

// runOnce executes one GC pass:
// 1. GetExpiredDocuments()
// 2. For each: MarkDocumentOutdated() — sets outdated=1 in DB, writes updated frontmatter
// 3. Logs summary: "memory gc: marked N documents as outdated"
func (g *MemoryGCService) runOnce(ctx context.Context) error
```

**Wiring**: Start in `internal/app/app.go` when `cfg.Remembrances.Enabled && cfg.Remembrances.MemoryEnabled`. Parse `MemoryGCInterval` with `time.ParseDuration`, fallback to 1h.

---

## Phase 7 — Memory Context Enrichment

**File**: `internal/llm/prompt/builder.go` or the existing context enrichment pipeline.

When `MemoryEnabled && MemoryContextEnrichmentEnabled`:

1. Before each turn, call `GetMemoriesForInjection(ctx, userMessage, maxItems, maxChars, pinnedScopes)`
2. Build a `<memories>` block:
```
<memories>
[key: user.preferred_lang] Go is my preferred language (scope: user, importance: 0.9)
[key: project.deploy_target] Deployment target is GCP europe-west1 (scope: project)
</memories>
```
3. Inject into system prompt BEFORE the main instructions (so the model treats it as known context, not retrieved information)
4. Increment hits for each memory returned (side-effect call via goroutine to avoid blocking)

**Scoring formula** in `GetMemoriesForInjection`:
```
score = 0.6 * vector_similarity
      + 0.3 * (1.0 / (age_in_days + 1.0))
      + 0.1 * min(float64(hits)/100.0, 1.0)
```

For `pinned_scopes` (default `["user/"]`): always include all non-outdated memories in those scopes (up to `maxItems/2` slots), regardless of query similarity. Fill remaining slots with similarity-ranked results.

**Budget enforcement**: truncate memory block to `MemoryContextMaxChars` characters total. Memories are sorted descending by score; truncate from the bottom if over budget.

---

## Phase 8 — TUI Settings

**File**: `internal/tui/page/settings.go` → `buildRemembrancesSection()` (lines 1781–2126)

Add a new subsection **"Memory"** after the existing context enrichment section:

```
╔══════════════════════════════════════╗
║  Memory System                       ║
╠══════════════════════════════════════╣
║  Memory Enabled              [OFF]   ║
║  Auto-inject in context      [OFF]   ║
║  Context max items            10     ║
║  Context max chars           1500    ║
║  Default TTL (days)           180    ║
║  GC interval (e.g. "1h")     "1h"   ║
║  Auto-capture memories       [OFF]   ║
║  Pinned scopes               user/   ║
╚══════════════════════════════════════╝
```

Fields map to new `RemembrancesConfig` memory fields. Persist via existing `config.UpdateRemembrances()`.

---

## Phase 9 — Web UI Settings

**File**: `web-ui/src/` — Remembrances/KB settings panel (existing phase from webui_settings plan)

Add **Memory** section (collapsible card) in the Remembrances settings page:

```
Memory System
  ├─ [toggle] Memory Enabled
  ├─ [toggle] Auto-inject memories in system prompt
  ├─ [number] Max memory items to inject (default: 10)
  ├─ [number] Max chars budget (default: 1500)
  ├─ [number] Default TTL days (default: 180)
  ├─ [text]   GC check interval (default: "1h")
  ├─ [toggle] Auto-capture after session
  └─ [tags]   Pinned scopes (always injected)
```

Backend API: extend the existing `PUT /api/v1/config/remembrances` endpoint to accept + return the new memory fields.

---

## Phase 10 — `kb_add_document` Tool Update

When the document has tag `memory` and provides `key` or `scope` in metadata, route through `UpsertMemory` instead of `AddDocument`/`UpdateDocument`. This preserves backward compatibility while enabling memory semantics transparently.

Also update `KBSearchDocumentsTool` to:
- Accept `exclude_outdated bool` parameter (default: true)
- Accept `scope` string filter parameter
- Increment hits for returned memory documents (when tag=memory)

---

## Implementation Order (priority)

| Phase | Priority | Dependencies |
|-------|----------|-------------|
| 1 - DB Migration | P0 | none |
| 2 - FrontMatter | P0 | none |
| 3 - Config | P0 | none |
| 4 - KBStore extensions | P1 | 1, 2 |
| 5 - Memory Tools | P1 | 3, 4 |
| 6 - GC Service | P1 | 4 |
| 7 - Context Enrichment | P2 | 4, 5 |
| 8 - TUI Settings | P2 | 3 |
| 9 - Web UI Settings | P2 | 3 |
| 10 - kb_add_document update | P3 | 4 |

Start with phases 1-3 in parallel, then 4-6, then 7-10.

---

## Key Decisions

1. **Tag `memory`** is the discriminator for the memory subsystem. Any KB document with tag `memory` participates in memory semantics (TTL, hits, injection).

2. **Upsert by `memory_key`**: unique SQLite index on `memory_key WHERE memory_key IS NOT NULL`. On conflict → update content + hits + expires_at + outdated=0.

3. **TTL extension formula**: `new_expires_at = MAX(current_expires_at, NOW + default_ttl_days)`. This means actively-used memories never expire.

4. **Outdated vs deleted**: `outdated=true` means the GC has flagged it. The document still exists, is returned if explicitly searched, but excluded from auto-injection. The agent can call `forget` to fully delete.

5. **`pinned_scopes`** (default `["user/"]`): always injected without needing query match. Ideal for stable user preferences.

6. **`session/` scope**: short-lived memories. Can default to a shorter TTL (e.g., 7 days). GC will naturally expire them.
