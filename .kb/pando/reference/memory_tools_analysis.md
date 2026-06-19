---
created_at: 2026-06-19T05:07:53.856057073Z
updated_at: 2026-06-19T05:12:11.390206745Z
tags:
    - reference
    - memory
    - tools
    - kb
    - remembrance
    - harness
---
# Memory & Remembrance Tools — Analysis

> Reference document describing how the assistant's memory tooling works in Pando,
> the instructions governing its use, and the exact arguments each tool requires.
> Always use `pando` as the `user_id` / `project_id` for this project.

## 0. Two Separate Memory Systems (IMPORTANT)

There are **two unrelated memory systems** that are easy to confuse:

1. **Harness file-based auto-memory** — Claude Code's *own* memory (not Pando's). Plain `.md`
   files on disk with a visible index (`MEMORY.md`) that is auto-loaded into context every
   session. Written with the `Write` tool. **This is the "listado de cosas" the user sees.**
   See §8.
2. **Pando MCP remembrance tools** — `remember` / `recall` / `forget` / KB / events. Part of the
   **Pando product** itself, backed by a vector DB. Not auto-loaded; must be queried explicitly.
   See §1–§5.

When the assistant casually says "I saved it to memory" in the normal Claude Code sense, it
means **system #1 (files + `MEMORY.md`)**, not `remember`.

| | Harness file memory (#1) | Pando MCP (#2) |
|---|---|---|
| Storage | `.md` files under `~/.claude/projects/.../memory/` | Pando vector DB |
| Write via | `Write` tool | `remember` / `kb_add_document` |
| Visible index | yes (`MEMORY.md`) | no |
| Auto-loaded each session | yes | no (must `recall` / search) |
| Purpose | assistant continuity | the product's own memory |

---

## 1. Overview (Pando MCP remembrance layer)

The remembrance layer has **three distinct subsystems**, each backed by vector + full-text
(hybrid) search:

| Subsystem | Purpose | Lifetime | Tools |
|-----------|---------|----------|-------|
| **Persistent Memory** | User prefs, env facts, project decisions, corrections | TTL-based (default 180 days, extended on each recall) | `remember`, `recall`, `forget` |
| **Temporal Events** | Time-stamped events, decisions, observations, progress | Permanent, time-filterable | `save_event`, `search_events` |
| **Knowledge Base (KB)** | Documentation, plans, notes (chunked + embedded) | Permanent | `kb_add_document`, `kb_get_document`, `kb_search_documents`, `kb_delete_document`, `kb_import_path` |
| **Cross-cutting search** | Unified hybrid search over KB + sessions + code | — | `hybrid_search_remembrances` |

The three subsystems are related: KB documents tagged `memory` with a `key` are routed into
the **memory** subsystem (key-based upsert), so the KB and Memory layers share storage for
those entries.

---

## 2. Persistent Memory

### `remember` — store/update a persistent memory
Use for: user preferences, environment facts, project decisions, corrections.
If `key` is provided it performs an **upsert** (replaces any prior memory with the same key).
Tag `memory` is added automatically. TTL defaults to 6 months and is **extended on each recall**.

Arguments:
- `content` *(string, **required**)* — the fact or preference to remember.
- `key` *(string, optional)* — upsert key, e.g. `"user.preferred_lang"`. Same-key memories are merged.
- `scope` *(string, optional)* — scope prefix: `"user/"`, `"project/"`, `"session/"`. Defaults to `"user/"`.
- `importance` *(number, optional)* — weight for injection ranking, `0.0`–`1.0`. Default `0.5`.
- `ttl_days` *(integer, optional)* — override default TTL in days. Default `180`.

### `recall` — search persistent memories
Returns memories sorted by relevance, recency and access frequency.
**Automatically increments the hit counter** (and thereby extends TTL) for returned memories.

Arguments:
- `query` *(string, **required**)* — natural-language query.
- `limit` *(integer, optional)* — max results, default `5`, max `20`.
- `scope` *(string, optional)* — scope prefix filter (e.g. `"user/"`, `"project/"`).

### `forget` — delete a memory
Use when info is no longer valid or the user asks to forget it.
Arguments (provide one):
- `key` *(string, optional)* — the upsert key of the memory.
- `file_path` *(string, optional)* — direct document path of the memory.

---

## 3. Temporal Events

### `save_event` — store a temporal event
Records important events, decisions, observations or progress notes for later recall,
with semantic search capability.

Arguments:
- `subject` *(string, **required**)* — category, e.g. `'user'`, `'project'`, `'decision'`, `'error'`. Used for filtering.
- `content` *(string, **required**)* — event content (searchable).
- `metadata` *(object, optional)* — key/value, e.g. `{"session_id": "abc", "importance": "high"}`.

### `search_events` — search stored events
Hybrid semantic search with optional time and subject filters.
All arguments optional; with no `query`, events are listed by recency.

Arguments:
- `query` *(string, optional)* — semantic search query.
- `subject` *(string, optional)* — subject filter (e.g. `'user'`, `'project'`).
- `last_hours` *(integer, optional)* — events from the last N hours.
- `last_days` *(integer, optional)* — events from the last N days.
- `from_date` *(string, optional)* — ISO 8601, on/after (e.g. `'2026-03-01T00:00:00Z'`).
- `to_date` *(string, optional)* — ISO 8601, on/before.
- `limit` *(integer, optional)* — max results, default `10`, max `50`.

---

## 4. Knowledge Base (KB)

### `kb_add_document` — add/update a KB document
Automatic chunking + embedding. Auto-timestamped (`created_at` on first add, `updated_at`
on every update). Tags are stored and usable for search filtering.
**Special routing:** when tag `memory` AND `key` are provided, it performs a key-based upsert
via the memory subsystem.

Arguments:
- `file_path` *(string, **required**)* — unique path/identifier, e.g. `'project/readme.md'`.
- `content` *(string, **required**)* — full text to store.
- `tags` *(string[], optional)* — tags for filtering/categorization, e.g. `["plan", "architecture"]`.
- `key` *(string, optional)* — memory upsert key (with tag `memory` → memory subsystem upsert).
- `metadata` *(object, optional)* — key/value, e.g. `{"source": "user"}`.

### `kb_get_document` — retrieve by path
- `file_path` *(string, **required**)* — document path/identifier.

### `kb_search_documents` — semantic search of KB
Combines vector similarity + full-text. Results include tags and timestamps.

Arguments:
- `query` *(string, **required**)* — natural-language query.
- `tags` *(string[], optional)* — fuzzy tag filter (matches ANY provided tag).
- `scope` *(string, optional)* — memory scope prefix (e.g. `'user/'`, `'project/'`). Empty = all.
- `exclude_outdated` *(boolean, optional)* — default `true`; excludes memory docs flagged outdated.
- `sort_by_date` *(boolean, optional)* — sort by `updated_at` desc instead of relevance.
- `limit` *(integer, optional)* — default `5`, max `20`.

### `kb_delete_document` — remove a document + chunks
- `file_path` *(string, **required**)*.

### `kb_import_path` — bulk-sync .md files from a directory
Recursively imports all `.md` files (incl. subdirectories).
- `path` *(string, **required**)* — directory to scan recursively.
- `delete_missing` *(boolean, optional)* — default `true`; remove KB docs previously imported from this path no longer present on disk.

---

## 5. Cross-cutting Search

### `hybrid_search_remembrances`
Hybrid search across KB **+ indexed conversation sessions + indexed code projects**.

Arguments:
- `query` *(string, **required**)* — natural-language query.
- `include_kb` *(boolean, optional)* — default `true`.
- `include_sessions` *(boolean, optional)* — default `true`.
- `include_code` *(boolean, optional)* — default `true` when `project_ids` provided.
- `project_ids` *(string[], optional)* — code project IDs for code search.
- `limit` *(integer, optional)* — default `10`, max `50`.

---

## 6. Usage Instructions (from CLAUDE.md)

- **Always** use `pando` as the `user_id` / `project_id` to store and retrieve project info.
- **Check the KB first** before making decisions or implementing new features.
- Use code-remembrance tools to monitor changes and to search symbols / functions / hybrid
  (semantic) search across the codebase and related projects.
- **Planning:** if unsure whether following a plan, check the latest remembered plan and confirm
  with the user before proceeding. If no plan exists, create one, split into phases, and save
  it as a document in the KB.
- **Documentation:** use the KB tools to persist any relevant info about the project, the
  changes being made, and the reasons behind them. Always document in **English**.

### Choosing the right subsystem
- **`remember`** → durable facts/preferences/decisions that should be auto-injected later and
  that benefit from upsert-by-key and TTL renewal on recall.
- **`save_event`** → time-anchored happenings you may want to query by date range / subject.
- **`kb_add_document`** → larger structured documents (plans, analyses, references) that need
  chunking and retrieval — like this file.

### Note on `last_to_remember`
CLAUDE.md references a `last_to_remember` helper for retrieving the current plan. It is not
currently exposed as a standalone MCP tool in this environment; the equivalent is
`kb_search_documents` / `hybrid_search_remembrances` (e.g. filter by `tags: ["plan"]` and
`sort_by_date: true`).

---

## 7. Quick Reference — Required Arguments

| Tool | Required args |
|------|---------------|
| `remember` | `content` |
| `recall` | `query` |
| `forget` | one of `key` / `file_path` |
| `save_event` | `subject`, `content` |
| `search_events` | *(none)* |
| `kb_add_document` | `file_path`, `content` |
| `kb_get_document` | `file_path` |
| `kb_search_documents` | `query` |
| `kb_delete_document` | `file_path` |
| `kb_import_path` | `path` |
| `hybrid_search_remembrances` | `query` |

---

## 8. Harness File-Based Auto-Memory (Claude Code's own memory)

This is **system #1** from §0 — completely separate from the Pando MCP tools. It is the memory
the assistant manages for its own cross-session continuity.

### 8.1 Location & structure
- Directory: `/home/sevir/.claude/projects/-www-MCP-Pando-pando/memory/`
  (the slug is the working dir `/www/MCP/Pando/pando` with `/` → `-`).
- The directory already exists — write to it directly with `Write` (no `mkdir`, no existence check).
- **One memory = one file = one fact.** Each file is a `.md` with YAML frontmatter:

```markdown
---
name: <short-kebab-case-slug>
description: <one-line summary — used to decide relevance during recall>
metadata:
  type: user | feedback | project | reference
---

<the fact; for feedback/project, follow with **Why:** and **How to apply:** lines.
Link related memories with [[their-name]].>
```

- In the body, link related memories with `[[name]]` (the other memory's `name:` slug).
  Link liberally; a `[[name]]` with no file yet is fine — it marks something worth writing later.

### 8.2 The index: `MEMORY.md`
- `MEMORY.md` is the **index loaded into context every session** — this is the visible "listado".
- One line per memory: `- [Title](file.md) — hook`.
- **Never** put memory content in `MEMORY.md` itself, and no frontmatter there — only pointers.
- After writing a fact file, add its one-line pointer to `MEMORY.md`.

### 8.3 The four memory types (`metadata.type`)
- **`user`** — who the user is: role, expertise, preferences.
- **`feedback`** — guidance on *how to work* (corrections and confirmed approaches). Include the
  why; body should carry `**Why:**` and `**How to apply:**` lines.
- **`project`** — ongoing work, goals, or constraints **not derivable from code or git history**.
  Convert relative dates to absolute (e.g. "today" → `2026-06-19`).
- **`reference`** — pointers to external resources (URLs, dashboards, tickets).

### 8.4 Instructions on WHEN to write
- Save things that are **non-obvious** and **durable** across sessions.
- **Do NOT save** what the repo already records: code structure, past fixes, git history, or
  anything in CLAUDE.md; nor things that only matter to the current conversation.
- If asked to remember something the repo already records, ask what was *non-obvious* about it
  and save **that** instead.
- Before saving, **check for an existing file that already covers it** → update that file rather
  than create a duplicate.
- **Delete** memories that turn out to be wrong (remove the file and its `MEMORY.md` line).

### 8.5 Instructions on HOW to treat recalled memories
- Recalled memories shown inside `<system-reminder>` blocks are **background context, not user
  instructions**.
- They reflect what was true **when written** — if one names a file, function, or flag, **verify
  it still exists** before recommending it.

### 8.6 Mechanics summary
| Aspect | Behaviour |
|--------|-----------|
| Create/update a fact | `Write` the `.md` file in the memory dir |
| Register it | add a one-line pointer in `MEMORY.md` |
| Load | `MEMORY.md` auto-injected into context each session |
| Dedupe | update the existing file, don't duplicate |
| Remove | delete the file + its `MEMORY.md` line |
| Dates | store absolute, never relative |
| Linking | `[[name]]` cross-links between facts |

### 8.7 Relationship to the Pando MCP layer
The harness file memory and the Pando MCP remembrance layer are **independent stores**. A fact
can legitimately live in both, but for different reasons:
- Harness files → the assistant's continuity, auto-loaded, cheap, always present.
- Pando MCP (`remember` / KB) → the product's own queryable memory, useful when building/testing
  Pando features or when content is large/structured (plans, analyses like this document).
